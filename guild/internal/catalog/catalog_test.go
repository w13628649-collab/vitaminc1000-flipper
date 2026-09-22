package catalog

import (
	"reflect"
	"testing"
)

var categories = []Category{
	{ID: "weapons", LabelZH: "武器", LabelEN: "Weapons", Sort: 0},
	{ID: "crafting", LabelZH: "制作", LabelEN: "Crafting", Sort: 1},
	{ID: "firestaff", Parent: "weapons", LabelZH: "火焰法杖", LabelEN: "Fire Staff", Sort: 0},
	{ID: "refinedresources", Parent: "crafting", LabelZH: "加工资源", LabelEN: "Refined Resources", Sort: 0},
}

var (
	cloth4 = Item{"T4_CLOTH", "细布", "Fine Cloth", "crafting", "refinedresources", "CLOTH", 4, 0, 1}
	cloth5 = Item{"T5_CLOTH", "精布", "Ornate Cloth", "crafting", "refinedresources", "CLOTH", 5, 0, 1}
	staff  = Item{"T4_2H_FIRESTAFF@1", "老手级杰出火焰法杖", "Adept's Great Fire Staff",
		"weapons", "firestaff", "2H_FIRESTAFF", 4, 1, 5}
)

func build(items ...Item) *Catalog { return New(items, categories, "") }

func ids(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ItemID
	}
	return out
}

func TestBraceExpansion(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"T{4,5}_{ORE,FIBER}", []string{"T4_ORE", "T4_FIBER", "T5_ORE", "T5_FIBER"}},
		{"T4_CLOTH", []string{"T4_CLOTH"}},
		{"T{4, 5}_ORE", []string{"T4_ORE", "T5_ORE"}}, // 容忍空格
	}
	for _, c := range cases {
		if got := ExpandBraces(c.in); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s 展开成 %v,想要 %v", c.in, got, c.want)
		}
	}
}

func TestResolveReportsMissingIDs(t *testing.T) {
	resolved, missing := build(cloth4, cloth5, staff).Resolve([]string{"T{4,5,6}_CLOTH"}, nil)
	if !reflect.DeepEqual(resolved, []string{"T4_CLOTH", "T5_CLOTH"}) {
		t.Fatalf("命中 %v", resolved)
	}
	// 手写清单会在游戏更新后悄悄失效,必须报出来
	if !reflect.DeepEqual(missing, []string{"T6_CLOTH"}) {
		t.Fatalf("缺失 %v,想要 [T6_CLOTH]", missing)
	}
}

func TestResolveDedupesAndExcludes(t *testing.T) {
	resolved, _ := build(cloth4, cloth5).Resolve(
		[]string{"T4_CLOTH", "T{4,5}_CLOTH"}, []string{"T5_CLOTH"})
	if !reflect.DeepEqual(resolved, []string{"T4_CLOTH"}) {
		t.Fatalf("命中 %v,想要 [T4_CLOTH]", resolved)
	}
}

func TestDisplayNamePrefersOfficialChinese(t *testing.T) {
	c := build(cloth4, cloth5)
	if got := c.NameOf("T5_CLOTH"); got != "精布" {
		t.Fatalf("中文名 = %s", got)
	}
	if got := c.ENNameOf("T5_CLOTH"); got != "Ornate Cloth" {
		t.Fatalf("英文名 = %s", got)
	}
}

// dump 里附魔物品和本体同名,列表里会分不清
func TestEnchantedItemNameCarriesLevel(t *testing.T) {
	if got := build(staff).NameOf("T4_2H_FIRESTAFF@1"); got != "老手级杰出火焰法杖.1" {
		t.Fatalf("显示名 = %s", got)
	}
}

func TestUnknownIDFallsBackToItself(t *testing.T) {
	if got := build(cloth4).NameOf("T9_UNKNOWN"); got != "T9_UNKNOWN" {
		t.Fatalf("显示名 = %s", got)
	}
}

func TestSearchMatchesChineseEnglishAndID(t *testing.T) {
	c := build(cloth4, cloth5, staff)
	cases := []struct {
		q    string
		want []string
	}{
		{"细布", []string{"T4_CLOTH"}},
		{"ornate", []string{"T5_CLOTH"}},
		{"T4_CLOTH", []string{"T4_CLOTH"}},
		{"布", []string{"T4_CLOTH", "T5_CLOTH"}},
	}
	for _, tc := range cases {
		if got := ids(c.Search(tc.q, 30)); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("搜 %q 得到 %v,想要 %v", tc.q, got, tc.want)
		}
	}
	if got := c.Search("  ", 30); len(got) != 0 {
		t.Fatalf("空查询返回了 %v", ids(got))
	}
}

func TestBrowseByCategoryAndTier(t *testing.T) {
	c := build(cloth4, cloth5, staff)
	cases := []struct {
		name string
		q    BrowseQuery
		want []string
	}{
		{"按大类", BrowseQuery{Category: "crafting", Enchantment: -1}, []string{"T4_CLOTH", "T5_CLOTH"}},
		{"按子类", BrowseQuery{Subcategory: "firestaff", Enchantment: -1}, []string{"T4_2H_FIRESTAFF@1"}},
		{"大类加等级", BrowseQuery{Category: "crafting", Tier: 5, Enchantment: -1}, []string{"T5_CLOTH"}},
		{"按物品族", BrowseQuery{Family: "CLOTH", Enchantment: -1}, []string{"T4_CLOTH", "T5_CLOTH"}},
		// Enchantment=0 是"只要无附魔的",不是"不筛"——后者用 -1
		{"无附魔", BrowseQuery{Enchantment: 0}, []string{"T4_CLOTH", "T5_CLOTH"}},
		{"一级附魔", BrowseQuery{Enchantment: 1}, []string{"T4_2H_FIRESTAFF@1"}},
		{"不筛附魔", BrowseQuery{Enchantment: -1}, []string{"T4_CLOTH", "T4_2H_FIRESTAFF@1", "T5_CLOTH"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(c.Browse(tc.q)); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("得到 %v,想要 %v", got, tc.want)
			}
		})
	}
}

