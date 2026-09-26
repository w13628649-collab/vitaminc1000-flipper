package econ

import (
	"math"
	"math/rand"
	"testing"

	"albion-guild/internal/conf"
)

// band = 0 时必须和改之前的 Quote 逐位一致:不是"差不多",是每个 float 的每一位都一样,
// 而且不带 Clamp。线上把 maker_price_band 写成 0 就等于退回老行为,这条得有测试钉住
func TestQuoteBanded_带宽为0时和Quote逐位一致(t *testing.T) {
	cfg := conf.Default().Economics
	rng := rand.New(rand.NewSource(11))
	for i := 0; i < 5000; i++ {
		buy := Book{SellMin: rng.Int63n(1_000_000) + 1, BuyMax: rng.Int63n(1_000_000) + 1}
		sell := Book{SellMin: rng.Int63n(1_000_000) + 1, BuyMax: rng.Int63n(1_000_000) + 1}
		// 参考价随便给,甚至离谱:带宽关着就不许看它
		b := Band{Width: 0, BuyRef: float64(rng.Int63n(2_000_000)), SellRef: float64(rng.Int63n(2_000_000))}
		for _, m := range Modes {
			want := Quote(buy, sell, m, cfg)
			got := QuoteBanded(buy, sell, m, cfg, b)
			if got.Unit != want || got.Raw != want || got.Clamp != nil {
				t.Fatalf("%s %+v %+v:band=0 应和 Quote 逐位一致\n得到 %+v / %+v / %+v\n应为 %+v",
					m.Label(), buy, sell, got.Unit, got.Raw, got.Clamp, want)
			}
		}
	}
}

// 参考价缺失(0)的那条腿不钳:没有历史的交给历史闸门,这里不替它编一个价
func TestQuoteBanded_参考价缺失时不钳(t *testing.T) {
	cfg := conf.Default().Economics
	book := Book{SellMin: 9996, BuyMax: 1000} // 离谱的宽盘口
	mm := Mode{Maker, Maker}
	want := Quote(book, book, mm, cfg)
	if got := QuoteBanded(book, book, mm, cfg, Band{Width: 0.2}); got.Unit != want || got.Clamp != nil {
		t.Fatalf("两边都没有参考价,不该钳,得到 %+v / %+v", got.Unit, got.Clamp)
	}
	// 只有卖出城有参考价:只钳挂卖,挂买照盘口
	got := QuoteBanded(book, book, mm, cfg, Band{Width: 0.2, SellRef: 4770})
	if got.Clamp == nil || got.Clamp.BuyClamped || !got.Clamp.SellClamped || got.MyBid != 1001 || got.MyAsk != 5724 {
		t.Fatalf("只该钳挂卖到 5,724,得到 %+v / %+v", got.Unit, got.Clamp)
	}
}

// 吃单腿的价是盘口上真挂着的单,带宽再窄、参考价再离谱都不动它
func TestQuoteBanded_吃单腿不钳(t *testing.T) {
	cfg := conf.Default().Economics
	buy := Book{SellMin: 9000, BuyMax: 100}
	sell := Book{SellMin: 50_000, BuyMax: 30_000}
	b := Band{Width: 0.01, BuyRef: 1000, SellRef: 1000}
	for _, m := range Modes {
		got := QuoteBanded(buy, sell, m, cfg, b)
		if m.Buy == Taker && got.MyBid != buy.SellMin {
			t.Fatalf("%s:秒买付的是卖一 %d,得到 %d", m.Label(), buy.SellMin, got.MyBid)
		}
		if m.Sell == Taker && got.MyAsk != sell.BuyMax {
			t.Fatalf("%s:秒卖收的是买一 %d,得到 %d", m.Label(), sell.BuyMax, got.MyAsk)
		}
		if m == (Mode{Taker, Taker}) && (got.Clamp != nil || got.Unit != Quote(buy, sell, m, cfg)) {
			t.Fatalf("秒买秒卖没有挂单腿,应和 Quote 完全一样,得到 %+v / %+v", got.Unit, got.Clamp)
		}
		if got.Clamp != nil && ((m.Buy == Taker && got.Clamp.BuyClamped) || (m.Sell == Taker && got.Clamp.SellClamped)) {
			t.Fatalf("%s:吃单腿不该标成被钳,得到 %+v", m.Label(), got.Clamp)
		}
	}
}

