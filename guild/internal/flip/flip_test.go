package flip

import (
	"testing"

	"albion-guild/internal/arb"
	"albion-guild/internal/conf"
	"albion-guild/internal/portfolio"
	"albion-guild/internal/scan"
)

// 从 Thetford 同一个卖单簿往两个方向秒买,盘口上一共只有 11 件。
// 两条路线各自都说"我能买 11 件",组合页要是只记成交量桶(一天 1000 件),
// 就会一天从那 11 件里买走 22 件
func TestPortfolio_同一盘口的深度池两条路线共用(t *testing.T) {
	route := func(to string, depthQty int64) arb.Route {
		return arb.Route{
			ItemID: "T6_METALBAR_LEVEL4@4", ItemName: "T6 钢条 .4", Quality: 1,
			FromCity: "Thetford", ToCity: to, Mode: "taker-taker",
			Qty: 11, CostPerUnit: 200_000, ProfitPerUnit: 49_600, HoursPerTrip: 1,
			SourceDaily: 5000, DestDaily: 5000, BuyDepthQty: depthQty,
		}
	}
	run := func(routes ...arb.Route) portfolio.Plan {
		s := &Service{Cfg: conf.Default()}
		s.last.Store(&scan.Result{Routes: routes})
		return s.Portfolio(portfolio.DefaultOptions(10_000_000))
	}
	total := func(p portfolio.Plan) float64 {
		sum := 0.0
		for _, sl := range p.Slices {
			sum += sl.DailyQty
		}
		return sum
	}

	p := run(route("Martlock", 11), route("Lymhurst", 11))
	if got := total(p); got > 11 {
		t.Fatalf("两条路线一天共从 11 件的盘口里买走 %.0f 件: %+v", got, p.Slices)
	}
	if len(p.Slices) != 1 || p.Slices[0].DailyQty != 11 {
		t.Fatalf("第一条吃满 11 件、第二条没货可吃,得到 %+v", p.Slices)
	}

	// 对照:没按阶梯算的路线(BuyDepthQty=0)只受成交量桶(一天 1000 件)约束,
	// 两条都分得到、合计远超 11 件——证明上面是深度池拦的
	if q := run(route("Martlock", 0), route("Lymhurst", 0)); len(q.Slices) != 2 || total(q) <= 11 {
		t.Fatalf("没有深度池时两条都该分到,得到 %+v", q.Slices)
	}
}

// 审查探针:同一个 Lymhurst 卖单簿,往 Martlock 能走到 1105 件,往 Thetford 限价浅、
// 只走到 105 件。深度池以前取最小容量,两条一起放进组合时深的那条只分到 105 件,
// 组合页报 5,460/天,真实可做的约 57,460/天
func TestPortfolio_深浅两条路线共用一个卖单簿时深的不被浅的卡住(t *testing.T) {
	deep := arb.Route{ItemID: "T5_CLOTH", Quality: 1, FromCity: "Lymhurst", ToCity: "Martlock",
		Mode: "taker-taker", Qty: 1105, CostPerUnit: 1100, ProfitPerUnit: 52, HoursPerTrip: 1,
		SourceDaily: 6000, DestDaily: 6000, BuyDepthQty: 1105, DailyProfit: 57_460}
	shallow := arb.Route{ItemID: "T5_CLOTH", Quality: 1, FromCity: "Lymhurst", ToCity: "Thetford",
		Mode: "taker-taker", Qty: 105, CostPerUnit: 1010, ProfitPerUnit: 46, HoursPerTrip: 1,
		SourceDaily: 6000, DestDaily: 6000, BuyDepthQty: 105, DailyProfit: 4_830}
	plan := func(routes ...arb.Route) map[string]float64 {
		s := &Service{Cfg: conf.Default()}
		s.last.Store(&scan.Result{Routes: routes})
		out := map[string]float64{}
		for _, sl := range s.Portfolio(portfolio.DefaultOptions(10_000_000)).Slices {
			out[sl.Key] += sl.DailyQty
		}
		return out
	}
	const kDeep, kShallow = "arb:T5_CLOTH|Lymhurst->Martlock", "arb:T5_CLOTH|Lymhurst->Thetford"

	// 深的回报率高,先分:吃满自己限价以内的 1105 件;浅的限价以内的货已经被吃光
	got := plan(deep, shallow)
	if got[kDeep] != 1105 || got[kShallow] != 0 {
		t.Fatalf("深的应分到 1105 件、浅的 0 件,得到 %v", got)
	}
	// 浅的回报率高时反过来:浅的吃最便宜的 105 件,深的吃它限价以内剩下的 1000 件,
	// 合计正好是深的那条限价以内挂着的量,没有超卖
	shallow.ProfitPerUnit = 80
	got = plan(deep, shallow)
	if got[kShallow] != 105 || got[kDeep] != 1000 {
		t.Fatalf("浅的先分 105、深的分剩下的 1000,得到 %v", got)
	}
}
