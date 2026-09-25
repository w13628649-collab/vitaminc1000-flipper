package arb

import (
	"math"
	"math/rand"
	"strings"
	"testing"

	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/econ"
	"albion-guild/internal/screen"
)

// T6_METALBAR_LEVEL4@4 @ Martlock 买方实抓阶梯:9 档 2,026 件,最优价 5% 以内
// 只有 7 档 11 件,再往下就是 53,001(−79.6%)和 1 银的占位单
var t6BidLadder = []depth.Level{
	{Price: 260066, Qty: 1}, {Price: 260065, Qty: 2}, {Price: 260064, Qty: 1},
	{Price: 260060, Qty: 2}, {Price: 260050, Qty: 2}, {Price: 260040, Qty: 1},
	{Price: 260000, Qty: 2}, {Price: 53001, Qty: 15}, {Price: 1, Qty: 2000},
}

// capSide 造一个来自抓包、深度可信的盘口边(同 scan 融合层的产物)。
func capSide(levels ...depth.Level) screen.Side {
	return screen.Side{
		Source: screen.SourceCapture, Price: levels[0].Price, AgeHours: 0.2,
		Depth:  &screen.DepthView{Support: depth.Analyze(levels, 0.05)},
		Levels: levels,
	}
}

func aodpSide(price int64) screen.Side {
	return screen.Side{Source: screen.SourceAODP, Price: price, AgeHours: 1}
}

func modeOf(r Route, key string) (ModeQuote, bool) {
	for _, m := range r.Modes {
		if m.Mode == key {
			return m, true
		}
	}
	return ModeQuote{}, false
}

func routeOf(routes []Route, from, to string) (Route, bool) {
	for _, r := range routes {
		if r.FromCity == from && r.ToCity == to {
			return r, true
		}
	}
	return Route{}, false
}

// 纯 AODP(Sides 零值)时什么都不变:不按阶梯、不限件数
func TestRoute_Sides零值时没有深度字段(t *testing.T) {
	r := find(market("Lymhurst", 1000, 950, 5000), market("Martlock", 1600, 1500, 5000))[0]
	if r.BuyDepthQty != 0 || r.SellDepthQty != 0 || r.DepthChecked || r.Bottleneck == "depth" {
		t.Fatalf("零值 Sides 不该有深度字段,得到 %+v", r)
	}
	if r.FromAsk != nil || r.ToBid != nil || r.BuyLegSource != "" {
		t.Fatalf("没有来源说明时不输出,得到 %+v", r)
	}
	for _, m := range r.Modes {
		if m.Blocked != "" || m.Breakeven <= m.Friction {
			t.Fatalf("%s:不该被拦,真实门槛应高于名义摩擦,得到 %+v", m.Label, m)
		}
	}
}

