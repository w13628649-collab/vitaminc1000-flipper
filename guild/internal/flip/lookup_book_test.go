package flip

import (
	"math"
	"testing"
	"time"

	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

var t0 = time.Date(2026, 9, 23, 14, 22, 0, 0, time.UTC)

func ord(city string, q int, side model.Side, price, amt int64, first, last time.Time) store.LiveOrder {
	return store.LiveOrder{City: city, Quality: q, Side: side, Price: price, Amount: amt,
		FirstSeen: first, LastSeen: last}
}

func sellAt(price, amt int64, seen time.Time) store.LiveOrder {
	return ord("Martlock", 1, model.SideOffer, price, amt, seen, seen)
}

func buyAt(price, amt int64, seen time.Time) store.LiveOrder {
	return ord("Martlock", 1, model.SideRequest, price, amt, seen, seen)
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// 实测 T6_LEATHER @ Martlock:上一轮有一张 4906 的卖单,比最近一轮的最低价 4909
// 还便宜。它要是还在,最近一轮必然会出现 —— 所以已经没了,必须丢掉。
// 现有的 QuotesByCity / BestQuotes 在 30 分钟内会把卖一报成 4906。
func TestBuildSideDropsGhostBetterThanLatestWorst(t *testing.T) {
	latest := t0.Add(-2 * time.Minute)
	prev := t0.Add(-25 * time.Minute)
	orders := []store.LiveOrder{
		sellAt(4906, 3, prev), // 残单
		sellAt(4909, 10, latest),
		sellAt(4920, 5, latest),
		sellAt(5100, 7, latest),
	}
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if s.Support.Best != 4909 {
		t.Fatalf("最优价应是最近一轮的 4909,得到 %d", s.Support.Best)
	}
	if s.DroppedOrders != 1 || s.DroppedQty != 3 {
		t.Fatalf("残单应被丢弃: dropped=%d qty=%d", s.DroppedOrders, s.DroppedQty)
	}
	if s.StaleOrders != 0 || len(s.Levels) != 3 {
		t.Fatalf("没截断时不该有 stale 档: stale=%d levels=%d", s.StaleOrders, len(s.Levels))
	}
	if s.far != 5100 || s.Orders != 3 || s.QtyTotal != 22 || s.Truncated {
		t.Fatalf("far=%d orders=%d total=%d truncated=%v", s.far, s.Orders, s.QtyTotal, s.Truncated)
	}
}

// 最近一轮恰好 50 单 = 一页,多半没翻完。比这一页最差价更差的旧单可能还在,
// 保留但标 stale、不进统计;落在这一页价格范围里的旧单照样丢。
func TestBuildSideKeepsDeeperOrdersAsStaleWhenTruncated(t *testing.T) {
	latest := t0.Add(-1 * time.Minute)
	prev := t0.Add(-40 * time.Minute)
	var orders []store.LiveOrder
	for i := int64(0); i < lookupPageSize; i++ {
		orders = append(orders, sellAt(100+i, 1, latest))
	}
	orders = append(orders,
		sellAt(120, 9, prev), // 在翻过的范围里,没再出现 → 丢
		sellAt(155, 4, prev), // 比这一页最差的 149 还远 → stale
		sellAt(160, 6, prev),
	)
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if !s.Truncated {
		t.Fatal("50 单应判为截断")
	}
	if s.DroppedOrders != 1 || s.DroppedQty != 9 {
		t.Fatalf("dropped=%d qty=%d,应只丢 120 那张", s.DroppedOrders, s.DroppedQty)
	}
	if s.StaleOrders != 2 || s.StaleQty != 10 {
		t.Fatalf("stale=%d qty=%d", s.StaleOrders, s.StaleQty)
	}
	if s.LevelCount != 50 || len(s.Levels) != 52 {
		t.Fatalf("level_count=%d levels=%d", s.LevelCount, len(s.Levels))
	}
	last := s.Levels[len(s.Levels)-1]
	if !last.Stale || last.Price != 160 || !s.Levels[50].Stale || s.Levels[49].Stale {
		t.Fatalf("stale 档应排在最后: %+v", s.Levels[49:])
	}
	// stale 不进近价件数和总件数
	if s.QtyTotal != 50 || s.Support.QtyTotal != 50 {
		t.Fatalf("qty_total=%d support.qty_total=%d", s.QtyTotal, s.Support.QtyTotal)
	}
	// 累计一路累下去,stale 也接着累
	if last.CumQty != 60 {
		t.Fatalf("cum=%d", last.CumQty)
	}
	// 吃单不吃 stale
	s.walk(55)
	if s.Fill == nil || s.Fill.Got != 50 || s.Fill.Filled {
		t.Fatalf("fill=%+v", s.Fill)
	}
}

// 续页晚到:第 1 页(100..149)10 分钟前到,第 2 页(150..199)刚到,间隔超过 slack。
// 最近一轮只有第 2 页,但第 1 页是满页,说明第 2 页是它的续页 —— 真实最优价仍是 100。
// 以前的规则把第 1 页整页当残单丢掉,卖单最低报成 150。
func TestBuildSideKeepsEarlierPageWhenLatestIsContinuation(t *testing.T) {
	p1 := t0.Add(-10 * time.Minute)
	p2 := t0.Add(-10 * time.Second)
	var orders []store.LiveOrder
	for i := int64(0); i < lookupPageSize; i++ {
		orders = append(orders, sellAt(100+i, 1, p1), sellAt(150+i, 2, p2))
	}
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if s.Support.Best != 100 || s.DroppedOrders != 0 || s.PrevPageOrders != 50 {
		t.Fatalf("best=%d dropped=%d prev_page=%d", s.Support.Best, s.DroppedOrders, s.PrevPageOrders)
	}
	if s.Orders != 100 || s.QtyTotal != 150 || s.LevelCount != 100 || !s.Truncated || s.far != 199 {
		t.Fatalf("orders=%d total=%d levels=%d truncated=%v far=%d",
			s.Orders, s.QtyTotal, s.LevelCount, s.Truncated, s.far)
	}
	// 最优价那一档的龄是第 1 页的龄,不是第 2 页的
	if !s.bestSeen.Equal(p1) || s.Levels[0].Price != 100 || s.Levels[0].Stale {
		t.Fatalf("bestSeen=%v first=%+v", s.bestSeen, s.Levels[0])
	}

	// 第 2 页不满 = 翻到底了,不算截断
	orders = orders[:0]
	for i := int64(0); i < lookupPageSize; i++ {
		orders = append(orders, sellAt(100+i, 1, p1))
	}
	for i := int64(0); i < 30; i++ {
		orders = append(orders, sellAt(150+i, 1, p2))
	}
	s = buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if s.Support.Best != 100 || s.Orders != 80 || s.Truncated || s.PrevPageOrders != 50 {
		t.Fatalf("best=%d orders=%d truncated=%v prev=%d", s.Support.Best, s.Orders, s.Truncated, s.PrevPageOrders)
	}
}

// 续页之前那一页照样按同一套规则清理:比第 1 页最优价还好、更早的零星单仍是残单。
func TestBuildSideContinuationStillDropsGhostsAheadOfEarlierPage(t *testing.T) {
	ghost := t0.Add(-40 * time.Minute)
	p1 := t0.Add(-10 * time.Minute)
	p2 := t0.Add(-10 * time.Second)
	orders := []store.LiveOrder{sellAt(99, 7, ghost)}
	for i := int64(0); i < lookupPageSize; i++ {
		orders = append(orders, sellAt(100+i, 1, p1), sellAt(150+i, 1, p2))
	}
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if s.Support.Best != 100 || s.DroppedOrders != 1 || s.DroppedQty != 7 || s.Orders != 100 {
		t.Fatalf("best=%d dropped=%d/%d orders=%d", s.Support.Best, s.DroppedOrders, s.DroppedQty, s.Orders)
	}
}

// 比最近一轮最优价还好的旧单只有几张、不成一页 → 最近一轮是从第一页重新翻的,
// 它们要是还在就一定会出现。零星几张不能被当成"前一页"留下来。
func TestBuildSideFewOrdersAheadAreGhostsNotAPage(t *testing.T) {
	prev := t0.Add(-30 * time.Minute)
	latest := t0.Add(-time.Minute)
	var orders []store.LiveOrder
	for i := int64(0); i < 5; i++ {
		orders = append(orders, sellAt(100+i, 1, prev)) // 两轮之间被买走的
	}
	for i := int64(0); i < lookupPageSize; i++ {
		orders = append(orders, sellAt(105+i, 1, latest))
	}
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if s.Support.Best != 105 || s.DroppedOrders != 5 || s.PrevPageOrders != 0 {
		t.Fatalf("best=%d dropped=%d prev=%d", s.Support.Best, s.DroppedOrders, s.PrevPageOrders)
	}
}

// 满页最后一档的价上,更早看到的单可能只是排到了下一页:标 stale,不剔除。
// 没截断时同样的单就是没了。
func TestBuildSideOrderAtWorstOfFullPageIsStale(t *testing.T) {
	latest := t0.Add(-time.Minute)
	prev := t0.Add(-30 * time.Minute)
	var orders []store.LiveOrder
	for i := int64(0); i < lookupPageSize; i++ {
		orders = append(orders, sellAt(100+i, 1, latest))
	}
	orders = append(orders, sellAt(149, 4, prev))
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if !s.Truncated || s.StaleOrders != 1 || s.DroppedOrders != 0 || s.QtyTotal != 50 {
		t.Fatalf("truncated=%v stale=%d dropped=%d total=%d", s.Truncated, s.StaleOrders, s.DroppedOrders, s.QtyTotal)
	}

	orders = []store.LiveOrder{sellAt(100, 1, latest), sellAt(101, 1, latest), sellAt(101, 4, prev)}
	s = buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if s.Truncated || s.StaleOrders != 0 || s.DroppedOrders != 1 {
		t.Fatalf("truncated=%v stale=%d dropped=%d", s.Truncated, s.StaleOrders, s.DroppedOrders)
	}
}

// 装备不筛品质时一页 50 单横跨几个品质(实测 Brecilien T5_SHOES_LEATHER_HELL@2:
// 一次 50 单里 q1 4 张、q2 22 张、q3 21 张、q4 3 张)。单个品质永远凑不到 50,
// 以前的规则因此永远判"翻完了",更深的旧单直接丢、卖单最高当成完整值。
func TestBuildSideTruncationCountsPageAcrossQualities(t *testing.T) {
	page := t0.Add(-time.Minute)
	prev := t0.Add(-30 * time.Minute)
	var all []store.LiveOrder
	add := func(q int, n int, price int64) {
		for i := 0; i < n; i++ {
			all = append(all, ord("Brecilien", q, model.SideOffer, price+int64(i), 1, page, page))
		}
	}
	add(1, 4, 80_000)
	add(2, 22, 81_000)
	add(3, 21, 90_000)
	add(4, 3, 110_000)
	// 上一轮翻到第 2 页才看到的 q1 单
	all = append(all, ord("Brecilien", 1, model.SideOffer, 120_000, 2, prev, prev))

	var q1 []store.LiveOrder
	for _, o := range all {
		if o.Quality == 1 {
			q1 = append(q1, o)
		}
	}
	s := buildSide(q1, model.SideOffer, t0, lookupSlack, lookupNearPct, countResponses(all))
	if !s.Truncated || s.StaleOrders != 1 || s.DroppedOrders != 0 || s.Orders != 4 {
		t.Fatalf("跨品质满页: truncated=%v stale=%d dropped=%d orders=%d",
			s.Truncated, s.StaleOrders, s.DroppedOrders, s.Orders)
	}
	// buildBook 自己按全部品质数页
	b := buildBook("T5_SHOES_LEATHER_HELL@2", "Brecilien", 1, all, t0, 6*time.Hour, 0, conf.Default().Economics)
	if !b.Sell.Truncated || b.Sell.StaleOrders != 1 {
		t.Fatalf("buildBook: truncated=%v stale=%d", b.Sell.Truncated, b.Sell.StaleOrders)
	}
	// 只按 q1 自己数(旧行为)就是 4 张、没截断
	s = buildSide(q1, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if s.Truncated || s.DroppedOrders != 1 {
		t.Fatalf("单品质口径: truncated=%v dropped=%d", s.Truncated, s.DroppedOrders)
	}

	// 整页不满(30 单)就是翻完了,更深的旧单照样剔除
	all = all[:0]
	add(1, 4, 80_000)
	add(2, 26, 81_000)
	all = append(all, ord("Brecilien", 1, model.SideOffer, 120_000, 2, prev, prev))
	q1 = q1[:0]
	for _, o := range all {
		if o.Quality == 1 {
			q1 = append(q1, o)
		}
	}
	s = buildSide(q1, model.SideOffer, t0, lookupSlack, lookupNearPct, countResponses(all))
	if s.Truncated || s.DroppedOrders != 1 || s.StaleOrders != 0 {
		t.Fatalf("不满页: truncated=%v dropped=%d stale=%d", s.Truncated, s.DroppedOrders, s.StaleOrders)
	}
}

// 跨品质的续页:第 1 页(q1 10 张 + q2 40 张)10 分钟前,第 2 页刚到。
// 对 q1 来说前一页只有 10 张,单看 q1 不像一页;整页是满的,所以仍按续页保留。
func TestBuildSideContinuationAcrossQualities(t *testing.T) {
	p1 := t0.Add(-10 * time.Minute)
	p2 := t0.Add(-10 * time.Second)
	var all []store.LiveOrder
	for i := int64(0); i < 10; i++ {
		all = append(all, ord("Martlock", 1, model.SideOffer, 1000+i, 1, p1, p1))
	}
	for i := int64(0); i < 40; i++ {
		all = append(all, ord("Martlock", 2, model.SideOffer, 1000+i, 1, p1, p1))
	}
	for i := int64(0); i < 20; i++ {
		all = append(all, ord("Martlock", 1, model.SideOffer, 1100+i, 1, p2, p2))
	}
	var q1 []store.LiveOrder
	for _, o := range all {
		if o.Quality == 1 {
			q1 = append(q1, o)
		}
	}
	s := buildSide(q1, model.SideOffer, t0, lookupSlack, lookupNearPct, countResponses(all))
	if s.Support.Best != 1000 || s.Orders != 30 || s.PrevPageOrders != 10 || s.DroppedOrders != 0 {
		t.Fatalf("best=%d orders=%d prev=%d dropped=%d", s.Support.Best, s.Orders, s.PrevPageOrders, s.DroppedOrders)
	}
}

func TestBuildSideDropsDeeperOrdersWhenNotTruncated(t *testing.T) {
	latest := t0.Add(-1 * time.Minute)
	prev := t0.Add(-40 * time.Minute)
	orders := []store.LiveOrder{
		sellAt(100, 1, latest), sellAt(101, 1, latest),
		sellAt(500, 8, prev), // 整本簿都看到了,它不在里面
	}
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if s.DroppedOrders != 1 || s.StaleOrders != 0 || len(s.Levels) != 2 {
		t.Fatalf("dropped=%d stale=%d levels=%d", s.DroppedOrders, s.StaleOrders, len(s.Levels))
	}
}

// 买单从高到低。1 银占位单在总件数里,但不在近价窗口里。
func TestBuildSideBuyLadder(t *testing.T) {
	seen := t0.Add(-3 * time.Minute)
	orders := []store.LiveOrder{
		buyAt(1, 2000, seen),
		buyAt(260, 5, seen),
		buyAt(266, 1, seen), buyAt(266, 2, seen),
		buyAt(269, 4, seen),
		buyAt(200, 30, seen),
	}
	s := buildSide(orders, model.SideRequest, t0, lookupSlack, lookupNearPct, nil)
	want := []int64{269, 266, 260, 200, 1}
	if len(s.Levels) != len(want) {
		t.Fatalf("levels=%d", len(s.Levels))
	}
	for i, p := range want {
		if s.Levels[i].Price != p {
			t.Fatalf("第 %d 档 %d,期望 %d", i, s.Levels[i].Price, p)
		}
	}
	if s.Levels[1].Qty != 3 || s.Levels[1].Orders != 2 {
		t.Fatalf("同价两张单应合并: %+v", s.Levels[1])
	}
	if s.Levels[2].CumQty != 12 {
		t.Fatalf("cum=%d", s.Levels[2].CumQty)
	}
	if !near(s.Levels[3].Off, 1-200.0/269.0) {
		t.Fatalf("off=%v", s.Levels[3].Off)
	}
	// 5% 以内:269 / 266 / 260(≥ 255.55)
	if s.Support.QtyNear != 12 || s.Support.LevelsNear != 3 || s.Support.QtyTotal != 2042 {
		t.Fatalf("support=%+v", s.Support)
	}
	if !near(s.Support.GapAfterNear, 1-200.0/269.0) {
		t.Fatalf("gap=%v", s.Support.GapAfterNear)
	}
	if s.far != 1 {
		t.Fatalf("far=%d", s.far)
	}
}

func TestBuildSideAges(t *testing.T) {
	orders := []store.LiveOrder{
		ord("Martlock", 1, model.SideOffer, 100, 1, t0.Add(-3*time.Hour), t0.Add(-2*time.Minute)),
		ord("Martlock", 1, model.SideOffer, 100, 1, t0.Add(-1*time.Hour), t0.Add(-1*time.Minute)),
	}
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	l := s.Levels[0]
	if !near(l.StandingHours, 3) || !near(l.AgeHours, 1.0/60) {
		t.Fatalf("standing=%v age=%v", l.StandingHours, l.AgeHours)
	}
	if s.AgeHours == nil || !near(*s.AgeHours, 1.0/60) || !s.bestSeen.Equal(t0.Add(-time.Minute)) {
		t.Fatalf("age=%v bestSeen=%v", s.AgeHours, s.bestSeen)
	}
}

func TestBuildSideEmpty(t *testing.T) {
	s := buildSide(nil, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)
	if s.has() || s.Levels == nil || len(s.Levels) != 0 || s.AgeHours != nil {
		t.Fatalf("空的一侧: %+v", s)
	}
	s.walk(10)
	if s.Fill == nil || s.Fill.Got != 0 {
		t.Fatalf("fill=%+v", s.Fill)
	}
}

func TestBuildBookFiltersCellAndSummarizes(t *testing.T) {
	cfg := conf.Default()
	seen := t0.Add(-time.Minute)
	orders := []store.LiveOrder{
		sellAt(339, 42, seen), sellAt(345, 100, seen),
		buyAt(286, 50, seen),
		// 别的格子不该混进来
		ord("Thetford", 1, model.SideOffer, 10, 1, seen, seen),
		ord("Martlock", 2, model.SideRequest, 999, 1, seen, seen),
	}
	b := buildBook("T4_METALBAR", "Martlock", 1, orders, t0, 6*time.Hour, 60, cfg.Economics)
	if b.Sell.Support.Best != 339 || b.Buy.Support.Best != 286 {
		t.Fatalf("sell=%d buy=%d", b.Sell.Support.Best, b.Buy.Support.Best)
	}
	if b.Sell.Fill == nil || b.Sell.Fill.Got != 60 || b.Sell.Fill.Worst != 345 {
		t.Fatalf("sell fill=%+v", b.Sell.Fill)
	}
	if b.Buy.Fill == nil || b.Buy.Fill.Got != 50 || b.Buy.Fill.Filled {
		t.Fatalf("buy fill=%+v", b.Buy.Fill)
	}
	bk := econ.Book{SellMin: 339, BuyMax: 286}
	u := econ.Quote(bk, bk, econ.Mode{Buy: econ.Maker, Sell: econ.Maker}, cfg.Economics)
	s := b.Summary
	if s.Margin == nil || !near(*s.Margin, u.Margin) || s.MyBid != 287 || s.MyAsk != 338 {
		t.Fatalf("summary=%+v", s)
	}
	if s.Spread == nil || !near(*s.Spread, 339.0/286.0-1) {
		t.Fatalf("spread=%v", s.Spread)
	}
	if !near(s.Breakeven, 1.025/0.935-1) {
		t.Fatalf("breakeven=%v", s.Breakeven)
	}
	if b.WindowHours != 6 || b.PageSize != lookupPageSize {
		t.Fatalf("params %+v", b)
	}

	// 一侧没有 → 不算价差
	b = buildBook("T4_METALBAR", "Martlock", 1, orders[:2], t0, 6*time.Hour, 0, cfg.Economics)
	if b.Summary.Margin != nil || b.Summary.BestBuy != 0 || b.Sell.Fill != nil {
		t.Fatalf("单侧 summary=%+v fill=%v", b.Summary, b.Sell.Fill)
	}
}
