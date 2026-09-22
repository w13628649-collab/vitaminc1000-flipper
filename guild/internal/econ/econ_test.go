package econ

import (
	"math"
	"testing"

	"albion-guild/internal/conf"
)

func economics(mut func(*conf.Economics)) conf.Economics {
	c := conf.Default().Economics
	if mut != nil {
		mut(&c)
	}
	return c
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestFrictionFollowsBuyOrderFeeSwitch(t *testing.T) {
	if got := economics(nil).RoundTripFriction(); !near(got, 0.09) {
		t.Fatalf("挂买单收费时摩擦 = %v,想要 0.09", got)
	}
	free := economics(func(c *conf.Economics) { c.BuyOrderSetupFee = false })
	if got := free.RoundTripFriction(); !near(got, 0.065) {
		t.Fatalf("挂买单免费时摩擦 = %v,想要 0.065", got)
	}
	// 非会员市场税 8%
	noPremium := economics(func(c *conf.Economics) { c.MarketTax = 0.08 })
	if got := noPremium.RoundTripFriction(); !near(got, 0.13) {
		t.Fatalf("非会员摩擦 = %v,想要 0.13", got)
	}
}

func TestSpreadBelowFrictionLosesMoney(t *testing.T) {
	u := UnitEconomics(1000, 1080, economics(nil)) // 8% < 9%
	if u.ProfitPerUnit >= 0 {
		t.Fatalf("单件利润 = %v,价差吃不过摩擦时应该是负的", u.ProfitPerUnit)
	}
}

func TestOrdersMustBeatCurrentPrice(t *testing.T) {
	u := UnitEconomics(1000, 1200, economics(nil))
	if u.MyBid != 1001 || u.MyAsk != 1199 {
		t.Fatalf("挂单价 = (%d, %d),想要 (1001, 1199)", u.MyBid, u.MyAsk)
	}
}

func TestSkippingBuyFeeCostsLess(t *testing.T) {
	withFee := UnitEconomics(1000, 1200, economics(nil))
	without := UnitEconomics(1000, 1200, economics(func(c *conf.Economics) {
		c.BuyOrderSetupFee = false
	}))
	if !(without.CostPerUnit < withFee.CostPerUnit) {
		t.Fatalf("免手续费的成本 %v 应该更低(对比 %v)", without.CostPerUnit, withFee.CostPerUnit)
	}
	if !(without.ProfitPerUnit > withFee.ProfitPerUnit) {
		t.Fatalf("免手续费的利润 %v 应该更高(对比 %v)", without.ProfitPerUnit, withFee.ProfitPerUnit)
	}
}

func TestDepthIsTheBottleneck(t *testing.T) {
	u := UnitEconomics(1000, 1200, economics(nil))
	// 日成交 100 件 → 20% = 20 件,远小于 1000 万本金买得起的量
	p := SizePosition(u, 100, 10_000_000, conf.Default().Sizing)
	if p.Qty != 20 {
		t.Fatalf("可吃量 = %d,想要 20", p.Qty)
	}
	if p.CapitalBound {
		t.Fatal("市场深度才是瓶颈,不该判成本金受限")
	}
}

func TestCapitalIsTheBottleneck(t *testing.T) {
	u := UnitEconomics(1000, 1200, economics(nil))
	p := SizePosition(u, 1_000_000, 10_000_000, conf.Default().Sizing)
	want := int64(10_000_000 / u.CostPerUnit)
	if p.Qty != want {
		t.Fatalf("可吃量 = %d,想要 %d", p.Qty, want)
	}
	if !p.CapitalBound {
		t.Fatal("本金才是瓶颈")
	}
	if p.CapitalUsed > 10_000_000 {
		t.Fatalf("用掉本金 %v,超过 1000 万", p.CapitalUsed)
	}
}

// SPEC 的核心主张:利润率 30% 但一天只能做 3 件,不如 5% 但能做 2000 件。
func TestCapitalROISeparatesThinFromFat(t *testing.T) {
	sizing := conf.Default().Sizing
	fat := UnitEconomics(1000, 1450, economics(nil))  // 高毛利
	thin := UnitEconomics(1000, 1120, economics(nil)) // 低毛利

	fatPos := SizePosition(fat, 15, 10_000_000, sizing)
	thinPos := SizePosition(thin, 50_000, 10_000_000, sizing)

	if !(fat.Margin > thin.Margin) {
		t.Fatalf("厚利毛利率 %v 应该高于薄利 %v", fat.Margin, thin.Margin)
	}
	// 但日化绝对收益反过来——这就是为什么排序主键是 DailyProfit
	if !(thinPos.DailyProfit > fatPos.DailyProfit) {
		t.Fatalf("薄利日收益 %v 应该高于厚利 %v", thinPos.DailyProfit, fatPos.DailyProfit)
	}
	if !(thinPos.CapitalROI > fatPos.CapitalROI) {
		t.Fatalf("薄利本金回报 %v 应该高于厚利 %v", thinPos.CapitalROI, fatPos.CapitalROI)
	}
}

func TestDailyROIEqualsMarginForSameCityFlip(t *testing.T) {
	u := UnitEconomics(1000, 1200, economics(nil))
	p := SizePosition(u, 1000, 10_000_000, conf.Default().Sizing)
	if !near(p.DailyROI, u.Margin) {
		t.Fatalf("日化收益率 %v != 单笔毛利率 %v", p.DailyROI, u.Margin)
	}
}
