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
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct)
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
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct)
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

func TestBuildSideDropsDeeperOrdersWhenNotTruncated(t *testing.T) {
	latest := t0.Add(-1 * time.Minute)
	prev := t0.Add(-40 * time.Minute)
	orders := []store.LiveOrder{
		sellAt(100, 1, latest), sellAt(101, 1, latest),
		sellAt(500, 8, prev), // 整本簿都看到了,它不在里面
	}
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct)
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
	s := buildSide(orders, model.SideRequest, t0, lookupSlack, lookupNearPct)
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
	s := buildSide(orders, model.SideOffer, t0, lookupSlack, lookupNearPct)
	l := s.Levels[0]
	if !near(l.StandingHours, 3) || !near(l.AgeHours, 1.0/60) {
		t.Fatalf("standing=%v age=%v", l.StandingHours, l.AgeHours)
	}
	if s.AgeHours == nil || !near(*s.AgeHours, 1.0/60) || !s.bestSeen.Equal(t0.Add(-time.Minute)) {
		t.Fatalf("age=%v bestSeen=%v", s.AgeHours, s.bestSeen)
	}
}

func TestBuildSideEmpty(t *testing.T) {
	s := buildSide(nil, model.SideOffer, t0, lookupSlack, lookupNearPct)
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
