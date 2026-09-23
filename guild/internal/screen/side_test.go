package screen

import (
	"encoding/json"
	"math"
	"slices"
	"testing"

	"albion-guild/internal/aodp"
	"albion-guild/internal/depth"
	"albion-guild/internal/econ"
)

// Evaluate 就是两边都走 AODP 的 EvaluateSides。零值 Sides 的输出必须和它逐字相同,
// 否则"capture.enabled=false 时和以前一样"这句话就不成立
func TestEvaluateSides_零值Sides与Evaluate逐字相同(t *testing.T) {
	cases := []struct {
		name string
		rec  aodp.PriceRecord
	}{
		{"正常机会", makePrice(priceOpt{})},
		{"偏离度拒绝", makePrice(priceOpt{sell: 22_000})},
		{"过期拒绝", makePrice(priceOpt{sellAgeH: 7})},
		{"单边拒绝", func() aodp.PriceRecord { r := makePrice(priceOpt{}); r.BuyPriceMax = 0; return r }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o1, r1 := Evaluate(tc.rec, makeStats(statsOpt{}), config(), names, now)
			o2, r2 := EvaluateSides(tc.rec, Sides{}, makeStats(statsOpt{}), config(), names, now)
			a, _ := json.Marshal([]any{o1, r1})
			b, _ := json.Marshal([]any{o2, r2})
			if string(a) != string(b) {
				t.Fatalf("输出不同\nEvaluate      %s\nEvaluateSides %s", a, b)
			}
		})
	}
}

func TestEvaluateSides_零值两边补成AODP并带数据龄(t *testing.T) {
	opp, rej := EvaluateSides(makePrice(priceOpt{sellAgeH: 2, buyAgeH: 1}), Sides{}, makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("本该通过,却被 %s 拒了", rej.Reason)
	}
	if opp.Ask.Source != SourceAODP || opp.Ask.Price != 1200 || math.Abs(opp.Ask.AgeHours-2) > 1e-9 {
		t.Fatalf("ask 应为 aodp / 1200 / 2h,得到 %+v", opp.Ask)
	}
	if opp.Bid.Source != SourceAODP || opp.Bid.Price != 1000 || math.Abs(opp.Bid.AgeHours-1) > 1e-9 {
		t.Fatalf("bid 应为 aodp / 1000 / 1h,得到 %+v", opp.Bid)
	}
	if opp.BuyLegSource != SourceAODP || opp.SellLegSource != SourceAODP {
		t.Fatalf("两条腿都应是 aodp,得到 %s / %s", opp.BuyLegSource, opp.SellLegSource)
	}
}

// 挂买排在买单簿上、挂卖排在卖单簿上。卖方来自抓包、买方来自 AODP 时,
// 挂买挂卖这笔的买入腿是 AODP 的数、卖出腿是抓包的数
func TestEvaluateSides_腿按选中模式映射到两边(t *testing.T) {
	sides := Sides{
		Ask: Side{Source: SourceCapture, Price: 1200, AgeHours: 0.2,
			Depth: &DepthView{Support: depth.Support{Best: 1200, QtyNear: 50}}, Note: "测试备注"},
	}
	opp, rej := EvaluateSides(makePrice(priceOpt{}), sides, makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("本该通过,却被 %s 拒了", rej.Reason)
	}
	if opp.Mode != "maker-maker" {
		t.Fatalf("前提变了:这组夹具应选挂买挂卖,得到 %s", opp.Mode)
	}
	if opp.BuyLegSource != SourceAODP || math.Abs(opp.BuyLegAgeHours-1) > 1e-9 {
		t.Fatalf("挂买腿依托 bid(AODP, 1h),得到 %s / %v", opp.BuyLegSource, opp.BuyLegAgeHours)
	}
	if opp.SellLegSource != SourceCapture || opp.SellLegAgeHours != 0.2 {
		t.Fatalf("挂卖腿依托 ask(抓包, 0.2h),得到 %s / %v", opp.SellLegSource, opp.SellLegAgeHours)
	}
	if opp.Ask.Depth == nil || opp.Ask.Depth.QtyNear != 50 {
		t.Fatalf("抓包那边的深度应原样带出,得到 %+v", opp.Ask)
	}
	if !slices.Contains(opp.Hints, "测试备注") {
		t.Fatalf("Side.Note 应并进 hints,得到 %v", opp.Hints)
	}
	if !slices.Contains(opp.Hints, "买方深度未知:列表页只抓卖单,点进物品详情页才抓得到买单") {
		t.Fatalf("卖方抓包、买方不是时应提示去详情页,得到 %v", opp.Hints)
	}
	// 本步不加判据:置信度和纯 AODP 时一样
	if opp.Confidence != High {
		t.Fatalf("置信度 = %s,想要 high", opp.Confidence)
	}
}

func TestLegSides_四种模式(t *testing.T) {
	s := Sides{Ask: Side{Source: "A"}, Bid: Side{Source: "B"}}
	for _, tc := range []struct {
		m         econ.Mode
		buy, sell string
	}{
		{econ.Mode{Buy: econ.Taker, Sell: econ.Taker}, "A", "B"},
		{econ.Mode{Buy: econ.Taker, Sell: econ.Maker}, "A", "A"},
		{econ.Mode{Buy: econ.Maker, Sell: econ.Taker}, "B", "B"},
		{econ.Mode{Buy: econ.Maker, Sell: econ.Maker}, "B", "A"},
	} {
		b, l := legSides(tc.m, s)
		if b.Source != tc.buy || l.Source != tc.sell {
			t.Fatalf("%s:买入腿/卖出腿应为 %s/%s,得到 %s/%s", tc.m.Key(), tc.buy, tc.sell, b.Source, l.Source)
		}
	}
}

func TestEvaluateSides_拒绝带品质和两边来源(t *testing.T) {
	rec := makePrice(priceOpt{sell: 22_000})
	rec.Quality = 3
	_, rej := EvaluateSides(rec, Sides{Ask: Side{Source: SourceCapture, Price: 22_000}},
		makeStats(statsOpt{}), config(), names, now)
	if rej == nil || rej.Reason != "deviation" {
		t.Fatalf("应被偏离度拒绝,得到 %+v", rej)
	}
	if rej.Quality != 3 || rej.AskSource != SourceCapture || rej.BidSource != SourceAODP {
		t.Fatalf("拒绝应带品质 3、ask=capture、bid=aodp,得到 %+v", rej)
	}
}

func TestSideCaptured(t *testing.T) {
	d := &DepthView{Support: depth.Support{Best: 100}}
	for _, tc := range []struct {
		s    Side
		want bool
	}{
		{Side{Source: SourceCapture, Depth: d}, true},
		{Side{Source: SourceCapture}, false},                                     // 深度太旧没参与
		{Side{Source: SourceAODP, Depth: d}, false},                              // AODP 一律不算
		{Side{Source: SourceCapture, Depth: &DepthView{}}, false},                // 空阶梯
		{Side{Source: SourceCapture, Depth: &DepthView{Truncated: true}}, false}, // 同上
	} {
		if got := tc.s.Captured(); got != tc.want {
			t.Fatalf("%+v: Captured()=%v,想要 %v", tc.s, got, tc.want)
		}
	}
}
