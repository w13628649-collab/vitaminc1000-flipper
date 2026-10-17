package refine

import (
	"math"
	"testing"
)

// breakevenBooks:T5.4 条只能「T5.4 矿 + 买现成的 T4.4 条」一条路(没有别的附魔的矿可转、
// 没有 T4.4 矿和 T3 条可自炼 T4.4 条),矿价整体平移时引擎不会换路线,正好拿来代回去验算。
func breakevenBooks(oreShift int64) Books {
	b := Books{}
	setAsks(b, "T5_ORE_LEVEL4@4", "Thetford", 1000+oreShift, 10, 1100+oreShift, 200)
	setAsks(b, "T4_METALBAR_LEVEL4@4", "Thetford", 5000, 100)
	setAsks(b, "T5_METALBAR_LEVEL4@4", "Thetford", 30000, 5)
	return b
}

func findBreakeven(t *testing.T, c Candidate, item string) Breakeven {
	t.Helper()
	for _, be := range c.Breakeven {
		if be.Item == item {
			return be
		}
	}
	t.Fatalf("%s 的盈亏平衡价里没有 %s: %+v", c.ID, item, c.Breakeven)
	return Breakeven{}
}

// 盈亏平衡价代回去利润为零:把主原料的卖单整体抬高 δ,同样参数重跑,同一条路线的单根利润
// 随 δ 线性下降,斜率 D_f ÷ N,在 x* 处归零。卖单价是整数,所以抬 floor(δ),
// 断言剩下的利润正好是 (δ − floor(δ)) × D_f ÷ N。carry 时余料不计;zero 时余料件数也在 D_f 里。
func TestPlan盈亏平衡价代回去利润为零(t *testing.T) {
	const ore = "T5_ORE_LEVEL4@4"
	for _, mode := range []string{LeftoverCarry, LeftoverZero} {
		p := params("T5_METALBAR_LEVEL4@4")
		p.Focus.Mode = FocusOff
		p.PureStarts = 0
		p.Sources.BidOrders = false
		p.Leftover = mode
		res, err := Plan(Input{Params: p, Books: breakevenBooks(0), Recipes: EmbeddedRecipes()})
		if err != nil {
			t.Fatal(err)
		}
		if res.MainRaw != ore {
			t.Fatalf("main_raw = %q", res.MainRaw)
		}
		c := res.Candidates[0]
		if c.Reference != "" || !c.Filled || c.primary == nil {
			t.Fatalf("%s:第一条应当是拿得满料的普通候选: %+v", mode, c)
		}
		be := findBreakeven(t, c, ore)
		N := float64(p.Qty)

		// 闭式:x* = e_f + N·Unit ÷ D_f,D_f = 买进件数 + [zero] 余料件数
		var q, cost float64
		for _, s := range c.Steps {
			if s.Op == "buy" && s.Item == ore {
				q += s.Qty
				cost += s.Cost
			}
		}
		d := q
		if mode == LeftoverZero {
			for _, lo := range c.Leftover {
				if lo.Item == ore {
					d += lo.Qty
				}
			}
			if d <= q {
				t.Fatal("zero 估值下应当有矿的批末余料")
			}
		}
		assertNear(t, mode+" D_f", be.Qty, d, 1e-9)
		assertNear(t, mode+" e_f", be.Now, cost/q, 1e-9)
		assertNear(t, mode+" x*", be.Unit, cost/q+N*c.primary.Unit/d, 1e-9)
		if be.Never || be.Headroom <= 0 {
			t.Fatalf("%s:这条路线赚钱,还能涨: %+v", mode, be)
		}

		// 代回去
		delta := (be.Unit - be.Now) / MarketFees(p.Fees.economics()).TakeAsk
		shift := math.Floor(delta)
		res2, err := Plan(Input{Params: p, Books: breakevenBooks(int64(shift)), Recipes: EmbeddedRecipes()})
		if err != nil {
			t.Fatal(err)
		}
		c2 := findCandidate(t, res2, c.ID)
		if c2.sig != c.sig {
			t.Fatalf("%s:抬价后路线变了,验算不成立", mode)
		}
		want := (delta - shift) * d / N
		assertNear(t, mode+" 抬到 x* 附近的单根利润", c2.primary.Unit, want, 1e-6*c.UnitCost)
		// 再抬 1 银就亏
		res3, err := Plan(Input{Params: p, Books: breakevenBooks(int64(shift) + 1), Recipes: EmbeddedRecipes()})
		if err != nil {
			t.Fatal(err)
		}
		if u := findCandidate(t, res3, c.ID).primary.Unit; u >= 0 {
			t.Fatalf("%s:越过 x* 还在赚: %f", mode, u)
		}

		// 原料来路表回填:直接买这一行的买价上限 = x* ÷ 买方系数
		rs := res.RawSources[ore]
		if rs.Breakeven == nil || rs.Breakeven.Unit != be.Unit {
			t.Fatalf("%s:raw_sources 没回填盈亏平衡价: %+v", mode, rs.Breakeven)
		}
		for _, pr := range rs.Pure {
			if pr.Start == ore {
				if pr.Route != 0 || pr.MaxPrice == nil {
					t.Fatalf("%s:直接买那一行 %+v", mode, pr)
				}
				assertNear(t, mode+" 买价上限", *pr.MaxPrice, be.Unit, 1e-9)
			}
		}

		// 所有料一起涨 s%:s = N·Unit ÷ Σ(买价 + 运费 [+ 不结转余料的取得成本])
		if c.BreakevenAllPct == nil {
			t.Fatalf("%s:没有 breakeven_all_pct", mode)
		}
		base := 0.0
		for _, s := range c.Steps {
			if s.Op == "buy" || s.Op == "bid" {
				base += s.Cost
			}
		}
		if mode == LeftoverZero {
			for _, lo := range c.Leftover {
				base += lo.Cost
			}
		}
		assertNear(t, mode+" 联合涨幅", *c.BreakevenAllPct, N*c.primary.Unit/base, 1e-9)
	}
}

// 料拿不满时不报盈亏平衡价:成本只是拿得到的那部分,上限没意义。
func TestPlan拿不满料不报盈亏平衡价(t *testing.T) {
	b := Books{}
	setAsks(b, "T5_ORE_LEVEL4@4", "Thetford", 1000, 2)
	setAsks(b, "T4_METALBAR_LEVEL4@4", "Thetford", 5000, 100)
	setAsks(b, "T5_METALBAR_LEVEL4@4", "Thetford", 30000, 5)
	p := params("T5_METALBAR_LEVEL4@4")
	p.Focus.Mode = FocusOff
	res, err := Plan(Input{Params: p, Books: b, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Candidates {
		if !c.Filled && (len(c.Breakeven) != 0 || c.BreakevenAllPct != nil) {
			t.Fatalf("%s 没拿满料,不该有盈亏平衡价: %+v", c.ID, c.Breakeven)
		}
	}
}
