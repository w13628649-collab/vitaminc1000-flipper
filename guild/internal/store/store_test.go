package store

import (
	"context"
	"testing"
	"time"

	"albion-guild/internal/model"
)

// liveLastSeen 直接读 live 表的 last_seen,测试专用。
func liveLastSeen(t *testing.T, st *Store, id int64) time.Time {
	t.Helper()
	var ts time.Time
	if err := st.pool.QueryRow(context.Background(),
		`SELECT last_seen FROM market_order_live WHERE order_id = $1`, id).Scan(&ts); err != nil {
		t.Fatalf("读 order %d 的 last_seen: %v", id, err)
	}
	return ts
}

// touch 带的是每张单各自的观测时间,乱序到达很正常(断网重传、两个成员
// 先后上传同一眼)。last_seen 只能前进,旧观测不能把它往回拨
func TestTouchOrders_只前进不后退(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

	mk := func(id int64) model.MarketOrder {
		return model.MarketOrder{
			OrderID: id, ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1,
			Side: model.SideOffer, UnitPrice: 100, Amount: 5, ObservedAt: T.Add(-10 * time.Minute),
		}
	}
	if err := st.WriteOrders(ctx, "甲", []model.MarketOrder{mk(1), mk(2)}); err != nil {
		t.Fatal(err)
	}
	if err := st.TouchOrders(ctx, map[int64]time.Time{
		1: T.Add(-time.Minute),
		2: T.Add(-20 * time.Minute),
		3: T, // 库里没有的单:不报错、不凭空建行
	}); err != nil {
		t.Fatal(err)
	}

	if got := liveLastSeen(t, st, 1); !got.Equal(T.Add(-time.Minute)) {
		t.Fatalf("单 1 应前进到 T−1m,得到 %v", got)
	}
	if got := liveLastSeen(t, st, 2); !got.Equal(T.Add(-10 * time.Minute)) {
		t.Fatalf("单 2 应保持 T−10m,得到 %v", got)
	}
	var n int
	if err := st.pool.QueryRow(ctx, `SELECT count(*) FROM market_order_live`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("touch 不该建行,live 表应有 2 行,得到 %d", n)
	}
}
