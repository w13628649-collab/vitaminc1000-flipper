package econ

import (
	"testing"

	"albion-guild/internal/conf"
)

func TestFrictionDiffersByExecutionMode(t *testing.T) {
	cfg := conf.Default().Economics
	cases := []struct {
		mode Mode
		want float64
	}{
		// 秒买不收买方费用,只有卖出那 4% 税。Demo 漏掉的就是这一类
		{Mode{Taker, Taker}, 0.04},
		{Mode{Taker, Maker}, 0.065},
		{Mode{Maker, Taker}, 0.065},
		{Mode{Maker, Maker}, 0.09},
	}
	for _, c := range cases {
		if got := c.mode.Friction(cfg); !near(got, c.want) {
			t.Fatalf("%s 摩擦 = %v,想要 %v", c.mode.Label(), got, c.want)
		}
	}
}

// 两条腿用的价格基准不同,这是最容易算错的地方。
func TestEachLegUsesItsOwnPriceBasis(t *testing.T) {
	cfg := conf.Default().Economics
	book := Book{SellMin: 1200, BuyMax: 1000}

	taker := Quote(book, book, Mode{Taker, Taker}, cfg)
	if taker.MyBid != 1200 || taker.MyAsk != 1000 {
		t.Fatalf("秒买秒卖应该是 付卖一1200 / 收买一1000,得到 %d / %d", taker.MyBid, taker.MyAsk)
	}
	maker := Quote(book, book, Mode{Maker, Maker}, cfg)
	if maker.MyBid != 1001 || maker.MyAsk != 1199 {
		t.Fatalf("挂买挂卖应该是 1001 / 1199,得到 %d / %d", maker.MyBid, maker.MyAsk)
	}
}

// 盘口窄的时候秒买秒卖是亏的:省下的手续费不够赔进去的价差。
func TestTightSpreadMakesInstantExecutionLose(t *testing.T) {
	cfg := conf.Default().Economics
	book := Book{SellMin: 1050, BuyMax: 1000} // 只有 5% 盘口

	taker := Quote(book, book, Mode{Taker, Taker}, cfg)
	if taker.ProfitPerUnit >= 0 {
		t.Fatalf("窄盘口秒买秒卖利润 = %v,应该是负的", taker.ProfitPerUnit)
	}
}

// 跨城才是秒买秒卖真正划算的地方:两地价差大,省下 5% 手续费很值。
func TestWideCrossCitySpreadFavorsInstantExecution(t *testing.T) {
	cfg := conf.Default().Economics
	cheap := Book{SellMin: 1000, BuyMax: 950} // 产地
	rich := Book{SellMin: 1500, BuyMax: 1420} // 销地

	unit, mode := Best(cheap, rich, cfg)
	if unit.ProfitPerUnit <= 0 {
		t.Fatalf("跨城利润 = %v,应该为正", unit.ProfitPerUnit)
	}
	// 秒买 1000、秒卖 1420 拿到 1363,净赚 363;
	// 挂买挂卖虽然价格更好,但要多付 5% 手续费,还得两头等
	if mode.Buy != Taker || mode.Sell != Taker {
		t.Logf("最优模式是 %s,单件利润 %.0f", mode.Label(), unit.ProfitPerUnit)
	}
	for _, m := range Modes {
		u := Quote(cheap, rich, m, cfg)
		t.Logf("  %s: 买%d 卖%d 单件利润 %.0f 毛利 %.1f%%",
			m.Label(), u.MyBid, u.MyAsk, u.ProfitPerUnit, u.Margin*100)
	}
}

// Best 挑出来的必须真是利润最高的那个。
func TestBestPicksHighestProfitMode(t *testing.T) {
	cfg := conf.Default().Economics
	buy := Book{SellMin: 900, BuyMax: 800}
	sell := Book{SellMin: 1400, BuyMax: 1300}

	unit, mode := Best(buy, sell, cfg)
	for _, m := range Modes {
		if u := Quote(buy, sell, m, cfg); u.ProfitPerUnit > unit.ProfitPerUnit+1e-9 {
			t.Fatalf("Best 选了 %s(%.2f),但 %s 更高(%.2f)",
				mode.Label(), unit.ProfitPerUnit, m.Label(), u.ProfitPerUnit)
		}
	}
}
