package arb

import (
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/econ"
	"albion-guild/internal/screen"
)

// 挂单腿带宽钳制在跨城这一侧的回归测试。数字取自用户截图(2026-09-26,「机会这里没对上吧」):
// 特异松木.3 Bridgewatch → Thetford 挂买挂卖 4,358 → 9,995、毛利 109.2%、日收益 2.17M,排机会页前列。
// Thetford 卖一 9,996(×6 / 近价 146)、买一 4,550,7 日成交均价 4,770、30 日 4,400、日均 4,791 件;
// Bridgewatch 买一 4,357、7 日均价 4,378。
//
// Bridgewatch 的卖一和日均件数截图上没有:卖一取 4,700(秒买挂卖因此不如挂买挂卖,和截图选的一致),
// 日均件数按截图的日收益反推(2.17M / 单件 4,878 ≈ 445 件/天可吃 → 2,225 件)
func t4WoodMarkets() (Market, Market) {
	from := market("Bridgewatch", 4700, 4357, 2225)
	from.Stats.AvgPrice7d = 4378
	to := market("Thetford", 9996, 4550, 4791)
	to.Stats.AvgPrice7d = 4770
	to.Stats.AvgPrice30d, to.Stats.StdDev30d, to.Stats.CV = 4400, 248, 248.0/4400
	to.Ask = capSide(depth.Level{Price: 9996, Qty: 6}, depth.Level{Price: 10_300, Qty: 140}, depth.Level{Price: 12_000, Qty: 500})
	return from, to
}

func findWood(o Options, markets ...Market) (Route, bool) {
	return routeOf(Find("T4_WOOD_LEVEL3@3", "特异松木", 1, markets, conf.Default().Economics, o, now),
		"Bridgewatch", "Thetford")
}

func TestClamp_T4特异松木跨城挂卖封到5724(t *testing.T) {
	from, to := t4WoodMarkets()

	// 改之前(band = 0):复现截图那条
	off := opts()
	off.MakerPriceBand = 0
	old, ok := findWood(off, from, to)
	if !ok || old.Mode != "maker-maker" || old.BuyPrice != 4358 || old.SellPrice != 9995 ||
		math.Abs(old.Margin-1.092) > 0.001 || math.Abs(old.DailyProfit-2_170_877) > 1 || old.Clamp != nil {
		t.Fatalf("前提:band=0 应是截图上的 4,358 → 9,995、109.2%%、日收益 2.17M,得到 %v / %+v", ok, old)
	}
	if old.Confidence != screen.Low || !strings.Contains(strings.Join(old.Warnings, ";"), "11.6σ") {
		t.Fatalf("前提:销地 z 11.6σ 应降为 low,得到 %s / %v", old.Confidence, old.Warnings)
	}

	r, ok := findWood(opts(), from, to)
	if !ok {
		t.Fatal("挂卖封到 5,724 之后仍赚 19.8%,路线应该在")
	}
	if r.Mode != "maker-maker" || r.BuyPrice != 4358 || r.SellPrice != 5724 {
		t.Fatalf("应是挂买挂卖 4,358 → 5,724,得到 %s %d → %d", r.Mode, r.BuyPrice, r.SellPrice)
	}
	// 5724 × 0.935 / (4358 × 1.025) − 1 = 19.81%,单件 885,可吃量没变(445 件),日收益 39.4 万
	if want := 5724*0.935/(4358*1.025) - 1; math.Abs(r.Margin-want) > 1e-12 || math.Abs(r.Margin-0.1981) > 0.0001 {
		t.Fatalf("毛利应是 19.81%%,不再是 109%%,得到 %.4f", r.Margin)
	}
	if r.Qty != 445 || math.Abs(r.ProfitPerUnit-884.99) > 0.01 || math.Abs(r.DailyProfit-393_820.55) > 0.5 {
		t.Fatalf("应 445 件、单件 884.99、日收益 393,820.55,得到 %d / %.4f / %.4f", r.Qty, r.ProfitPerUnit, r.DailyProfit)
	}
	c := r.Clamp
	if c == nil || c.BuyClamped || !c.SellClamped || c.RawBuyPrice != 4358 || c.RawSellPrice != 9995 ||
		c.SellRef != 4770 || math.Abs(c.RawMargin-old.Margin) > 1e-15 {
		t.Fatalf("clamp 应说明挂卖从 9,995 封到 5,724,得到 %+v", c)
	}
	joined := strings.Join(r.Warnings, ";")
	if !strings.Contains(joined, "挂卖价按销地 7 日成交均价 4,770 上浮 20% 封到 5,724:卖一 9,996 是均价的 2.10 倍") {
		t.Fatalf("警告里要有一句人话,得到 %v", r.Warnings)
	}
	// 其余判据照旧:z 还是 11.6σ、还是 low
	if r.Confidence != screen.Low || !strings.Contains(joined, "11.6σ") {
		t.Fatalf("置信度和 z 的判断不受钳制影响,得到 %s / %v", r.Confidence, r.Warnings)
	}
	// 秒买挂卖也被封到 5,724;modes 里被钳的都带 clamp
	for _, m := range r.Modes {
		if strings.HasSuffix(m.Mode, "-maker") && (m.SellPrice != 5724 || m.Clamp == nil || !m.Clamp.SellClamped) {
			t.Fatalf("%s 的挂卖腿也要封到 5,724,得到 %+v", m.Label, m)
		}
	}
	raw, _ := json.Marshal(r)
	for _, k := range []string{`"clamp":{"band":0.2,"buy_clamped":false,"sell_clamped":true,"raw_buy_price":4358,"raw_sell_price":9995`,
		`"sell_ref":4770`} {
		if !strings.Contains(string(raw), k) {
			t.Fatalf("JSON 里应有 %s,得到 %s", k, raw)
		}
	}
}

