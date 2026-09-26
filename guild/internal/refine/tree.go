package refine

import (
	"fmt"
	"sort"

	"albion-guild/internal/catalog"
)

// 配方树(GET /api/refine/recipe):给界面做提示、也方便排查「为什么和游戏对不上」。
// 只讲配方和转化格子,不看价格;转化的单价按调用方给的 k 和主人费率算(B5、B6)。

// TreeInput 是配方里的一项材料。
type TreeInput struct {
	Item string `json:"item"`
	// Kind:raw 原料 | refined 下级料 | heart 心
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	// Returns 是 dump 上这项材料参不参与返还(A6);心实际返不返还还要看 heart_returns 开关(A7)
	Returns bool `json:"returns"`
}

// TreeRecipe 是一条精炼配方。
type TreeRecipe struct {
	Variant int         `json:"variant"`
	Heart   bool        `json:"heart"`
	Focus   int         `json:"focus"` // 基础专注(D1),实际消耗还要按 FCE 折算(D2)
	Silver  int64       `json:"silver"`
	Amount  int         `json:"amount"`
	Inputs  []TreeInput `json:"inputs"`
}

// TreeNode 是链上的一级半成品。
type TreeNode struct {
	Item    string `json:"item"`
	Tier    int    `json:"tier"`
	Enchant int    `json:"enchant"`
	// ItemValue 是产物 IV,站费按它算(E1、E2)
	ItemValue float64 `json:"item_value"`
	// Makeable:T4 起才自炼,T3 及以下只当链底的输入、只能买(设计 §2.1)
	Makeable bool         `json:"makeable"`
	Recipes  []TreeRecipe `json:"recipes"`
	Lower    string       `json:"lower,omitempty"`
}

// TreeEdge 是一条转化边:1 个 From 换 1 个 To(B3)。
type TreeEdge struct {
	From   string  `json:"from"`
	To     string  `json:"to"`
	Silver int64   `json:"silver"` // 基础银币(B4)
	ToIV   float64 `json:"to_iv"`  // 产物 IV,主人费用按它算(B6)
	// Unit 是按这次的 k、主人费率算的单次实付(B5)
	Unit float64 `json:"unit"`
}

// TreeRoute 是从某一档原料一路转化到目标原料的最便宜走法(不看价格,只算转化费)。
type TreeRoute struct {
	Start string   `json:"start"`
	Path  []string `json:"path"`
	Unit  float64  `json:"unit"` // 每件从 Start 转到目标的转化费合计
}

// TreeRaw 是一种要用到的原料,以及能转化到它的全部起点。
type TreeRaw struct {
	Item   string      `json:"item"`
	Routes []TreeRoute `json:"routes"`
}

// Tree 是一个物品的配方树。
type Tree struct {
	Item string `json:"item"`
	Line string `json:"line"`
	// Kind:refined(半成品,有精炼链)| raw(原料,只有转化格子)
	Kind string `json:"kind"`
	// Specialty 是这条线的精炼专精城(C3)
	Specialty string     `json:"specialty"`
	Chain     []TreeNode `json:"chain"`
	Raws      []TreeRaw  `json:"raws"`
	// Edges 是转化格子:Raws 里每种原料的全部起点之间的转化边,去重,按产物从高到低排
	Edges                  []TreeEdge `json:"edges"`
	TransmuteFactor        float64    `json:"transmute_factor"`
	TransmuteStationPer100 float64    `json:"transmute_station_per100"`
	RecipeSource           string     `json:"recipe_source"`
	RecipeSyncedAt         string     `json:"recipe_synced_at"`
}

