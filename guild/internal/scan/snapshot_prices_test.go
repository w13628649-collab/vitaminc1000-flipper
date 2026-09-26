package scan

import (
	"testing"
	"time"

	"albion-guild/internal/aodp"
)

func st(t time.Time) aodp.Stamp { return aodp.Stamp{T: t} }

// 四个价各看各的时间戳:同一条记录里卖价新、买价可能反而旧。0 价 / 零时间是"AODP 没数据",
// 不是"现在没有挂单",不能盖掉旧的真实报价
func TestNewerPrice_逐字段新的胜(t *testing.T) {
	t0 := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	base := aodp.PriceRecord{ItemID: "A", City: "X", Quality: 1,
		SellPriceMin: 100, SellPriceMinDate: st(t0),
		SellPriceMax: 200, SellPriceMaxDate: st(t0),
		BuyPriceMin: 1, BuyPriceMinDate: st(t0),
		BuyPriceMax: 80, BuyPriceMaxDate: st(t0)}
	add := aodp.PriceRecord{ItemID: "A", City: "X", Quality: 1,
		SellPriceMin: 95, SellPriceMinDate: st(t0.Add(time.Minute)), // 更新 → 换
		SellPriceMax: 250, SellPriceMaxDate: st(t0.Add(-time.Minute)), // 更旧 → 不换
		BuyPriceMin: 0, BuyPriceMinDate: st(t0.Add(time.Hour)), // 0 价 → 不换
		BuyPriceMax: 85, BuyPriceMaxDate: aodp.Stamp{}} // 没时间戳 → 不换
	got, changed := NewerPrice(base, add)
	if !changed || got.SellPriceMin != 95 || !got.SellPriceMinDate.T.Equal(t0.Add(time.Minute)) ||
		got.SellPriceMax != 200 || got.BuyPriceMin != 1 || got.BuyPriceMax != 80 {
		t.Fatalf("只该换卖单最低价,得到 %+v / %v", got, changed)
	}
	if _, changed := NewerPrice(got, got); changed {
		t.Fatal("同一条不算换")
	}
	// 旧的那一格没数据:新的有就用
	got, changed = NewerPrice(aodp.PriceRecord{ItemID: "A", City: "X", Quality: 1}, base)
	if !changed || got != base {
		t.Fatalf("空的一格应整条换成有数据的,得到 %+v", got)
	}
}

func TestSnapshotWithPrices_只更新已有的格子且不动原快照(t *testing.T) {
	t0 := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	s := &Snapshot{FetchedAt: t0, ItemIDs: []string{"A"}, Prices: []aodp.PriceRecord{
		{ItemID: "A", City: "X", Quality: 1, SellPriceMin: 100, SellPriceMinDate: st(t0)},
		{ItemID: "A", City: "Y", Quality: 1, SellPriceMin: 300, SellPriceMinDate: st(t0)},
	}}
	if s.WithPrices(nil) != s {
		t.Fatal("没有可并的应原样返回")
	}
	old := []aodp.PriceRecord{{ItemID: "A", City: "X", Quality: 1, SellPriceMin: 90, SellPriceMinDate: st(t0.Add(-time.Hour))}}
	if s.WithPrices(old) != s {
		t.Fatal("全比快照旧的应原样返回")
	}
	n := s.WithPrices([]aodp.PriceRecord{
		{ItemID: "A", City: "X", Quality: 1, SellPriceMin: 90, SellPriceMinDate: st(t0.Add(time.Minute))},
		{ItemID: "A", City: "X", Quality: 1, SellPriceMin: 91, SellPriceMinDate: st(t0.Add(2 * time.Minute))}, // 同一格两条:取新的
		{ItemID: "A", City: "X", Quality: 2, SellPriceMin: 50, SellPriceMinDate: st(t0.Add(time.Minute))},     // 快照没有的格子:不添
		{ItemID: "B", City: "X", Quality: 1, SellPriceMin: 50, SellPriceMinDate: st(t0.Add(time.Minute))},     // 快照没有的物品:不添
	})
	if n == s || len(n.Prices) != 2 || n.Prices[0].SellPriceMin != 91 || n.Prices[1].SellPriceMin != 300 {
		t.Fatalf("应只把 X 那一格换成 91,得到 %+v", n.Prices)
	}
	if s.Prices[0].SellPriceMin != 100 || !n.FetchedAt.Equal(t0) || len(n.ItemIDs) != 1 {
		t.Fatalf("原快照不能改,其余字段照抄,得到 %+v / %+v", s.Prices[0], n)
	}
}