// 销地买方是 T6 实抓阶梯。秒卖只能吃到 260,000 那一档为止的 11 件:
// 再往下 53,001 一件亏 15 万。以前按买一 260,066 不限件数算,一天能卖一千件
func TestRoute_T6买方阶梯秒卖只吃近价那11件(t *testing.T) {
	from := market("Thetford", 200_000, 0, 5000)
	from.Stats.AvgPrice7d = 210_000
	from.Ask = aodpSide(200_000)
	to := market("Martlock", 270_000, 260_066, 5000)
	to.Stats.AvgPrice7d = 250_000
	to.Bid = capSide(t6BidLadder...)
	// 卖方也是抓包:卖一只有 2 件的孤单,挂卖参考价站不住 → 挂卖腿被 thin_book 拦
	to.Ask = capSide(depth.Level{Price: 270_000, Qty: 2}, depth.Level{Price: 300_000, Qty: 50})

	r, ok := routeOf(find(from, to), "Thetford", "Martlock")
	if !ok {
		t.Fatal("应该有 Thetford → Martlock 的路线")
	}
	if r.Mode != "taker-taker" {
		t.Fatalf("挂卖被拦之后只剩秒买秒卖,得到 %s", r.Mode)
	}
	if r.SellPrice != 260_000 || r.SellDepthQty != 11 || r.BuyDepthQty != 0 || r.Qty != 11 {
		t.Fatalf("应卖到 260,000、盘口 11 件,得到 卖 %d / 深度 %d / %d / 件数 %d",
			r.SellPrice, r.SellDepthQty, r.BuyDepthQty, r.Qty)
	}
	if want := (260_000*0.96 - 200_000) * 11; math.Abs(r.DailyProfit-want) > 1e-6 {
		t.Fatalf("日收益应为 %.0f,得到 %.0f", want, r.DailyProfit)
	}
	if r.Bottleneck != "depth" || !strings.Contains(strings.Join(r.Warnings, ";"), "只够 11 件") {
		t.Fatalf("瓶颈应是盘口深度并说明只够 11 件,得到 %s / %v", r.Bottleneck, r.Warnings)
	}
	for _, m := range r.Modes {
		if m.SellPrice <= 53_001 {
			t.Fatalf("%s 报了 %d 的卖价:断崖以下的档不该进任何报价", m.Label, m.SellPrice)
		}
	}
	if tm, ok := modeOf(r, "taker-maker"); !ok || tm.Blocked != "thin_book" {
		t.Fatalf("秒买挂卖应带着 thin_book 出现在 modes 里,得到 %+v / %v", tm, ok)
	}
	if r.SellLegSource != screen.SourceCapture || r.BuyLegSource != screen.SourceAODP || r.DepthChecked {
		t.Fatalf("卖出腿来自抓包、买入腿 AODP,不算核过深度,得到 %s / %s / %v",
			r.SellLegSource, r.BuyLegSource, r.DepthChecked)
	}
	if r.ToBid == nil || r.ToBid.Depth == nil || r.ToBid.Depth.QtyNear != 11 || r.FromBid != nil {
		t.Fatalf("盘口边应原样带出(产地没有买价不输出),得到 %+v / %+v", r.ToBid, r.FromBid)
	}
}

// 产地卖方三档:1000×5 / 1010×100 / 1100×1000,销地 AODP 买一 1200。
// 逐档试:吃到 1010 一天赚 142×105 = 14,910;吃到 1100 单件只剩 52,
// 但能做 1,105 件,一天 57,460 —— 选深的那档
func TestRoute_产地卖方阶梯按日收益挑前缀(t *testing.T) {
	build := func(levels ...depth.Level) Route {
		from := market("Lymhurst", 1000, 900, 6000)
		from.Ask = capSide(levels...)
		// 产地买方很薄:挂买被拦,只剩秒买秒卖可比
		from.Bid = capSide(depth.Level{Price: 900, Qty: 5}, depth.Level{Price: 700, Qty: 1000})
		to := market("Martlock", 0, 1200, 6000)
		to.Bid = aodpSide(1200)
		r, ok := routeOf(find(from, to), "Lymhurst", "Martlock")
		if !ok {
			t.Fatal("应该有 Lymhurst → Martlock 的路线")
		}
		return r
	}

	two := build(depth.Level{Price: 1000, Qty: 5}, depth.Level{Price: 1010, Qty: 100})
	if two.BuyPrice != 1010 || two.BuyDepthQty != 105 || math.Abs(two.DailyProfit-14_910) > 1e-6 {
		t.Fatalf("只有两档时应吃到 1010、105 件、日收益 14,910,得到 %d / %d / %.2f",
			two.BuyPrice, two.BuyDepthQty, two.DailyProfit)
	}

	r := build(depth.Level{Price: 1000, Qty: 5}, depth.Level{Price: 1010, Qty: 100}, depth.Level{Price: 1100, Qty: 1000})
	if r.Mode != "taker-taker" {
		t.Fatalf("前提变了:应选秒买秒卖,得到 %s(modes %+v)", r.Mode, r.Modes)
	}
	if r.BuyPrice != 1100 || r.BuyDepthQty != 1105 || r.SellDepthQty != 0 || r.Qty != 1105 {
		t.Fatalf("应吃到 1100、盘口 1,105 件,得到 买 %d / 深度 %d / %d / 件数 %d",
			r.BuyPrice, r.BuyDepthQty, r.SellDepthQty, r.Qty)
	}
	if math.Abs(r.ProfitPerUnit-52) > 1e-9 || math.Abs(r.DailyProfit-57_460) > 1e-6 {
		t.Fatalf("单件 52、日收益 57,460,得到 %.4f / %.4f", r.ProfitPerUnit, r.DailyProfit)
	}
	if !(r.DailyProfit > two.DailyProfit) {
		t.Fatal("吃到 1100 应胜过只吃到 1010")
	}
	if r.Bottleneck != "depth" {
		t.Fatalf("1,105 件比成交量(1,200)和本金(9,090)都紧,瓶颈应是 depth,得到 %s", r.Bottleneck)
	}
}