// 流动性好、两端盘口都贴着各自均价的正常路线:结果和不钳逐位相同
func TestClamp_跨城正常路线不受影响(t *testing.T) {
	mk := func() []Market {
		from := market("Lymhurst", 1000, 950, 5000)
		from.Stats.AvgPrice7d = 980
		to := market("Martlock", 1600, 1500, 5000)
		to.Stats.AvgPrice7d = 1550
		return []Market{from, to}
	}
	off := opts()
	off.MakerPriceBand = 0
	on := Find("T5_CLOTH", "精布", 1, mk(), conf.Default().Economics, opts(), now)
	was := Find("T5_CLOTH", "精布", 1, mk(), conf.Default().Economics, off, now)
	if len(on) == 0 || !reflect.DeepEqual(on, was) {
		t.Fatalf("带宽内的路线不该有任何变化\n开 %+v\n关 %+v", on, was)
	}
	a, _ := json.Marshal(on)
	if strings.Contains(string(a), "clamp") {
		t.Fatalf("没被钳就不该输出 clamp: %s", a)
	}
}

// 参考价缺失(那一端没有 7 日均价)时不钳,和 band = 0 一样
func TestClamp_跨城参考价缺失时不变(t *testing.T) {
	from, to := t4WoodMarkets()
	from.Stats.AvgPrice7d, to.Stats.AvgPrice7d = 0, 0
	off := opts()
	off.MakerPriceBand = 0
	on, ok1 := findWood(opts(), from, to)
	was, ok2 := findWood(off, from, to)
	if !ok1 || !ok2 || !reflect.DeepEqual(on, was) || on.Clamp != nil || on.SellPrice != 9995 {
		t.Fatalf("没有参考价时不该钳\n开 %v %+v\n关 %v %+v", ok1, on, ok2, was)
	}
}

// band = 0 时和改之前逐位一致:每个模式的价和账都等于 econ.Quote + econ.Turnover,不带 clamp
func TestClamp_跨城带宽为0时和改之前逐位一致(t *testing.T) {
	from, to := t4WoodMarkets()
	o := opts()
	o.MakerPriceBand = 0
	r, ok := findWood(o, from, to)
	if !ok {
		t.Fatal("band=0 应有路线")
	}
	byMarket := math.Min(from.Stats.DailyVolumeQty, to.Stats.DailyVolumeQty) * o.AbsorbRatio
	for _, q := range r.Modes {
		// 这条路线两端都没按阶梯走(产地 AODP、销地卖方抓包但挂卖不走阶梯),每个模式就一个候选
		u := econ.Quote(from.Book, to.Book, q.mode, conf.Default().Economics)
		_, trips, daily := econ.Turnover(u, byMarket, o.Capital, o.routeHours(q.mode, from.City, to.City))
		if q.BuyPrice != u.MyBid || q.SellPrice != u.MyAsk || q.ProfitPerUnit != u.ProfitPerUnit ||
			q.Margin != u.Margin || q.TripsPerDay != trips || q.DailyProfit != daily || q.Clamp != nil ||
			q.unit != u || q.raw != u {
			t.Fatalf("%s:应和 Quote + Turnover 逐位一致,得到 %+v,应为 %+v", q.Label, q, u)
		}
	}
	if strings.Contains(strings.Join(r.Warnings, ";"), "挂卖价按") {
		t.Fatalf("band=0 不该有钳制说明,得到 %v", r.Warnings)
	}
}