// RecipeTree 建 item 的配方树。item 要是四条线 T2~T8 的原料或半成品,否则返回 error。
// k、feePer100 是算转化单价用的(调用方从请求或配置取)。
func RecipeTree(recipes *catalog.RecipeBook, item string, k, feePer100 float64) (*Tree, error) {
	family, tier, enchant, ok := ParseItemID(item)
	line, isLine := LineOf(family)
	// 附魔 .0~.4、id 要是规范写法(T8_ORE_LEVEL7@7、T8_ORE_LEVEL0@0 都不是),见 validate
	if !ok || !isLine || tier < MinTier || tier > MaxTier || (enchant > 0 && tier < MinEnchantTier) ||
		enchant > MaxEnchant || ItemID(family, tier, enchant) != item {
		return nil, fmt.Errorf("%q 不是矿/皮/纤维/木四条线 T2~T8(附魔 .0~.4)的原料或半成品", item)
	}
	if err := CheckTransmuteFactor(k); err != nil {
		return nil, err
	}
	if err := CheckStationFee(feePer100); err != nil {
		return nil, fmt.Errorf("转化主人费率: %w", err)
	}
	g := newGraph(recipes)
	t := &Tree{
		Item: item, Line: line.Key, Specialty: line.City,
		Chain: []TreeNode{}, Raws: []TreeRaw{}, Edges: []TreeEdge{},
		TransmuteFactor: k, TransmuteStationPer100: feePer100,
		RecipeSource: recipes.Source(), RecipeSyncedAt: recipes.SyncedAt(),
	}
	kindOf := func(id string) string {
		if _, ok := heartOf(id); ok {
			return "heart"
		}
		switch f, _, _, _ := ParseItemID(id); f {
		case line.Raw:
			return "raw"
		case line.Refined:
			return "refined"
		}
		return "other"
	}
	var raws []string
	rawSeen := map[string]bool{}
	if family == line.Raw {
		t.Kind = "raw"
		raws = append(raws, item)
	} else {
		t.Kind = "refined"
		for cur, n := item, 0; cur != "" && n < 16; n++ {
			_, ct, ce, _ := ParseItemID(cur)
			node := TreeNode{Item: cur, Tier: ct, Enchant: ce, Makeable: ct >= MinEnchantTier, Recipes: []TreeRecipe{}}
			next := ""
			for _, r := range g.refineRecipes(cur) {
				node.ItemValue = r.ItemValue
				tr := TreeRecipe{Variant: r.Variant, Heart: usesHeart(r), Focus: r.Focus, Silver: r.Silver,
					Amount: r.Amount, Inputs: []TreeInput{}}
				for _, in := range r.Inputs {
					kind := kindOf(in.ItemID)
					tr.Inputs = append(tr.Inputs, TreeInput{Item: in.ItemID, Kind: kind, Count: in.Count, Returns: in.Returns()})
					switch kind {
					case "refined":
						next = in.ItemID
					case "raw":
						// 只有要自炼的那几级的原料才需要来路;链底只买,不展开
						if node.Makeable && !rawSeen[in.ItemID] {
							rawSeen[in.ItemID] = true
							raws = append(raws, in.ItemID)
						}
					}
				}
				node.Recipes = append(node.Recipes, tr)
			}
			node.Lower = next
			t.Chain = append(t.Chain, node)
			if !node.Makeable {
				break
			}
			cur = next
		}
	}

	edgeSeen := map[[2]string]bool{}
	for _, raw := range raws {
		routes := g.routesTo(raw, k, feePer100)
		tr := TreeRaw{Item: raw, Routes: []TreeRoute{}}
		starts := make([]string, 0, len(routes))
		for s := range routes {
			starts = append(starts, s)
		}
		sort.Strings(starts)
		for _, s := range starts {
			tr.Routes = append(tr.Routes, TreeRoute{Start: s, Path: pathOf(routes, s), Unit: routes[s].cost})
			// 转化格子:转进这一档的每一条边(不只最便宜路线上的那几条)
			for _, e := range g.transIn[s] {
				key := [2]string{e.from, e.to}
				if _, in := routes[e.from]; !in || edgeSeen[key] {
					continue
				}
				edgeSeen[key] = true
				t.Edges = append(t.Edges, TreeEdge{From: e.from, To: e.to, Silver: e.silver, ToIV: e.toIV,
					Unit: TransmuteCost(e.silver, k, e.toIV, feePer100)})
			}
		}
		sort.SliceStable(tr.Routes, func(i, j int) bool {
			if tr.Routes[i].Unit != tr.Routes[j].Unit {
				return tr.Routes[i].Unit < tr.Routes[j].Unit
			}
			return tr.Routes[i].Start < tr.Routes[j].Start
		})
		t.Raws = append(t.Raws, tr)
	}
	sort.Slice(t.Edges, func(i, j int) bool {
		a, b := t.Edges[i], t.Edges[j]
		if levelSum(a.To) != levelSum(b.To) {
			return levelSum(a.To) > levelSum(b.To)
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.From < b.From
	})
	return t, nil
}