// 产地买方只有 5 件在收:挂买的两个模式被拦(照样列在 modes 里),秒买的照样能做
func TestRoute_产地买方薄只砍挂买模式(t *testing.T) {
	from := market("Lymhurst", 1000, 950, 5000)
	from.Bid = capSide(depth.Level{Price: 950, Qty: 5}, depth.Level{Price: 700, Qty: 1000})
	to := market("Martlock", 1600, 1500, 5000)
	r, ok := routeOf(find(from, to), "Lymhurst", "Martlock")
	if !ok {
		t.Fatal("秒买的模式不受产地买方影响,路线应该在")
	}
	if strings.HasPrefix(r.Mode, "maker-") {
		t.Fatalf("选用了挂买模式 %s", r.Mode)
	}
	seenBlocked, viableDone := 0, false
	for _, m := range r.Modes {
		isMakerBuy := strings.HasPrefix(m.Mode, "maker-")
		switch {
		case m.Blocked == "":
			if viableDone {
				t.Fatalf("可做的模式应排在被拦的前面: %+v", r.Modes)
			}
			if isMakerBuy {
				t.Fatalf("挂买模式 %s 不该可做", m.Mode)
			}
		default:
			viableDone = true
			if !isMakerBuy || m.Blocked != "no_bid_side" {
				t.Fatalf("只有挂买模式该被 no_bid_side 拦,得到 %s / %s", m.Mode, m.Blocked)
			}
			seenBlocked++
		}
	}
	if seenBlocked != 2 {
		t.Fatalf("挂买秒卖和挂买挂卖都应带着 Blocked 出现在 modes 里,得到 %d 个", seenBlocked)
	}
	if r.Modes[0].Mode != r.Mode {
		t.Fatal("modes 第一个应是选用的那个")
	}
}

// 销地只有卖单时 (卖一 + 0) / 2 是个假中价,以前会算出 −10σ 这种离谱的 z
func TestRoute_销地单边时不算z(t *testing.T) {
	to := market("Martlock", 1600, 0, 5000)
	to.Stats.AvgPrice30d, to.Stats.StdDev30d = 1600, 80
	r, ok := routeOf(find(market("Lymhurst", 1000, 950, 5000), to), "Lymhurst", "Martlock")
	if !ok {
		t.Fatal("挂卖模式可做,路线应该在")
	}
	if r.HasZ || r.ZScore != 0 {
		t.Fatalf("销地单边不该有 z,得到 %v / %v", r.HasZ, r.ZScore)
	}
	if r.Volatility != to.Stats.CV {
		t.Fatal("波动率照常带出")
	}
}

// 现存漏洞:产地买一只剩一张 2 银的占位单、销地只有卖单。比值兜底一组都比不了
// 买一(销地没有买一),只看卖一那组是正常的;挂买腿于是按 3 银成本算,毛利上万倍
func TestRoute_逐腿偏离检查堵住天价毛利(t *testing.T) {
	mk := func() (Market, Market) {
		from := market("Lymhurst", 1000, 2, 5000)
		from.Stats.AvgPrice7d = 1000
		to := market("Martlock", 1300, 0, 5000)
		to.Stats.AvgPrice7d = 1000
		return from, to
	}

	// 先钉住漏洞本身:关掉逐腿检查,就会出一条天价毛利的挂买挂卖
	from, to := mk()
	off := opts()
	off.DeviationMin, off.DeviationMax = 0, 0
	loose, ok := routeOf(Find("T5_CLOTH", "精布", 1, []Market{from, to}, conf.Default().Economics, off, now), "Lymhurst", "Martlock")
	if !ok || loose.Mode != "maker-maker" || loose.Margin < 100 {
		t.Fatalf("前提变了:关掉检查时应出天价毛利的挂买挂卖,得到 %s / %.0f%%", loose.Mode, loose.Margin*100)
	}

	from, to = mk()
	r, ok := routeOf(find(from, to), "Lymhurst", "Martlock")
	if !ok {
		t.Fatal("秒买挂卖是正常的(1000 买、1299 挂),路线应该在")
	}
	if r.Mode != "taker-maker" {
		t.Fatalf("应选秒买挂卖,得到 %s", r.Mode)
	}
	for _, m := range r.Modes {
		if strings.HasPrefix(m.Mode, "maker-") {
			t.Fatalf("挂买腿依托 2 银的买一(0.002x 均价),不该出现,得到 %+v", m)
		}
	}
}