// 吃单模式不受影响:秒买秒卖没有挂单腿,带宽开关前后逐位相同
func TestClamp_跨城吃单模式不受影响(t *testing.T) {
	mk := func() []Market {
		// 产地卖一贴着均价,销地买一是均价的 1.9 倍:秒卖砸的是真挂着的买单,不钳;
		// 挂卖那两个模式按销地均价封到 1,200。市场极大、本金是瓶颈,一趟最快的秒买秒卖最赚
		from := market("Lymhurst", 1000, 900, 10_000_000)
		from.Stats.AvgPrice7d = 1000
		to := market("Martlock", 2000, 1900, 10_000_000)
		to.Stats.AvgPrice7d = 1000
		return []Market{from, to}
	}
	off := opts()
	off.MakerPriceBand = 0
	on, ok1 := routeOf(Find("T5_CLOTH", "精布", 1, mk(), conf.Default().Economics, opts(), now), "Lymhurst", "Martlock")
	was, ok2 := routeOf(Find("T5_CLOTH", "精布", 1, mk(), conf.Default().Economics, off, now), "Lymhurst", "Martlock")
	if !ok1 || !ok2 || on.Mode != "taker-taker" || was.Mode != "taker-taker" {
		t.Fatalf("前提:两边都应选秒买秒卖,得到 %s / %s", on.Mode, was.Mode)
	}
	tOn, _ := modeOf(on, "taker-taker")
	tWas, _ := modeOf(was, "taker-taker")
	if !reflect.DeepEqual(tOn, tWas) || on.BuyPrice != 1000 || on.SellPrice != 1900 || on.Clamp != nil ||
		on.DailyProfit != was.DailyProfit || on.Qty != was.Qty {
		t.Fatalf("秒买秒卖应逐位不变\n开 %+v\n关 %+v", tOn, tWas)
	}
}

// 剪枝在有钳制时也必须精确:四种模式都拿暴力枚举对。挂单腿的价和走到第几档无关,
// 所以"更深只会更差"照样成立
func TestBestFill带钳制时剪枝和暴力枚举一致(t *testing.T) {
	cfg := conf.Default().Economics
	rng := rand.New(rand.NewSource(17))
	clamped, walkedClamped := 0, 0 // 确认真的覆盖到了"有钳制"和"钳制 + 吃单腿走阶梯"
	for trial := 0; trial < 300; trial++ {
		asks := randomLadder(rng, 1000, +1)
		bids := randomLadder(rng, 1000+int64(rng.Intn(400)), -1)
		from := market("Lymhurst", asks[0].Price, 900+int64(rng.Intn(100)), float64(rng.Intn(20000)+100))
		from.Ask = capSide(asks...)
		from.Stats.AvgPrice7d = float64(700 + rng.Intn(600))
		to := market("Martlock", bids[0].Price+int64(rng.Intn(600)), bids[0].Price, float64(rng.Intn(20000)+100))
		to.Bid = capSide(bids...)
		to.Stats.AvgPrice7d = float64(900 + rng.Intn(700))
		o := opts()
		o.Capital = int64(rng.Intn(5_000_000) + 50_000)
		o.DeviationMin, o.DeviationMax = 0, 0 // 只对剪枝,不让偏离度把大半组合挡掉
		byMarket := math.Min(from.Stats.DailyVolumeQty, to.Stats.DailyVolumeQty) * o.AbsorbRatio

		for _, m := range econ.Modes {
			got, gotOK := bestFill(m, from, to, byMarket, cfg, o)
			want, wantOK := bruteFillAny(m, from, to, byMarket, cfg, o)
			if gotOK != wantOK || got.DailyProfit != want.DailyProfit || got.BuyPrice != want.BuyPrice ||
				got.SellPrice != want.SellPrice || got.qty != want.qty {
				t.Fatalf("第 %d 组 %s 不一致\n剪枝 %v %+v\n暴力 %v %+v", trial, m.Label(), gotOK, got, wantOK, want)
			}
			if gotOK && got.Clamp != nil {
				clamped++
				if m.Buy != m.Sell && !math.IsInf(math.Min(got.buyCap, got.sellCap), 1) {
					walkedClamped++
				}
			}
		}
	}
	if clamped < 50 || walkedClamped < 20 {
		t.Fatalf("随机样本没怎么覆盖到钳制:有钳制 %d 组,其中吃单腿走了阶梯的 %d 组", clamped, walkedClamped)
	}
}

// bruteFillAny 是 bruteFill 的四模式版:只有吃单腿按阶梯枚举,挂单腿一个候选,账走 QuoteBanded。
func bruteFillAny(m econ.Mode, from, to Market, byMarket float64, cfg conf.Economics, opt Options) (ModeQuote, bool) {
	var best ModeQuote
	found := false
	band := screen.MakerBand(opt.MakerPriceBand, from.Stats, to.Stats)
	for _, b := range ladder(from.Ask, m.Buy == econ.Taker, from.Book.SellMin) {
		for _, s := range ladder(to.Bid, m.Sell == econ.Taker, to.Book.BuyMax) {
			bk, sk := from.Book, to.Book
			if m.Buy == econ.Taker {
				bk.SellMin = b.price
			}
			if m.Sell == econ.Taker {
				sk.BuyMax = s.price
			}
			u := econ.QuoteBanded(bk, sk, m, cfg, band).Unit
			if !legsSane(m, bk, sk, from.Stats, to.Stats, opt) || u.ProfitPerUnit < opt.MinProfitPerUnit || u.Margin < opt.MinMargin {
				continue
			}
			q, _, daily := econ.Turnover(u, math.Min(byMarket, math.Min(b.qty, s.qty)), opt.Capital, opt.routeHours(m, from.City, to.City))
			if q >= 1 && (!found || daily > best.DailyProfit) {
				found = true
				best = ModeQuote{BuyPrice: u.MyBid, SellPrice: u.MyAsk, DailyProfit: daily, qty: q}
			}
		}
	}
	return best, found
}
