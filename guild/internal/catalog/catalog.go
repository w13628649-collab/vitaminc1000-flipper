package catalog

import (
	"sort"
	"strings"
)

// Category 是分类树里的一个节点。
type Category struct {
	ID      string
	Parent  string
	LabelZH string
	LabelEN string
	Sort    int
}

// Catalog 是物品目录。数据在 PostgreSQL,搜索和浏览在内存里做——
// 一万一千条,几 MB 而已,没必要每次查询都打数据库。
type Catalog struct {
	items      map[string]Item
	categories []Category
	syncedAt   string
}

func New(items []Item, categories []Category, syncedAt string) *Catalog {
	byID := make(map[string]Item, len(items))
	for _, i := range items {
		byID[i.ItemID] = i
	}
	return &Catalog{items: byID, categories: categories, syncedAt: syncedAt}
}

func (c *Catalog) Len() int         { return len(c.items) }
func (c *Catalog) SyncedAt() string { return c.syncedAt }

func (c *Catalog) Get(itemID string) (Item, bool) {
	i, ok := c.items[itemID]
	return i, ok
}

// NameOf 满足 screen.Namer。查不到就退回 id 本身,
// 好过在界面上显示空白。
func (c *Catalog) NameOf(itemID string) string {
	if i, ok := c.items[itemID]; ok {
		return i.DisplayName()
	}
	return itemID
}

func (c *Catalog) ENNameOf(itemID string) string {
	if i, ok := c.items[itemID]; ok && i.NameEN != "" {
		return i.NameEN
	}
	return itemID
}

func order(a, b Item) bool {
	if a.Tier != b.Tier {
		return a.Tier < b.Tier
	}
	if a.Enchantment != b.Enchantment {
		return a.Enchantment < b.Enchantment
	}
	return a.ItemID < b.ItemID
}

// Search 中文名、英文名、物品 ID 都能搜。精确匹配排最前,再是前缀匹配。
func (c *Catalog) Search(query string, limit int) []Item {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return nil
	}
	var exact, prefix, rest []Item
	for _, item := range c.items {
		if !item.matches(needle) {
			continue
		}
		zh, en, id := strings.ToLower(item.NameZH), strings.ToLower(item.NameEN), strings.ToLower(item.ItemID)
		switch {
		case needle == zh || needle == en || needle == id:
			exact = append(exact, item)
		case strings.HasPrefix(zh, needle) || strings.HasPrefix(en, needle) || strings.HasPrefix(id, needle):
			prefix = append(prefix, item)
		default:
			rest = append(rest, item)
		}
	}
	for _, group := range [][]Item{exact, prefix, rest} {
		sort.Slice(group, func(i, j int) bool { return order(group[i], group[j]) })
	}
	out := append(append(exact, prefix...), rest...)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// BrowseQuery 对应游戏里市场那几个下拉框。
// Enchantment 用 -1 表示"不限",因为 0 是有意义的值(无附魔)。
type BrowseQuery struct {
	Category    string
	Subcategory string
	Family      string
	Tier        int
	Enchantment int
	Limit       int
}

func (c *Catalog) Browse(q BrowseQuery) []Item {
	var picked []Item
	for _, item := range c.items {
		if q.Category != "" && item.Category != q.Category {
			continue
		}
		if q.Subcategory != "" && item.Subcategory != q.Subcategory {
			continue
		}
		if q.Family != "" && item.Family != q.Family {
			continue
		}
		if q.Tier != 0 && item.Tier != q.Tier {
			continue
		}
		if q.Enchantment >= 0 && item.Enchantment != q.Enchantment {
			continue
		}
		picked = append(picked, item)
	}
	sort.Slice(picked, func(i, j int) bool {
		a, b := picked[i], picked[j]
		if a.Tier != b.Tier {
			return a.Tier < b.Tier
		}
		if a.Enchantment != b.Enchantment {
			return a.Enchantment < b.Enchantment
		}
		an, bn := a.DisplayNameBase(), b.DisplayNameBase()
		if an != bn {
			return an < bn
		}
		return a.ItemID < b.ItemID
	})
	if q.Limit > 0 && len(picked) > q.Limit {
		picked = picked[:q.Limit]
	}
	return picked
}

// Resolve 展开模式并对目录校验。
//
// 返回 (命中的 ID, 目录里不存在的 ID)。不存在的通常意味着写错了 tier
// 或者游戏改了命名,调用方应该把它报出来而不是静默丢掉。
func (c *Catalog) Resolve(patterns, exclude []string) (resolved, missing []string) {
	excluded := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		excluded[e] = struct{}{}
	}
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		for _, candidate := range ExpandBraces(pattern) {
			if _, skip := excluded[candidate]; skip {
				continue
			}
			if _, dup := seen[candidate]; dup {
				continue
			}
			seen[candidate] = struct{}{}
			if _, ok := c.items[candidate]; ok {
				resolved = append(resolved, candidate)
			} else {
				missing = append(missing, candidate)
			}
		}
	}
	return resolved, missing
}

