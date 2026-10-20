package refine

import (
	"slices"
	"testing"
)

// 心配方:一个心替掉一个原料(.0~.3 才有)。心多半只在一两座城有人挂,
// 以前只在买料城市里找,找不到就悄悄跳过,界面上也不提 —— 用户以为「没做这条路」(2026-10-20)。

func heartStepQty(res *Result, heart string) (qty, transport float64, city string) {
	for _, s := range res.Candidates[0].Steps {
		if s.Op == "buy" && s.Item == heart {
			qty += s.Qty
			transport += s.Transport
			city = s.City
		}
	}
	return
}

func TestHeart_没价时不用也不算缺价_手填价让它胜出(t *testing.T) {
	l := Lines[0]
	target := l.RefinedID(5, 1)
	books := synthBooks(l, l.City, 7)
	p := params(target)
	res, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	h := res.Heart
	if h == nil || !h.HasRecipe || h.BestPrice != nil || h.Used != 0 {
		t.Fatalf("哪里都没有心、也没手填:有心配方、没有价、没用,得到 %+v", h)
	}
	if slices.Contains(res.Data.Missing, l.Heart) {
		t.Fatal("心是可选的输入,没价不该进缺价清单")
	}

	p.Fees.HeartPrice = 1 // 比任何原料都便宜:心配方一定胜出
	res, err = Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	h = res.Heart
	if h.BestPrice == nil || *h.BestPrice != 1 || h.BestSource != heartSourceManual || h.Used <= 0 {
		t.Fatalf("手填 1 银:最便宜的是手填那档、推荐路线要用心,得到 %+v", h)
	}
	if q, _, _ := heartStepQty(res, l.Heart); q != h.Used {
		t.Fatalf("Used %g 和步骤清单里买心的件数 %g 对不上", h.Used, q)
	}
}

func TestHeart_心可以在买料城市以外买_不加运费(t *testing.T) {
	l := Lines[0]
	target := l.RefinedID(5, 1)
	books := synthBooks(l, l.City, 7)
	books[Key{l.Heart, "Caerleon"}] = Book{Asks: askSide(1, 500)}
	p := params(target)
	p.Sources.TransportPct = 0.5

	// 老行为(heart_cities 不填 = 和买料城市一样):Caerleon 的心看不到
	res, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Heart.BestPrice != nil || res.Heart.Used != 0 {
		t.Fatalf("心只在买料城市里找时 Caerleon 的心不该被用上:%+v", res.Heart)
	}

	p.Sources.HeartCities = []string{l.City, "Caerleon"}
	res, err = Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	h := res.Heart
	if h.BestPrice == nil || h.BestCity != "Caerleon" || h.BestSource != "capture" || h.Used <= 0 || h.BestAgeH == nil {
		t.Fatalf("Caerleon 有 1 银的心:要找到、要用上、带年龄,得到 %+v", h)
	}
	q, transport, city := heartStepQty(res, l.Heart)
	if q <= 0 || city != "Caerleon" || transport != 0 {
		t.Fatalf("买心那一步要在 Caerleon、不加运费:件数 %g、城 %q、运费 %g", q, city, transport)
	}
}

func TestHeart_点4链没有心配方也不报心价(t *testing.T) {
	l := Lines[0]
	target := l.RefinedID(8, 4)
	p := params(target)
	p.Fees.HeartPrice = 1
	res, err := Plan(Input{Params: p, Books: synthBooks(l, l.City, 7), Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	if h := res.Heart; h == nil || h.HasRecipe || h.BestPrice != nil || h.Used != 0 {
		t.Fatalf(".4 档没有心配方:不报心价、不用心,得到 %+v", h)
	}
}

func TestHeart_手填价校验(t *testing.T) {
	for _, v := range []float64{-1, 1e7} {
		p := params(Lines[0].RefinedID(5, 1))
		p.Fees.HeartPrice = v
		if err := Validate(p); err == nil {
			t.Errorf("心的单价 %g 应该被拒", v)
		}
	}
}