// 用户截图那条:T4_WOOD_LEVEL3@3 Thetford 卖一 9,996,7 日成交均价 4,770。
// 挂卖价封到 4,770 × 1.2 = 5,724;挂买那边 Bridgewatch 买一 4,357、均价 4,378,
// 下沿 3,502 比买一还低,不动
func TestQuoteBanded_T4特异松木挂卖封到5724(t *testing.T) {
	cfg := conf.Default().Economics
	from := Book{SellMin: 4700, BuyMax: 4357}
	to := Book{SellMin: 9996, BuyMax: 4550}
	mm := Mode{Maker, Maker}
	got := QuoteBanded(from, to, mm, cfg, Band{Width: 0.2, BuyRef: 4378, SellRef: 4770})
	c := got.Clamp
	if c == nil || c.BuyClamped || !c.SellClamped || c.SellRef != 4770 || c.BuyRef != 0 || c.Band != 0.2 {
		t.Fatalf("只该钳挂卖,得到 %+v", c)
	}
	if got.MyBid != 4358 || got.MyAsk != 5724 || c.RawBuyPrice != 4358 || c.RawSellPrice != 9995 {
		t.Fatalf("挂价应为 4,358 → 5,724(钳前 4,358 → 9,995),得到 %d → %d(钳前 %d → %d)",
			got.MyBid, got.MyAsk, c.RawBuyPrice, c.RawSellPrice)
	}
	// 钳前就是截图上的 109.2%;钳后 5724×0.935 / (4358×1.025) − 1 ≈ 19.8%
	if math.Abs(c.RawMargin-(9995*0.935/(4358*1.025)-1)) > 1e-12 || math.Abs(c.RawMargin-1.092) > 0.001 {
		t.Fatalf("钳前毛利应是 109.2%%,得到 %.4f", c.RawMargin)
	}
	if want := 5724*0.935/(4358*1.025) - 1; math.Abs(got.Margin-want) > 1e-12 || math.Abs(got.Margin-0.1981) > 0.0001 {
		t.Fatalf("钳后毛利应是 19.81%%,得到 %.4f", got.Margin)
	}
	// 税费一份:钳过的账和直接按 4,358 → 5,724 两个价算出来的逐位相同
	if got.Unit != unitAt(4358, 5724, mm, cfg) || got.Raw != Quote(from, to, mm, cfg) {
		t.Fatalf("钳过的账应按钳过的价走同一套税费,得到 %+v", got.Unit)
	}
}

// 审查复现:卖一已经跌破 均价 × (1 − band) 时,按均价抬的挂买价会越过卖一。
// 产地卖一 7,500、买一 5,000、7 日均价 10,000:以前抬到 8,000,比卖一还高——这时秒买 7,500
// 更便宜,还不付创建费、不用等,"照抄"的挂买价是错单。对称的,销地买一 1,300 已经高过
// 均价 1,000 × 1.2,以前挂卖价被压到 1,200,比直接砸给买一还便宜。
//
// 卖一 < 均价说明整本盘口已经跌到均价下面、均价是旧的:下沿改按卖一算(卖一上真有人在卖);
// 买一 > 均价同理,上限改按买一算
func TestQuoteBanded_钳过的挂价不越过对面盘口(t *testing.T) {
	cfg := conf.Default().Economics
	mm := Mode{Maker, Maker}

	// 挂买腿:参考价 min(均价 10,000, 卖一 7,500) = 7,500,下沿 6,000
	got := QuoteBanded(Book{SellMin: 7500, BuyMax: 5000}, Book{SellMin: 13_000, BuyMax: 11_000}, mm, cfg,
		Band{Width: 0.2, BuyRef: 10_000, SellRef: 12_000})
	if c := got.Clamp; got.MyBid != 6000 || c == nil || !c.BuyClamped || c.BuyRef != 7500 || c.BuyRefFrom != RefSellMin {
		t.Fatalf("挂买价应按卖一 7,500 下浮到 6,000、标明参考价取自卖一,得到 %d / %+v", got.MyBid, c)
	}
	// 挂卖腿:参考价 max(均价 1,000, 买一 1,300) = 1,300,上限 1,560
	got = QuoteBanded(Book{SellMin: 800, BuyMax: 700}, Book{SellMin: 2000, BuyMax: 1300}, mm, cfg,
		Band{Width: 0.2, BuyRef: 750, SellRef: 1000})
	if c := got.Clamp; got.MyAsk != 1560 || c == nil || !c.SellClamped || c.SellRef != 1300 || c.SellRefFrom != RefBuyMax {
		t.Fatalf("挂卖价应按买一 1,300 上浮到 1,560、标明参考价取自买一,得到 %d / %+v", got.MyAsk, c)
	}
	// 参考价取自均价时照旧标 avg_7d(截图那条)
	got = QuoteBanded(Book{SellMin: 4700, BuyMax: 4357}, Book{SellMin: 9996, BuyMax: 4550}, mm, cfg,
		Band{Width: 0.2, BuyRef: 4378, SellRef: 4770})
	if c := got.Clamp; c == nil || c.SellRefFrom != RefAvg7d || c.BuyRefFrom != "" || got.MyAsk != 5724 {
		t.Fatalf("Thetford 买一 4,550 低于均价 4,770,参考价仍是均价,得到 %+v", c)
	}

	// 随机:含小价格、窄带宽(取整后可能碰到对面价)。被封的腿,挂买价一定低于同本卖一、
	// 挂卖价一定高于同本买一
	rng := rand.New(rand.NewSource(29))
	hits := 0
	for i := 0; i < 20_000; i++ {
		p := func() int64 {
			if rng.Intn(4) == 0 {
				return rng.Int63n(6) // 0 = 这一侧没价;1~5 专门试取整
			}
			return rng.Int63n(20_000) + 1
		}
		buy, sell := Book{SellMin: p(), BuyMax: p()}, Book{SellMin: p(), BuyMax: p()}
		b := Band{Width: rng.Float64() * 0.95, BuyRef: float64(rng.Int63n(30_000)), SellRef: float64(rng.Int63n(30_000))}
		for _, m := range Modes {
			got := QuoteBanded(buy, sell, m, cfg, b)
			c := got.Clamp
			if c == nil {
				continue
			}
			hits++
			if c.BuyClamped && buy.SellMin > 0 && got.MyBid >= buy.SellMin {
				t.Fatalf("%s %+v %+v:挂买价 %d 越过了同本卖一 %d", m.Label(), buy, b, got.MyBid, buy.SellMin)
			}
			if c.SellClamped && sell.BuyMax > 0 && got.MyAsk <= sell.BuyMax {
				t.Fatalf("%s %+v %+v:挂卖价 %d 越过了同本买一 %d", m.Label(), sell, b, got.MyAsk, sell.BuyMax)
			}
		}
	}
	if hits < 1000 {
		t.Fatalf("随机样本没怎么覆盖到钳制:%d", hits)
	}
}

