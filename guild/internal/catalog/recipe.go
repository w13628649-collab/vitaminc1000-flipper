package catalog

import (
	"encoding/json"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RecipeKind 是配方的种类,由 dump 里 craftingrequirements 的
// @craftbuttonlocaoverride 决定——游戏自己就是靠它在站里分「精炼」和「转化」两个按钮。
type RecipeKind string

const (
	RecipeRefine    RecipeKind = "refine"    // 原料 → 半成品
	RecipeTransmute RecipeKind = "transmute" // 原料升一级附魔或一级 tier
)

// recipeButtons 只收这两种按钮。装备制造、兑换、合成之类这一期不做,
// 也不进库——收进来没人用,每次同步还要整份替换。
var recipeButtons = map[string]RecipeKind{
	"@CRAFTBUILDING_ITEM_DETAILS_BUTTON_REFINE":    RecipeRefine,
	"@CRAFTBUILDING_ITEM_DETAILS_BUTTON_TRANSMUTE": RecipeTransmute,
}

// RecipeInput 是配方里的一项材料。
type RecipeInput struct {
	// ItemID 是市场 id(T8_ORE_LEVEL4@4),和挂单、AODP 用的一致
	ItemID string `json:"item"`
	Count  int    `json:"count"`
	// MaxReturn 是 craftresource 的 @maxreturnamount 原值。nil 表示 dump 里没写:
	// 精炼的原料和下级料都没写,按返还率正常返还(规则表 A6);转化的输入显式写了 0,
	// 不返还(B3)。心也没写,但它返不返还没核实(A7),由上层的开关决定
	MaxReturn *int `json:"max_return,omitempty"`
}

// Returns 说明这项材料按 dump 参不参与返还。
func (in RecipeInput) Returns() bool { return in.MaxReturn == nil || *in.MaxReturn > 0 }

// Recipe 是一个物品的一条配方。同一个物品可能有好几条:T4~T8 .0~.3 的半成品
// 另有一条用心的配方,T5 以上的附魔原料能从两个方向转化上来。
type Recipe struct {
	// ItemID 是产物的市场 id
	ItemID string `json:"item"`
	// Variant 是这条配方在 dump 里是该物品的第几条 craftingrequirements(从 0 起)
	Variant int        `json:"variant"`
	Kind    RecipeKind `json:"kind"`
	// Silver 是配方要的银币。精炼是 0;转化是「基础价」,游戏实付还要乘 k、
	// 加主人费用(规则表 B4、B5)。.4 半成品的配方里干脆没写这个属性,按 0 算
	Silver int64 `json:"silver"`
	// Focus 是开专注时每次扣的基础专注(D1),转化是 0
	Focus int `json:"focus"`
	// Amount 是每次产出几件,没写按 1
	Amount int `json:"amount"`
	// ItemValue 是**产物**的物品价值(IV),站费和转化主人费用都按它算(E1、B6)
	ItemValue float64 `json:"item_value"`
	// UnlockedToCraft 是产物的 @unlockedtocraft:为 true 的不用在命运之轮解锁。
	// 原料都带它,所以转化不用解锁;T3 以上的半成品没有,要先解锁精炼节点(A10)
	UnlockedToCraft bool          `json:"unlocked_to_craft"`
	Inputs          []RecipeInput `json:"inputs"`
}

// levelSuffixRE 认 dump id 末尾的 _LEVELn。附魔资源在 items.json 里是独立条目,
// 附魔等级就写在名字里(T8_ORE_LEVEL4)。
var levelSuffixRE = regexp.MustCompile(`_LEVEL(\d+)$`)

// MarketID 把 items.json 里的 id 换成市场 / AODP / 抓包用的 id。
//
// 附魔资源在 dump 里叫 T8_ORE_LEVEL4,市场上叫 T8_ORE_LEVEL4@4;无附魔的两边一样。
// enchant 是 dump 里的 @enchantmentlevel;调用方拿不到时传 -1,按名字末尾的 _LEVELn 推。
func MarketID(dumpID string, enchant int) string {
	if strings.Contains(dumpID, "@") {
		return dumpID
	}
	if enchant < 0 {
		enchant = 0
		if m := levelSuffixRE.FindStringSubmatch(dumpID); m != nil {
			enchant, _ = strconv.Atoi(m[1])
		}
	}
	if enchant <= 0 {
		return dumpID
	}
	return dumpID + "@" + strconv.Itoa(enchant)
}

// DumpID 是 MarketID 的反方向:剥掉 @ 后缀。
func DumpID(marketID string) string {
	base, _, _ := strings.Cut(marketID, "@")
	return base
}

// ParseRecipes 从原始 items.json 里抽精炼和转化配方。
//
// 同步走的是 Fetch(和物品目录同一次下载);这个读调用方给的 reader,
// 给测试和重新生成内置快照用,自己不联网。
func ParseRecipes(r io.Reader) ([]Recipe, error) {
	var payload struct {
		Items map[string]json.RawMessage `json:"items"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil, err
	}
	_, recipes := parseRawItems(payload.Items)
	return recipes, nil
}

// recipesOf 抽一个物品的精炼 / 转化配方。row 是 dump 里的一个物品节点。
func recipesOf(row map[string]any) []Recipe {
	dumpID, _ := row["@uniquename"].(string)
	reqs := asObjects(row["craftingrequirements"])
	if dumpID == "" || len(reqs) == 0 {
		return nil
	}
	id := MarketID(dumpID, attrInt(row, "@enchantmentlevel", -1))
	itemValue := attrFloat(row, "@itemvalue", 0)
	unlocked := attrString(row, "@unlockedtocraft") == "true"

	var out []Recipe
	for variant, req := range reqs {
		kind, ok := recipeButtons[attrString(req, "@craftbuttonlocaoverride")]
		if !ok {
			continue
		}
		var inputs []RecipeInput
		for _, res := range asObjects(req["craftresource"]) {
			inID := attrString(res, "@uniquename")
			if inID == "" {
				// 缺名字的材料没法算账,整条配方宁可不要,也不要少算一项材料
				inputs = nil
				break
			}
			in := RecipeInput{
				ItemID: MarketID(inID, attrInt(res, "@enchantmentlevel", -1)),
				Count:  attrInt(res, "@count", 1),
			}
			if _, has := res["@maxreturnamount"]; has {
				n := attrInt(res, "@maxreturnamount", 0)
				in.MaxReturn = &n
			}
			inputs = append(inputs, in)
		}
		if len(inputs) == 0 {
			continue
		}
		out = append(out, Recipe{
			ItemID:          id,
			Variant:         variant,
			Kind:            kind,
			Silver:          int64(math.Round(attrFloat(req, "@silver", 0))),
			Focus:           int(math.Round(attrFloat(req, "@craftingfocus", 0))),
			Amount:          attrInt(req, "@amountcrafted", 1),
			ItemValue:       itemValue,
			UnlockedToCraft: unlocked,
			Inputs:          inputs,
		})
	}
	return out
}

// SortRecipes 按 (产物, variant) 排序。入库、快照、比对都靠它得到确定的顺序。
func SortRecipes(recipes []Recipe) {
	sort.Slice(recipes, func(i, j int) bool {
		if recipes[i].ItemID != recipes[j].ItemID {
			return recipes[i].ItemID < recipes[j].ItemID
		}
		return recipes[i].Variant < recipes[j].Variant
	})
}

// asObjects:dump 是 XML 转的 JSON,只有一个子节点时是对象、多个时是数组,两种都认。
func asObjects(v any) []map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return []map[string]any{x}
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func attrString(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// attrFloat:dump 的属性都是字符串("409.6"),个别转换器会给成数字,两种都认。
// 缺失或解析不了就用 def。
func attrFloat(m map[string]any, key string, def float64) float64 {
	switch v := m[key].(type) {
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	case float64:
		return v
	}
	return def
}

func attrInt(m map[string]any, key string, def int) int {
	if _, ok := m[key]; !ok {
		return def
	}
	return int(math.Round(attrFloat(m, key, float64(def))))
}

// RecipeBook 是内存里的配方表,按产物索引。发布之后不再改,可以并发读。
type RecipeBook struct {
	byItem   map[string][]Recipe
	all      []Recipe
	syncedAt string
	source   string
}

// 配方表的来源,排查「为什么和游戏对不上」时先看它。
const (
	RecipesFromDB       = "db"       // 上次同步写进库的
	RecipesFromUpstream = "upstream" // 刚从 ao-bin-dumps 拉的
)

// NewRecipeBook 建索引。source 是上面几个常量之一,或者内置快照的名字。
func NewRecipeBook(recipes []Recipe, syncedAt, source string) *RecipeBook {
	all := append([]Recipe(nil), recipes...)
	SortRecipes(all)
	byItem := make(map[string][]Recipe)
	for _, r := range all {
		byItem[r.ItemID] = append(byItem[r.ItemID], r)
	}
	return &RecipeBook{byItem: byItem, all: all, syncedAt: syncedAt, source: source}
}

// Of 是一个产物的全部配方,按 variant 排好。nil 的 RecipeBook 也能调,返回空。
func (b *RecipeBook) Of(itemID string) []Recipe {
	if b == nil {
		return nil
	}
	return b.byItem[itemID]
}

// OfKind 只要某一种配方。
func (b *RecipeBook) OfKind(itemID string, kind RecipeKind) []Recipe {
	var out []Recipe
	for _, r := range b.Of(itemID) {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

// All 是全部配方,按 (产物, variant) 排好。返回的是内部切片,调用方不要改。
func (b *RecipeBook) All() []Recipe {
	if b == nil {
		return nil
	}
	return b.all
}

func (b *RecipeBook) Len() int {
	if b == nil {
		return 0
	}
	return len(b.all)
}

func (b *RecipeBook) SyncedAt() string {
	if b == nil {
		return ""
	}
	return b.syncedAt
}

func (b *RecipeBook) Source() string {
	if b == nil {
		return ""
	}
	return b.source
}