func TestFamilyKeyStripsTierEnchantAndLevel(t *testing.T) {
	cases := map[string]string{
		"T5_2H_FIRESTAFF@2": "2H_FIRESTAFF",
		"T4_CLOTH":          "CLOTH",
		// 材料的附魔是独立条目,得跟本体归到一族
		"T4_CLOTH_LEVEL1@1": "CLOTH",
	}
	for in, want := range cases {
		if got := FamilyOf(in); got != want {
			t.Fatalf("%s → %s,想要 %s", in, got, want)
		}
	}
}

// 火焰法杖那族里混着一件叫「Vendetta之怒」的特殊武器。
// 按全员取交集会把后缀打成空字符串,整族就没名字了。
func TestFamilySuffixNeedsOnlyAMajority(t *testing.T) {
	names := []string{"老手级杰出火焰法杖", "专家级杰出火焰法杖", "大师级杰出火焰法杖", "Vendetta之怒"}
	if got := LongestCommonSuffix(names, 0.7); got != "杰出火焰法杖" {
		t.Fatalf("族名 = %s", got)
	}
	if got := LongestCommonSuffix([]string{"专家级弓箭", "大师级弓箭", "新手级弓箭"}, 0.7); got != "弓箭" {
		t.Fatalf("族名 = %s", got)
	}
	if got := LongestCommonSuffix([]string{"细布"}, 0.7); got != "细布" {
		t.Fatalf("单个成员的族名 = %s", got)
	}
}

// 细布/精布/华布 的公共后缀只剩一个"布"字,金属锭更是只剩"条"。
func TestMaterialFamilyFallsBackToLowestTierName(t *testing.T) {
	if got := FamilyLabel([]Item{cloth4, cloth5}); got != "细布" { // 不是"布"
		t.Fatalf("材料族名 = %s", got)
	}
	staffs := []Item{
		{ItemID: "T4_2H_FIRESTAFF", NameZH: "老手级杰出火焰法杖", Family: "2H_FIRESTAFF", Tier: 4, MaxQuality: 5},
		{ItemID: "T5_2H_FIRESTAFF", NameZH: "专家级杰出火焰法杖", Family: "2H_FIRESTAFF", Tier: 5, MaxQuality: 5},
	}
	if got := FamilyLabel(staffs); got != "杰出火焰法杖" {
		t.Fatalf("装备族名 = %s", got)
	}
}

// 附魔变体不在原始 dump 里,分类得靠剥掉 @N 后的本体查。
func TestEnchantedItemInheritsBaseCategory(t *testing.T) {
	item, ok := build(staff).Get("T4_2H_FIRESTAFF@1")
	if !ok {
		t.Fatal("目录里没有这件附魔武器")
	}
	if item.BaseID() != "T4_2H_FIRESTAFF" {
		t.Fatalf("本体 id = %s", item.BaseID())
	}
	if item.Category != "weapons" || item.Subcategory != "firestaff" {
		t.Fatalf("分类 = (%s, %s)", item.Category, item.Subcategory)
	}
	if item.Enchantment != 1 {
		t.Fatalf("附魔等级 = %d", item.Enchantment)
	}
}

func TestMenuTreeOnlyHasCategoriesWithItems(t *testing.T) {
	tree := build(cloth4, cloth5, staff).MenuTree()
	var got []string
	for _, n := range tree {
		got = append(got, n.ID)
	}
	// 顺序照搬游戏内市场
	if !reflect.DeepEqual(got, []string{"weapons", "crafting"}) {
		t.Fatalf("大类顺序 = %v", got)
	}

	var crafting MenuCategory
	for _, n := range tree {
		if n.ID == "crafting" {
			crafting = n
		}
	}
	if crafting.Label != "制作" || crafting.Count != 2 {
		t.Fatalf("制作大类 = %+v", crafting)
	}
	sub := crafting.Subs[0]
	if sub.ID != "refinedresources" || sub.Label != "加工资源" {
		t.Fatalf("子类 = %+v", sub)
	}
	if len(sub.Families) != 1 || sub.Families[0].Key != "CLOTH" {
		t.Fatalf("物品族 = %+v", sub.Families)
	}
	if !reflect.DeepEqual(sub.Families[0].Tiers, []int{4, 5}) {
		t.Fatalf("等级档 = %v", sub.Families[0].Tiers)
	}
}

func TestMaxQualityDiffersBetweenMaterialsAndGear(t *testing.T) {
	c := build(cloth4, staff)
	// 材料只有普通一档,界面据此禁用品质筛选
	if item, _ := c.Get("T4_CLOTH"); item.MaxQuality != 1 {
		t.Fatalf("材料品质档 = %d", item.MaxQuality)
	}
	if item, _ := c.Get("T4_2H_FIRESTAFF@1"); item.MaxQuality != 5 {
		t.Fatalf("装备品质档 = %d", item.MaxQuality)
	}
}
