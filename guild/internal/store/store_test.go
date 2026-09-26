package store

import (
	"context"
	"testing"
	"time"

	"albion-guild/internal/book"
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

// 一轮 flush 里变了的单走 upsert、没变的单走 touch。两路要是分开提交,
// 中间被读簿读到就是"半眼":变了的单已把 newest 推到这一眼,没变的还停在
// 上一眼,段内的真实挂单全被判成幽灵。这里用另一个事务的行锁把 Flush 卡在
// touch 那一步,卡住期间从别的连接读簿,只能看到完整的上一眼
func TestFlush_一眼只提交一次(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	k := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1, Side: model.SideOffer}
	mk := func(id, price int64, amount int32, at time.Time) model.MarketOrder {
		return model.MarketOrder{OrderID: id, ItemID: k.ItemID, LocationID: k.LocationID,
			Quality: k.Quality, Side: k.Side, UnitPrice: price, Amount: amount, ObservedAt: at}
	}
	// 和线上读簿同一条路:BookOrders 取数、book.Build 剔残单
	read := func() book.Side {
		t.Helper()
		return readSide(t, st, k, T.Add(-6*time.Hour), 120*time.Second)
	}

	// 上一眼 @T−10m
	if err := st.WriteOrders(ctx, "甲", []model.MarketOrder{
		mk(1, 100, 5, T.Add(-10*time.Minute)),
		mk(2, 105, 7, T.Add(-10*time.Minute)),
		mk(3, 110, 3, T.Add(-10*time.Minute)),
	}); err != nil {
		t.Fatal(err)
	}

	lock, err := st.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback(ctx) }()
	if _, err := lock.Exec(ctx, `SELECT 1 FROM market_order_live WHERE order_id = 1 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}

	// 这一眼 @T:110 被买走一件(变了),100 和 105 没变(touch)
	done := make(chan error, 1)
	go func() {
		done <- st.Flush(ctx,
			map[string][]model.MarketOrder{"乙": {mk(3, 110, 2, T)}},
			map[int64]time.Time{1: T, 2: T})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		if err := st.pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("Flush 应卡在单 1 的行锁上,却提前返回了: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("等不到 Flush 卡在行锁上")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if b := read(); b.Dropped != 0 || !sameLevels(levelsOf(b), []lvl{{100, 5}, {105, 7}, {110, 3}}) {
		t.Fatalf("Flush 没提交时只能看到完整的上一眼,读到了 %v / %d 幽灵", levelsOf(b), b.Dropped)
	}

	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	b := read()
	if b.Dropped != 0 || !sameLevels(levelsOf(b), []lvl{{100, 5}, {105, 7}, {110, 2}}) || !b.Newest.Equal(T) {
		t.Fatalf("提交后应是完整的这一眼,得到 %v / %d 幽灵 / newest %v", levelsOf(b), b.Dropped, b.Newest)
	}
	if got := readLive(t, st, 3); got.reporter != "乙" {
		t.Fatalf("变了的那张单 reporter 应为乙,得到 %q", got.reporter)
	}
}

// 一轮里好几个人报了同一张单:各人的观测都进 event,当前态取观测时间最晚的那条,
// 和 map 遍历顺序、谁先交无关
func TestFlush_多人同一张单取最晚观测(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	o := model.MarketOrder{OrderID: 7, ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1,
		Side: model.SideOffer, UnitPrice: 100, Amount: 5}
	a, b, c := o, o, o
	a.Amount, a.ObservedAt = 5, T.Add(-2*time.Minute)
	b.Amount, b.ObservedAt = 3, T
	c.Amount, c.ObservedAt = 4, T.Add(-time.Minute)
	if err := st.Flush(ctx, map[string][]model.MarketOrder{"甲": {a}, "乙": {b}, "丙": {c}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := readLive(t, st, 7); got.amount != 3 || got.reporter != "乙" || !got.lastSeen.Equal(T) {
		t.Fatalf("当前态应为乙的 ×3 @T,得到 %+v", got)
	}
	var n int
	if err := st.pool.QueryRow(ctx, `SELECT count(*) FROM market_order_event WHERE order_id = 7`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("三次观测都该进 event,得到 %d", n)
	}
}

// liveRow 读 live 表里一张单的当前态,测试专用。
type liveRow struct {
	price    int64
	amount   int32
	lastSeen time.Time
	reporter string
}

func readLive(t *testing.T, st *Store, id int64) liveRow {
	t.Helper()
	var r liveRow
	if err := st.pool.QueryRow(context.Background(),
		`SELECT unit_price, amount, last_seen, reporter FROM market_order_live WHERE order_id = $1`, id).
		Scan(&r.price, &r.amount, &r.lastSeen, &r.reporter); err != nil {
		t.Fatalf("读 order %d: %v", id, err)
	}
	return r
}

// 乱序晚到的旧观测(断网重传、两个成员先后上传)只要状态和当前不同,
// ingest 服务重启后 LRU 是空的,照样会当成"变了"送来。它不能把新状态盖回旧状态:
// 否则库里是"旧件数 + 新 last_seen",读簿会把它当成最近一眼的一部分。
// 但它照样进 event 表——observed_at 是真实时间,历史不该丢
func TestWriteOrders_晚到的旧观测不回写当前态(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

	cur := model.MarketOrder{
		OrderID: 42, ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1,
		Side: model.SideOffer, UnitPrice: 100, Amount: 3, ObservedAt: T,
	}
	if err := st.WriteOrders(ctx, "乙", []model.MarketOrder{cur}); err != nil {
		t.Fatal(err)
	}
	old := cur
	old.UnitPrice, old.Amount, old.ObservedAt = 101, 5, T.Add(-5*time.Minute)
	if err := st.WriteOrders(ctx, "甲", []model.MarketOrder{old}); err != nil {
		t.Fatal(err)
	}

	if got := readLive(t, st, 42); got.price != 100 || got.amount != 3 ||
		!got.lastSeen.Equal(T) || got.reporter != "乙" {
		t.Fatalf("当前态应保持 100×3 @T 乙,得到 %+v", got)
	}
	var n int
	if err := st.pool.QueryRow(ctx,
		`SELECT count(*) FROM market_order_event WHERE order_id = 42`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("两次观测都该进 event 表,得到 %d 行", n)
	}

	// 同一时刻的另一份观测照常覆盖:谁对谁错判断不了,不能因此卡死
	same := cur
	same.Amount = 2
	if err := st.WriteOrders(ctx, "丙", []model.MarketOrder{same}); err != nil {
		t.Fatal(err)
	}
	if got := readLive(t, st, 42); got.amount != 2 || got.reporter != "丙" {
		t.Fatalf("同一时刻的观测应覆盖,得到 %+v", got)
	}
}
