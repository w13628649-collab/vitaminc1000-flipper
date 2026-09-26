package catalog

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// fixtureItemsJSON 是从 items.json(ao-bin-dumps,和 90b141e 的 items.xml 逐项一致)
// 里原样截的几个节点,只删了和配方无关的属性。覆盖:
//   - T8_METALBAR_LEVEL4:.4 半成品,只有一条配方,**没写 @silver**
//   - T4_METALBAR_LEVEL1:T4 附魔半成品,两条配方,第二条用心;下级料是 T3(.0)
//   - T8_ORE_LEVEL4:两条转化配方(数组),输入显式 maxreturnamount=0
//   - T4_ORE_LEVEL1:只有一条转化配方,craftingrequirements 是对象不是数组
//   - 山脉之心:配方是阵营点兑换和黑心转换,不是精炼/转化,不收
//   - 火焰法杖:装备制造配方(没有按钮覆盖),不收。这一个是按结构简化过的
const fixtureItemsJSON = `{"items": {
 "simpleitem": [
  {"@uniquename": "T8_METALBAR_LEVEL4", "@tier": "8", "@enchantmentlevel": "4",
   "@shopcategory": "crafting", "@shopsubcategory1": "refinedresources", "@itemvalue": "4096",
   "craftingrequirements": {"@craftingfocus": "4714", "@time": "0.27", "@amountcrafted": "1",
    "@craftbuttonlocaoverride": "@CRAFTBUILDING_ITEM_DETAILS_BUTTON_REFINE",
    "craftresource": [
     {"@uniquename": "T8_ORE_LEVEL4", "@enchantmentlevel": "4", "@count": "5"},
     {"@uniquename": "T7_METALBAR_LEVEL4", "@enchantmentlevel": "4", "@count": "1"}]}},
  {"@uniquename": "T4_METALBAR_LEVEL1", "@shopcategory": "crafting", "@shopsubcategory1": "refinedresources",
   "@tier": "4", "@enchantmentlevel": "1", "@itemvalue": "32",
   "craftingrequirements": [
    {"@silver": "0", "@time": "0.0625", "@craftingfocus": "94", "@amountcrafted": "1",
     "@craftbuttonlocaoverride": "@CRAFTBUILDING_ITEM_DETAILS_BUTTON_REFINE",
     "craftresource": [
      {"@uniquename": "T4_ORE_LEVEL1", "@enchantmentlevel": "1", "@count": "2"},
      {"@uniquename": "T3_METALBAR", "@count": "1", "@enchantmentlevel": "0"}]},
    {"@silver": "0", "@time": "0.0625", "@craftingfocus": "94", "@amountcrafted": "1",
     "@craftbuttonlocaoverride": "@CRAFTBUILDING_ITEM_DETAILS_BUTTON_REFINE",
     "craftresource": [
      {"@uniquename": "T4_ORE_LEVEL1", "@enchantmentlevel": "1", "@count": "1"},
      {"@uniquename": "T1_FACTION_MOUNTAIN_TOKEN_1", "@count": "1"},
      {"@uniquename": "T3_METALBAR", "@count": "1", "@enchantmentlevel": "0"}]}]},
  {"@uniquename": "T8_ORE_LEVEL4", "@tier": "8", "@enchantmentlevel": "4",
   "@shopcategory": "crafting", "@shopsubcategory1": "resources", "@itemvalue": "409.6",
   "@unlockedtocraft": "true",
   "craftingrequirements": [
    {"@silver": "779625", "@time": "0.027",
     "@craftbuttonlocaoverride": "@CRAFTBUILDING_ITEM_DETAILS_BUTTON_TRANSMUTE",
     "craftresource": {"@uniquename": "T7_ORE_LEVEL4", "@enchantmentlevel": "4", "@count": "1", "@maxreturnamount": "0"}},
    {"@silver": "748440", "@time": "0.027",
     "@craftbuttonlocaoverride": "@CRAFTBUILDING_ITEM_DETAILS_BUTTON_TRANSMUTE",
     "craftresource": {"@uniquename": "T8_ORE_LEVEL3", "@enchantmentlevel": "3", "@count": "1", "@maxreturnamount": "0"}}]},
  {"@uniquename": "T4_ORE_LEVEL1", "@shopcategory": "crafting", "@shopsubcategory1": "resources",
   "@tier": "4", "@enchantmentlevel": "1", "@unlockedtocraft": "true", "@itemvalue": "12",
   "craftingrequirements": {"@silver": "1500", "@time": "0.00625", "@craftingfocus": "0",
    "@craftbuttonlocaoverride": "@CRAFTBUILDING_ITEM_DETAILS_BUTTON_TRANSMUTE",
    "craftresource": {"@uniquename": "T4_ORE", "@count": "1", "@maxreturnamount": "0", "@enchantmentlevel": "0"}}},
  {"@uniquename": "T1_FACTION_MOUNTAIN_TOKEN_1", "@tier": "1", "@unlockedtocraft": "true",
   "@shopcategory": "crafting", "@shopsubcategory1": "cityresources",
   "craftingrequirements": [
    {"@time": "0", "@craftingfocus": "0", "currency": {"@uniquename": "FACTION_MOUNTAIN", "@amount": "3000"}},
    {"@silver": "5000", "@time": "0", "@craftingfocus": "0",
     "craftresource": {"@uniquename": "T1_FACTION_CAERLEON_TOKEN_1", "@count": "1"}}]}
 ],
 "weapon": [
  {"@uniquename": "T4_2H_FIRESTAFF", "@maxqualitylevel": "5", "@shopcategory": "weapons",
   "@shopsubcategory1": "firestaff", "@tier": "4",
   "craftingrequirements": {"@silver": "0", "@time": "1", "@craftingfocus": "393",
    "craftresource": [{"@uniquename": "T4_PLANKS", "@count": "16"}, {"@uniquename": "T4_METALBAR", "@count": "8"}]}}
 ]
}}`