// 三级菜单:大类 → 子类 → 物品族。三层形状不同,分成三个类型比
// 自嵌套的节点更好读,前端也少几个 if。
type (
	MenuCategory struct {
		ID    string    `json:"id"`
		Label string    `json:"label"`
		Count int       `json:"count"`
		Subs  []MenuSub `json:"subs"`
	}
	MenuSub struct {
		ID       string       `json:"id"`
		Label    string       `json:"label"`
		Count    int          `json:"count"`
		Families []MenuFamily `json:"families"`
	}
	MenuFamily struct {
		Key   string `json:"key"`
		Label string `json:"label"`
		Count int    `json:"count"`
		Tiers []int  `json:"tiers"`
	}
)

// MenuTree 是三级菜单用的完整树:大类 → 子类 → 物品族。
// 一次全给前端,悬停展开就不用再等网络了。
func (c *Catalog) MenuTree() []MenuCategory {
	labels := map[[2]string]Category{}
	topOrder := map[string]int{}
	subOrder := map[[2]string]int{}
	for _, cat := range c.categories {
		labels[[2]string{cat.ID, cat.Parent}] = cat
		if cat.Parent == "" {
			topOrder[cat.ID] = cat.Sort
		} else {
			subOrder[[2]string{cat.Parent, cat.ID}] = cat.Sort
		}
	}
	labelOf := func(id, parent string) string {
		cat, ok := labels[[2]string{id, parent}]
		if !ok {
			cat, ok = labels[[2]string{id, ""}]
		}
		if !ok {
			return id
		}
		if cat.LabelZH != "" {
			return cat.LabelZH
		}
		if cat.LabelEN != "" {
			return cat.LabelEN
		}
		return id
	}

	grouped := map[string]map[string]map[string][]Item{}
	for _, item := range c.items {
		if item.Category == "" {
			continue
		}
		subs, ok := grouped[item.Category]
		if !ok {
			subs = map[string]map[string][]Item{}
			grouped[item.Category] = subs
		}
		families, ok := subs[item.Subcategory]
		if !ok {
			families = map[string][]Item{}
			subs[item.Subcategory] = families
		}
		families[item.Family] = append(families[item.Family], item)
	}

	rank := func(m map[string]int, key string) int {
		if v, ok := m[key]; ok {
			return v
		}
		return 9999
	}

	var tree []MenuCategory
	for cid := range grouped {
		tree = append(tree, MenuCategory{ID: cid})
	}
	sort.Slice(tree, func(i, j int) bool {
		return rank(topOrder, tree[i].ID) < rank(topOrder, tree[j].ID)
	})

	for ti := range tree {
		cid := tree[ti].ID
		subs := grouped[cid]

		var subIDs []string
		for sid := range subs {
			subIDs = append(subIDs, sid)
		}
		sort.Slice(subIDs, func(i, j int) bool {
			a, ok := subOrder[[2]string{cid, subIDs[i]}]
			if !ok {
				a = 9999
			}
			b, ok := subOrder[[2]string{cid, subIDs[j]}]
			if !ok {
				b = 9999
			}
			if a != b {
				return a < b
			}
			return subIDs[i] < subIDs[j]
		})

		var subNodes []MenuSub
		for _, sid := range subIDs {
			families := subs[sid]
			var famNodes []MenuFamily
			total := 0
			for fkey, members := range families {
				tiers := map[int]struct{}{}
				for _, m := range members {
					tiers[m.Tier] = struct{}{}
				}
				var tierList []int
				for tier := range tiers {
					tierList = append(tierList, tier)
				}
				sort.Ints(tierList)
				famNodes = append(famNodes, MenuFamily{
					Key:   fkey,
					Label: FamilyLabel(members),
					Count: len(members),
					Tiers: tierList,
				})
				total += len(members)
			}
			sort.Slice(famNodes, func(i, j int) bool {
				ai, bi := 0, 0
				if len(famNodes[i].Tiers) > 0 {
					ai = famNodes[i].Tiers[0]
				}
				if len(famNodes[j].Tiers) > 0 {
					bi = famNodes[j].Tiers[0]
				}
				if ai != bi {
					return ai < bi
				}
				return famNodes[i].Label < famNodes[j].Label
			})
			subNodes = append(subNodes, MenuSub{
				ID: sid, Label: labelOf(sid, cid), Count: total, Families: famNodes,
			})
		}
		count := 0
		for _, n := range subNodes {
			count += n.Count
		}
		tree[ti].Label = labelOf(cid, "")
		tree[ti].Count = count
		tree[ti].Subs = subNodes
	}
	return tree
}