// 剪枝必须是精确的:和暴力枚举所有前缀对的结果逐位相同
func TestBestFill剪枝和暴力枚举一致(t *testing.T) {
	cfg := conf.Default().Economics
	rng := rand.New(rand.NewSource(7))
	for trial := 0; trial < 300; trial++ {
		asks := randomLadder(rng, 1000, +1)
		bids := randomLadder(rng, 1000+int64(rng.Intn(400)), -1)
		from := market("Lymhurst", asks[0].Price, 900, float64(rng.Intn(20000)+100))
		from.Ask = capSide(asks...)
		to := market("Martlock", bids[0].Price+50, bids[0].Price, float64(rng.Intn(20000)+100))
		to.Bid = capSide(bids...)
		o := opts()
		o.Capital = int64(rng.Intn(5_000_000) + 50_000)
		byMarket := math.Min(from.Stats.DailyVolumeQty, to.Stats.DailyVolumeQty) * o.AbsorbRatio

		m := econ.Mode{Buy: econ.Taker, Sell: econ.Taker}
		got, gotOK := bestFill(m, from, to, byMarket, cfg, o)
		want, wantOK := bruteFill(m, from, to, byMarket, cfg, o)
		if gotOK != wantOK || got.DailyProfit != want.DailyProfit || got.BuyPrice != want.BuyPrice ||
			got.SellPrice != want.SellPrice || got.qty != want.qty {
			t.Fatalf("第 %d 组不一致\n剪枝 %v %+v\n暴力 %v %+v", trial, gotOK, got, wantOK, want)
		}
	}
}

// bruteFill 枚举所有前缀对,不剪枝;并列取先遇到的(浅的)。
func bruteFill(m econ.Mode, from, to Market, byMarket float64, cfg conf.Economics, opt Options) (ModeQuote, bool) {
	var best ModeQuote
	found := false
	for _, b := range ladder(from.Ask, true, 0) {
		for _, s := range ladder(to.Bid, true, 0) {
			bk, sk := from.Book, to.Book
			bk.SellMin, sk.BuyMax = b.price, s.price
			u := econ.Quote(bk, sk, m, cfg)
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

func randomLadder(rng *rand.Rand, top int64, dir int64) []depth.Level {
	n := rng.Intn(12) + 1
	out := make([]depth.Level, 0, n)
	p := top
	for i := 0; i < n; i++ {
		out = append(out, depth.Level{Price: p, Qty: int64(rng.Intn(300) + 1)})
		p += dir * int64(rng.Intn(40)+1)
	}
	return out
}

// 两侧各 128 档、全部可盈利:最坏情况下双层循环的开销
func BenchmarkEvaluateLadders(b *testing.B) {
	asks := make([]depth.Level, 128)
	bids := make([]depth.Level, 128)
	for i := range asks {
		asks[i] = depth.Level{Price: 1000 + int64(i), Qty: 10}
		bids[i] = depth.Level{Price: 2000 - int64(i), Qty: 10}
	}
	from := market("Lymhurst", 1000, 900, 10_000_000)
	from.Ask = capSide(asks...)
	to := market("Martlock", 2100, 2000, 10_000_000)
	to.Bid = capSide(bids...)
	markets := []Market{from, to}
	cfg, o := conf.Default().Economics, opts()
	r, ok := routeOf(Find("T5_CLOTH", "精布", 1, markets, cfg, o, now), "Lymhurst", "Martlock")
	if tt, has := modeOf(r, "taker-taker"); !ok || !has || math.IsInf(tt.buyCap, 1) || math.IsInf(tt.sellCap, 1) {
		b.Fatalf("前提变了:秒买秒卖两侧都应按阶梯走,得到 %+v", r)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Find("T5_CLOTH", "精布", 1, markets, cfg, o, now)
	}
}