func intp(n int) *int { return &n }

func TestParseRecipes夹具(t *testing.T) {
	got, err := ParseRecipes(strings.NewReader(fixtureItemsJSON))
	if err != nil {
		t.Fatal(err)
	}
	want := []Recipe{
		{ItemID: "T4_METALBAR_LEVEL1@1", Variant: 0, Kind: RecipeRefine, Focus: 94, Amount: 1, ItemValue: 32,
			Inputs: []RecipeInput{{ItemID: "T4_ORE_LEVEL1@1", Count: 2}, {ItemID: "T3_METALBAR", Count: 1}}},
		// 心配方:原料少一个,换成一颗心;下级料照旧
		{ItemID: "T4_METALBAR_LEVEL1@1", Variant: 1, Kind: RecipeRefine, Focus: 94, Amount: 1, ItemValue: 32,
			Inputs: []RecipeInput{{ItemID: "T4_ORE_LEVEL1@1", Count: 1},
				{ItemID: "T1_FACTION_MOUNTAIN_TOKEN_1", Count: 1}, {ItemID: "T3_METALBAR", Count: 1}}},
		{ItemID: "T4_ORE_LEVEL1@1", Variant: 0, Kind: RecipeTransmute, Silver: 1500, Amount: 1, ItemValue: 12,
			UnlockedToCraft: true,
			Inputs:          []RecipeInput{{ItemID: "T4_ORE", Count: 1, MaxReturn: intp(0)}}},
		// .4 没写 @silver,按 0;没写 @unlockedtocraft,要先解锁
		{ItemID: "T8_METALBAR_LEVEL4@4", Variant: 0, Kind: RecipeRefine, Focus: 4714, Amount: 1, ItemValue: 4096,
			Inputs: []RecipeInput{{ItemID: "T8_ORE_LEVEL4@4", Count: 5}, {ItemID: "T7_METALBAR_LEVEL4@4", Count: 1}}},
		// 升 tier 和升附魔两个方向;8.3→8.4 是 748,440
		{ItemID: "T8_ORE_LEVEL4@4", Variant: 0, Kind: RecipeTransmute, Silver: 779_625, Amount: 1, ItemValue: 409.6,
			UnlockedToCraft: true,
			Inputs:          []RecipeInput{{ItemID: "T7_ORE_LEVEL4@4", Count: 1, MaxReturn: intp(0)}}},
		{ItemID: "T8_ORE_LEVEL4@4", Variant: 1, Kind: RecipeTransmute, Silver: 748_440, Amount: 1, ItemValue: 409.6,
			UnlockedToCraft: true,
			Inputs:          []RecipeInput{{ItemID: "T8_ORE_LEVEL3@3", Count: 1, MaxReturn: intp(0)}}},
	}
	if !reflect.DeepEqual(got, want) {
		g, _ := json.MarshalIndent(got, "", " ")
		w, _ := json.MarshalIndent(want, "", " ")
		t.Fatalf("解析结果不对\n得到 %s\n想要 %s", g, w)
	}

	// 返还:精炼的材料没写 maxreturnamount → 返还;转化显式写了 0 → 不返还
	if !got[0].Inputs[0].Returns() || !got[0].Inputs[1].Returns() {
		t.Fatal("精炼的原料和下级料应当参与返还(A6)")
	}
	if got[4].Inputs[0].Returns() {
		t.Fatal("转化的输入写了 maxreturnamount=0,不该返还(B3)")
	}
}

