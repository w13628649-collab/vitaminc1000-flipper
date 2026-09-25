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
