// Package catalog 是物品目录:中英文名、市场分类树、物品族、图标。
//
// 固定不变的东西全部入库,跟行情数据同一个 PostgreSQL。
//
// 数据来源(sync 时拉一次):
//   - formatted/items.json(24MB)官方中英文名 + 完整的附魔变体 ID
//   - 根目录 items.json(17MB)市场分类树、tier、品质档数,**不含附魔变体**
//   - data/categories.json(随包 6KB)分类的中文名,来自 90MB 的 localization.json,
//     极少变动,不值得让每个人 sync 时都下一遍
package catalog

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	tierRE = regexp.MustCompile(`^T(\d)_`)
	// 物品族 = 去掉等级前缀、附魔后缀和材料的 _LEVELn 中缀之后剩下的骨架。
	// T5_2H_FIRESTAFF@2 和 T4_2H_FIRESTAFF 是同一族;
	// T4_CLOTH_LEVEL1@1 和 T4_CLOTH 也是同一族(材料的附魔是独立条目)。
	familyStripRE = regexp.MustCompile(`^T\d+_|_LEVEL\d+|@\d+`)
	braceRE       = regexp.MustCompile(`\{([^{}]*)\}`)
)

func FamilyOf(itemID string) string { return familyStripRE.ReplaceAllString(itemID, "") }

type Item struct {
	ItemID      string `json:"item_id"`
	NameZH      string `json:"name_zh"`
	NameEN      string `json:"name_en"`
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
	Family      string `json:"family"`
	Tier        int    `json:"tier"`
	Enchantment int    `json:"enchantment"`
	MaxQuality  int    `json:"max_quality"`
}

// BaseID 剥掉附魔后缀。附魔变体不在原始 dump 里,分类要靠本体查。
func (i Item) BaseID() string {
	base, _, _ := strings.Cut(i.ItemID, "@")
	return base
}

// DisplayName 中文优先。dump 里附魔物品和本体同名,
// 补回 .N 以免列表里分不清。
func (i Item) DisplayName() string {
	base := i.NameZH
	if base == "" {
		base = i.NameEN
	}
	if base == "" {
		base = i.ItemID
	}
	if i.Enchantment != 0 {
		return fmt.Sprintf("%s.%d", base, i.Enchantment)
	}
	return base
}

func (i Item) matches(needle string) bool {
	return strings.Contains(strings.ToLower(i.NameZH), needle) ||
		strings.Contains(strings.ToLower(i.NameEN), needle) ||
		strings.Contains(strings.ToLower(i.ItemID), needle)
}

// LongestCommonSuffix 取一族名字的公共后缀。
//
// 一族里各等级的名字只差一个前缀:新手级/学徒级/老手级/专家级……
// 取公共后缀就能得到族名,不用硬编码那张等级前缀表——游戏加了新等级
// 或者改了叫法,这里不用跟着改。
//
// **只要求多数成员满足**,不是全部:火焰法杖那一族里混着一件叫
// 「Vendetta之怒」的特殊武器,按全员取交集会把后缀打成空字符串。
func LongestCommonSuffix(names []string, minRatio float64) string {
	seen := map[string]struct{}{}
	var uniq []string
	for _, n := range names {
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		uniq = append(uniq, n)
	}
	sort.Strings(uniq)
	if len(uniq) == 0 {
		return ""
	}
	if len(uniq) == 1 {
		return uniq[0]
	}

	need := int(math.Ceil(float64(len(uniq)) * minRatio))
	if need < 2 {
		need = 2
	}
	best := ""
	for _, source := range uniq {
		runes := []rune(source)
		// 从最长的后缀往短了试,第一个够票的就是这个名字能贡献的最长后缀
		for start := range runes {
			suffix := string(runes[start:])
			if len([]rune(suffix)) <= len([]rune(best)) {
				break
			}
			votes := 0
			for _, n := range uniq {
				if strings.HasSuffix(n, suffix) {
					votes++
				}
			}
			if votes >= need {
				best = suffix
				break
			}
		}
	}
	// 中文等级都是"X级",英文是"X's",公共后缀会把这个尾巴带进来
	return strings.TrimLeft(best, "级's的 ")
}

// FamilyLabel 给一族物品起个名。
//
// 装备各等级只差一个前缀(老手级/专家级/大师级……),最长公共后缀正好是族名。
// 材料不吃这套:细布 / 精布 / 华布 的公共后缀只剩一个"布"字,金属锭更是只剩
// "条"。退化成一两个字就改用这族里最低等级那件的全名——"钢条"比"条"认得出来。
func FamilyLabel(members []Item) string {
	var names []string
	for _, m := range members {
		if n := m.DisplayNameBase(); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		if len(members) > 0 {
			return members[0].ItemID
		}
		return ""
	}
	label := LongestCommonSuffix(names, 0.7)
	if len([]rune(label)) >= 2 {
		return label
	}
	lowest := members[0]
	for _, m := range members[1:] {
		if m.Tier < lowest.Tier ||
			(m.Tier == lowest.Tier && m.Enchantment < lowest.Enchantment) ||
			(m.Tier == lowest.Tier && m.Enchantment == lowest.Enchantment && m.ItemID < lowest.ItemID) {
			lowest = m
		}
	}
	if n := lowest.DisplayNameBase(); n != "" {
		return n
	}
	return label
}

// DisplayNameBase 是不带附魔后缀的显示名,族名计算用它。
func (i Item) DisplayNameBase() string {
	if i.NameZH != "" {
		return i.NameZH
	}
	return i.NameEN
}

// ExpandBraces 展开 T{4,5}_{ORE,FIBER} → [T4_ORE, T4_FIBER, T5_ORE, T5_FIBER]
func ExpandBraces(pattern string) []string {
	loc := braceRE.FindStringSubmatchIndex(pattern)
	if loc == nil {
		return []string{pattern}
	}
	head, tail := pattern[:loc[0]], pattern[loc[1]:]
	body := pattern[loc[2]:loc[3]]
	var out []string
	for _, option := range strings.Split(body, ",") {
		out = append(out, ExpandBraces(head+strings.TrimSpace(option)+tail)...)
	}
	return out
}

// parseIDParts 从 item id 里抠出 tier 和附魔等级。
func parseIDParts(itemID string) (tier, enchant int) {
	if m := tierRE.FindStringSubmatch(itemID); m != nil {
		tier, _ = strconv.Atoi(m[1])
	}
	if _, suffix, ok := strings.Cut(itemID, "@"); ok {
		enchant, _ = strconv.Atoi(suffix)
	}
	return tier, enchant
}
