package refine

import (
	"regexp"
	"strconv"
)

// Line 是一条精炼线:原料族 → 半成品族。
//
// 四条线的配方数量、专注、物品价值完全相同,只有用的心不同(规则表 A3 ✔:
// 两位审查人各自脚本比对 27 个物品,差异都是 0;recipes_test 里也断言了)。
// 石头线规则不同(附魔石头产出多块 .0 石块,没有附魔石块,A8),本期不做。
type Line struct {
	// Key 是线的名字,和原料族同名(ORE / HIDE / FIBER / WOOD)
	Key string
	// Raw、Refined 是 item id 里 T8_ 之后、_LEVEL 之前那一段
	Raw     string
	Refined string
	// Heart 是 T4~T8 .0~.3 的心配方用的心(A4 ✔:items.xml 107090–107129)
	Heart string
	// City 是这条线的精炼专精城,在那里精炼多 +40 加成(C3 ✔:craftingmodifiers.xml)
	City string
}

// Lines 是本期做的四条线。
var Lines = []Line{
	{Key: "ORE", Raw: "ORE", Refined: "METALBAR", Heart: "T1_FACTION_MOUNTAIN_TOKEN_1", City: "Thetford"},
	{Key: "HIDE", Raw: "HIDE", Refined: "LEATHER", Heart: "T1_FACTION_STEPPE_TOKEN_1", City: "Martlock"},
	{Key: "FIBER", Raw: "FIBER", Refined: "CLOTH", Heart: "T1_FACTION_SWAMP_TOKEN_1", City: "Lymhurst"},
	{Key: "WOOD", Raw: "WOOD", Refined: "PLANKS", Heart: "T1_FACTION_FOREST_TOKEN_1", City: "Fort Sterling"},
}

const (
	// MinTier:链底是 T2。items.xml 里 T2 半成品是 1 个 T2 原料炼 1 件、没有下级料,
	// T3 起才是 A1 的 n × 原料 + 1 × 下级料;T2 精炼不收站费(E3)
	MinTier = 2
	MaxTier = 8
	// MinEnchantTier:T3 及以下没有附魔版本,所以 T4.1~T4.4 的下级料用 T3(.0)(A2 ✔)
	MinEnchantTier = 4
	MaxEnchant     = 4
)

// LineOf 按原料族或半成品族找线。
func LineOf(family string) (Line, bool) {
	for _, l := range Lines {
		if l.Raw == family || l.Refined == family {
			return l, true
		}
	}
	return Line{}, false
}

// RawID、RefinedID 是这条线某一档的市场 id。
func (l Line) RawID(tier, enchant int) string     { return ItemID(l.Raw, tier, enchant) }
func (l Line) RefinedID(tier, enchant int) string { return ItemID(l.Refined, tier, enchant) }

// ItemID 拼市场 id:T8_ORE、T8_ORE_LEVEL4@4。
//
// 附魔资源在 items.json 里是独立条目 T8_ORE_LEVEL4,市场 / AODP / 抓包都带 @4,
// 两边的换算在 catalog.MarketID / DumpID。
func ItemID(family string, tier, enchant int) string {
	id := "T" + strconv.Itoa(tier) + "_" + family
	if enchant > 0 {
		e := strconv.Itoa(enchant)
		id += "_LEVEL" + e + "@" + e
	}
	return id
}

var itemIDRE = regexp.MustCompile(`^T(\d)_([A-Z]+)(?:_LEVEL(\d)@(\d))?$`)

// ParseItemID 是 ItemID 的反方向。只认市场 id:dump 里的 T8_ORE_LEVEL4(没有 @4)、
// _LEVEL 和 @ 对不上的、心这种不按 tier 编号的,都返回 ok=false。
func ParseItemID(id string) (family string, tier, enchant int, ok bool) {
	m := itemIDRE.FindStringSubmatch(id)
	if m == nil || m[3] != m[4] {
		return "", 0, 0, false
	}
	tier, _ = strconv.Atoi(m[1])
	if m[3] != "" {
		enchant, _ = strconv.Atoi(m[3])
	}
	return m[2], tier, enchant, true
}

// Enchants 是某个 tier 有的附魔档:T4 以上 .0~.4,T2/T3 只有 .0。
func Enchants(tier int) []int {
	if tier < MinEnchantTier {
		return []int{0}
	}
	out := make([]int, 0, MaxEnchant+1)
	for e := 0; e <= MaxEnchant; e++ {
		out = append(out, e)
	}
	return out
}