// 单边行情里 7 日均价是旧的。稀有钢条.2 @ Bridgewatch(审查影子库):买一 1,992、卖一 2,367、
// 7 日均价 2,549,整本盘口都在均价下面。按均价定下沿是 2,039,会把挂买价抬高、毛利 8.3% → 5.8%;
// 卖一上真有人按 2,367 在卖,按它定下沿是 1,894,买一 1,992 在带里,不钳。
// 反方向:买一高过均价(特异松木.3 @ Lymhurst 那种,买一是均价的 1.02 倍),上限跟着买一走
func TestQuoteBanded_单边行情参考价跟着对面盘口走(t *testing.T) {
	cfg := conf.Default().Economics
	mm := Mode{Maker, Maker}
	book := Book{SellMin: 2367, BuyMax: 1992}
	if got := QuoteBanded(book, book, mm, cfg, Band{Width: 0.2, BuyRef: 2549, SellRef: 2549}); got.Clamp != nil ||
		got.Unit != Quote(book, book, mm, cfg) {
		t.Fatalf("盘口整体跌到均价下面、价差 18.8%%,不该钳,得到 %+v / %+v", got.Unit, got.Clamp)
	}
	up := Book{SellMin: 1500, BuyMax: 1020}
	got := QuoteBanded(up, up, mm, cfg, Band{Width: 0.2, BuyRef: 1000, SellRef: 1000})
	if c := got.Clamp; c == nil || c.BuyClamped || !c.SellClamped || got.MyAsk != 1224 || c.SellRef != 1020 {
		t.Fatalf("买一 1,020 高过均价,上限应按买一 × 1.2 = 1,224,得到 %d / %+v", got.MyAsk, c)
	}
}

// 钳制只会让账变差:买价只升、卖价只降、利润只少。否则它就能把本来不在榜上的东西放进来
func TestQuoteBanded_只会让账变差(t *testing.T) {
	cfg := conf.Default().Economics
	rng := rand.New(rand.NewSource(3))
	for i := 0; i < 5000; i++ {
		buy := Book{SellMin: rng.Int63n(100_000) + 2, BuyMax: rng.Int63n(100_000) + 2}
		sell := Book{SellMin: rng.Int63n(100_000) + 2, BuyMax: rng.Int63n(100_000) + 2}
		b := Band{Width: rng.Float64() * 0.9, BuyRef: float64(rng.Int63n(100_000)), SellRef: float64(rng.Int63n(100_000))}
		for _, m := range Modes {
			got := QuoteBanded(buy, sell, m, cfg, b)
			if got.MyBid < got.Raw.MyBid || got.MyAsk > got.Raw.MyAsk || got.ProfitPerUnit > got.Raw.ProfitPerUnit ||
				got.Margin > got.Raw.Margin+1e-15 {
				t.Fatalf("%s %+v %+v %+v:钳过的账不该比盘口价更好,得到 %+v / 原 %+v", m.Label(), buy, sell, b, got.Unit, got.Raw)
			}
			if (got.Clamp == nil) != (got.Unit == got.Raw) {
				t.Fatalf("有没有 Clamp 要和价变没变对上,得到 %+v / %+v / %+v", got.Unit, got.Raw, got.Clamp)
			}
		}
	}
}
