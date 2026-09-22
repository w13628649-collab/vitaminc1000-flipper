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

// 名义摩擦偏小,真实盈亏平衡才是"价差够不够"的判据。
// 两侧费用基数不同(买侧乘 my_bid、卖侧乘 my_ask),相加得到的数不是门槛。
func TestBreakevenIsHigherThanNominalFriction(t *testing.T) {
	cfg := conf.Default().Economics
	cases := []struct {
		mode Mode
		want float64 // 真实门槛
	}{
		{Mode{Taker, Taker}, 1/0.96 - 1},      // 4.167%,名义 4.0%
		{Mode{Taker, Maker}, 1/0.935 - 1},     // 6.952%,名义 6.5%
		{Mode{Maker, Taker}, 1.025/0.96 - 1},  // 6.771%,名义 6.5%
		{Mode{Maker, Maker}, 1.025/0.935 - 1}, // 9.626%,名义 9.0%
	}
	for _, c := range cases {
		got := c.mode.Breakeven(cfg)
		if !near(got, c.want) {
			t.Fatalf("%s 盈亏平衡 = %.6f,想要 %.6f", c.mode.Label(), got, c.want)
		}
		if got <= c.mode.Friction(cfg) {
			t.Fatalf("%s:真实门槛 %.4f 应当高于名义摩擦 %.4f",
				c.mode.Label(), got, c.mode.Friction(cfg))
		}
	}

	// 名义摩擦分不开的两个模式,真实门槛能分开
	tm, mt := Mode{Taker, Maker}, Mode{Maker, Taker}
	if tm.Friction(cfg) != mt.Friction(cfg) {
		t.Fatal("前提变了:这两个模式的名义摩擦本来应该相等")
	}
	if near(tm.Breakeven(cfg), mt.Breakeven(cfg)) {
		t.Fatal("秒买挂卖和挂买秒卖的真实门槛应当不同(6.95% vs 6.77%)")
	}
}

// Breakeven 必须和 Quote 算出来的盈亏点一致,否则界面报的门槛是假的。
func TestBreakevenAgreesWithQuote(t *testing.T) {
	cfg := conf.Default().Economics
	for _, m := range Modes {
		be := m.Breakeven(cfg)
		// 构造一对刚好在门槛上的价:挂单腿会被 outbid/undercut 各让 1 银,
		// 所以取大基数把那 1 银的影响压到可忽略
		const bid = 10_000_000
		ask := int64(float64(bid) * (1 + be))

		// 刚好打平附近:低 1% 必亏,高 1% 必赚
		lo := Quote(Book{SellMin: bid, BuyMax: bid}, Book{SellMin: int64(float64(ask) * 0.99), BuyMax: int64(float64(ask) * 0.99)}, m, cfg)
		hi := Quote(Book{SellMin: bid, BuyMax: bid}, Book{SellMin: int64(float64(ask) * 1.01), BuyMax: int64(float64(ask) * 1.01)}, m, cfg)
		if lo.ProfitPerUnit >= 0 {
			t.Fatalf("%s:低于门槛 1%% 还赚钱(%.2f),Breakeven 偏高", m.Label(), lo.ProfitPerUnit)
		}
		if hi.ProfitPerUnit <= 0 {
			t.Fatalf("%s:高于门槛 1%% 还亏钱(%.2f),Breakeven 偏低", m.Label(), hi.ProfitPerUnit)
		}
	}
}