// 配方和目录是同一遍解析出来的,加了配方不能把原来的分类、品质档弄坏。
func TestParseRawItems顺手出配方不影响目录(t *testing.T) {
	var payload struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(fixtureItemsJSON), &payload); err != nil {
		t.Fatal(err)
	}
	meta, recipes := parseRawItems(payload.Items)
	if m := meta["T4_2H_FIRESTAFF"]; m.category != "weapons" || m.subcategory != "firestaff" || m.maxQuality != 5 {
		t.Fatalf("法杖分类 = %+v", m)
	}
	if m := meta["T8_ORE_LEVEL4"]; m.category != "crafting" || m.subcategory != "resources" || m.maxQuality != 1 {
		t.Fatalf("8.4 矿分类 = %+v", m)
	}
	if len(recipes) != 6 {
		t.Fatalf("配方 %d 条,想要 6", len(recipes))
	}
}

// T8_ORE_LEVEL4 ↔ T8_ORE_LEVEL4@4:dump 里附魔资源是独立条目,市场上带 @ 后缀。
func TestMarketID和DumpID能来回转(t *testing.T) {
	cases := []struct {
		dump    string
		enchant int
		market  string
	}{
		{"T8_ORE_LEVEL4", 4, "T8_ORE_LEVEL4@4"},
		{"T4_METALBAR_LEVEL1", 1, "T4_METALBAR_LEVEL1@1"},
		{"T8_ORE", 0, "T8_ORE"},
		{"T2_CLOTH", 0, "T2_CLOTH"},
		{"T1_FACTION_MOUNTAIN_TOKEN_1", 0, "T1_FACTION_MOUNTAIN_TOKEN_1"},
		// 拿不到 @enchantmentlevel 时按名字末尾推;心的 _TOKEN_1 不是附魔
		{"T6_PLANKS_LEVEL3", -1, "T6_PLANKS_LEVEL3@3"},
		{"T1_FACTION_SWAMP_TOKEN_1", -1, "T1_FACTION_SWAMP_TOKEN_1"},
	}
	for _, c := range cases {
		if got := MarketID(c.dump, c.enchant); got != c.market {
			t.Fatalf("MarketID(%s, %d) = %s,想要 %s", c.dump, c.enchant, got, c.market)
		}
		if got := DumpID(c.market); got != c.dump {
			t.Fatalf("DumpID(%s) = %s,想要 %s", c.market, got, c.dump)
		}
		if got := MarketID(DumpID(c.market), -1); got != c.market {
			t.Fatalf("来回一圈 %s → %s", c.market, got)
		}
	}
	// 已经是市场 id 的不再加一次后缀
	if got := MarketID("T8_ORE_LEVEL4@4", 4); got != "T8_ORE_LEVEL4@4" {
		t.Fatalf("重复加后缀: %s", got)
	}
}

func TestRecipeBook按产物索引(t *testing.T) {
	recipes, err := ParseRecipes(strings.NewReader(fixtureItemsJSON))
	if err != nil {
		t.Fatal(err)
	}
	// 打乱顺序进去,出来要按 (产物, variant) 排好
	shuffled := []Recipe{recipes[5], recipes[0], recipes[3], recipes[1], recipes[4], recipes[2]}
	book := NewRecipeBook(shuffled, "2026-09-23T00:00:00Z", RecipesFromDB)
	if !reflect.DeepEqual(book.All(), recipes) {
		t.Fatal("All() 没按 (产物, variant) 排序")
	}
	if got := book.OfKind("T8_ORE_LEVEL4@4", RecipeTransmute); len(got) != 2 || got[1].Silver != 748_440 {
		t.Fatalf("8.4 矿转化配方 = %+v", got)
	}
	if got := book.OfKind("T8_METALBAR_LEVEL4@4", RecipeTransmute); len(got) != 0 {
		t.Fatalf("半成品不该有转化配方: %+v", got)
	}
	if got := book.Of("T8_METALBAR_LEVEL4"); len(got) != 0 {
		t.Fatal("索引用的是市场 id,dump id 不该查得到")
	}
	if book.Len() != 6 || book.Source() != RecipesFromDB || book.SyncedAt() == "" {
		t.Fatalf("元信息 len=%d source=%s synced=%s", book.Len(), book.Source(), book.SyncedAt())
	}

	var none *RecipeBook
	if none.Len() != 0 || none.Of("T8_ORE") != nil || none.All() != nil || none.Source() != "" {
		t.Fatal("nil 配方表应当安全返回空")
	}
}