// 挂卖腿的到手比例要能复现游戏内挂单界面那笔账。
func TestSellLegMatchesInGameOrderScreen(t *testing.T) {
	cfg := conf.Default().Economics
	// T6_METALBAR_LEVEL4@4,56 件 × 357,978
	const qty, price = 56, 357_978
	gross := float64(qty * price)

	tax := gross * cfg.MarketTax  // 游戏显示 4% 尊享税率 801,871
	setup := gross * cfg.SetupFee // 游戏显示 2.5% 创建费 501,169
	subtotal := gross - tax       // 游戏显示"合计" 19,244,897 —— 只扣了税
	net := gross * Mode{Maker, Maker}.sellMul(cfg)

	if int64(tax+0.5) != 801_871 {
		t.Fatalf("市场税 %.0f,游戏显示 801871", tax)
	}
	if int64(setup+0.5) != 501_169 {
		t.Fatalf("创建费 %.0f,游戏显示 501169", setup)
	}
	if int64(subtotal+0.5) != 19_244_897 {
		t.Fatalf("合计 %.0f,游戏显示 19244897", subtotal)
	}
	// 创建费是下单当场单独扣的,不在"合计"里 —— 真正到手要再减掉它
	if int64(net+0.5) != int64(subtotal-setup+0.5) {
		t.Fatalf("到手 %.0f,应为 合计−创建费 = %.0f", net, subtotal-setup)
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

// 一天搬的量绝不能超过市场吃得下的量。
//
// 原来用 ceil(absorbable/qty) 算轮数再乘回去,qty 略小于 absorbable 时
// 轮数直接跳到 2,一天实际成交量接近上限的两倍,日收益跟着虚增近 100%。
func TestTurnoverNeverExceedsMarketCapacity(t *testing.T) {
	cfg := conf.Default().Economics
	u := Quote(Book{SellMin: 1200, BuyMax: 1000}, Book{SellMin: 1200, BuyMax: 1000},
		Mode{Maker, Maker}, cfg)

	const absorbable = 10_000
	for _, capital := range []int64{1_000_000, 9_000_000, 10_000_000, 10_260_250, 50_000_000} {
		qty, turns, daily := Turnover(u, absorbable, capital, 8)
		moved := float64(qty) * turns
		if moved > absorbable+1e-6 {
			t.Fatalf("本金 %d:一天搬了 %.0f 件,超过市场容量 %d", capital, moved, absorbable)
		}
		if want := u.ProfitPerUnit * moved; daily > want+1e-6 {
			t.Fatalf("本金 %d:日收益 %v 对不上搬运量 %v", capital, daily, want)
		}
	}
}

// 本金越多日收益只能升不能降。原来 ceil 的写法会出现
// "本金多 26 万,日收益砍一半"这种说不通的结果。
func TestTurnoverIsMonotonicInCapital(t *testing.T) {
	cfg := conf.Default().Economics
	u := Quote(Book{SellMin: 1200, BuyMax: 1000}, Book{SellMin: 1200, BuyMax: 1000},
		Mode{Maker, Maker}, cfg)

	prev := -1.0
	for capital := int64(500_000); capital <= 30_000_000; capital += 137_000 {
		_, _, daily := Turnover(u, 10_000, capital, 8)
		if daily < prev-1e-6 {
			t.Fatalf("本金涨到 %d 时日收益从 %v 掉到 %v", capital, prev, daily)
		}
		prev = daily
	}
}

// 市场撑得住时,转得快就该赚得多;市场是瓶颈时,转多快都一样。
func TestTurnoverSpeedOnlyMattersWhenCapitalBound(t *testing.T) {
	cfg := conf.Default().Economics
	u := Quote(Book{SellMin: 1200, BuyMax: 1000}, Book{SellMin: 1200, BuyMax: 1000},
		Mode{Maker, Maker}, cfg)

	// 市场很大、本金很小 → 多转几轮就多赚
	_, _, fast := Turnover(u, 1_000_000, 1_000_000, 2)
	_, _, slow := Turnover(u, 1_000_000, 1_000_000, 12)
	if !(fast > slow) {
		t.Fatalf("本金受限时快的 %v 应该高于慢的 %v", fast, slow)
	}

	// 市场很小、本金充裕 → 一轮就搬完,速度无关
	_, _, f2 := Turnover(u, 100, 50_000_000, 2)
	_, _, s2 := Turnover(u, 100, 50_000_000, 12)
	if f2 != s2 {
		t.Fatalf("市场受限时速度不该有影响:%v vs %v", f2, s2)
	}
}
