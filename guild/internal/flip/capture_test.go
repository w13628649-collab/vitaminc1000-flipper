package flip

import (
	"context"
	"errors"
	"testing"
	"time"

	"albion-guild/internal/book"
	"albion-guild/internal/conf"
	"albion-guild/internal/ingest"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

type ladderCall struct {
	keys  []model.QuoteKey
	since time.Time
}

// fakeLadder 记下每次读簿的参数,按事先放好的 orders 回请求到的 物品 × 城市 × 方向
// 上的单。和 store.BookOrders 的约定一致:**所有品质**都回(调用方按请求的 key 丢掉
// 多余的),Page 按整次响应跨品质数好,since 之前的不回。
type fakeLadder struct {
	calls  []ladderCall
	orders []store.LiveOrder
	err    error
}

func (f *fakeLadder) BookOrders(_ context.Context, keys []model.QuoteKey, since time.Time) ([]store.LiveOrder, error) {
	f.calls = append(f.calls, ladderCall{keys, since})
	if f.err != nil {
		return nil, f.err
	}
	type scope struct {
		item, city string
		side       model.Side
	}
	want := map[scope]bool{}
	for _, k := range keys {
		want[scope{k.ItemID, k.LocationID, k.Side}] = true
	}
	var in []store.LiveOrder
	for _, o := range f.orders {
		if want[scope{o.ItemID, o.City, o.Side}] && o.LastSeen.After(since) {
			in = append(in, o)
		}
	}
	return book.WithPages(in), nil
}

func (f *fakeLadder) CapturedItems(_ context.Context, cities []string, qualities []int,
	since time.Time) ([]store.CapturedItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []store.CapturedItem{{ItemID: "T7_LEATHER", Qty: 60, Orders: 3, LastSeen: since}}, nil
}

type fakeConflicts map[model.QuoteKey]time.Time

func (f fakeConflicts) ConflictedSince(keys []model.QuoteKey, since time.Time) map[model.QuoteKey]time.Time {
	out := map[model.QuoteKey]time.Time{}
	for _, k := range keys {
		if at, ok := f[k]; ok && at.After(since) {
			out[k] = at
		}
	}
	return out
}

var capT = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func capKeys() (a, b model.QuoteKey) {
	a = model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Martlock", Quality: 1, Side: model.SideOffer}
	b = model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Thetford", Quality: 1, Side: model.SideOffer}
	return a, b
}

// lo 是 k 这个盘口边上的一张单。
func lo(k model.QuoteKey, price, amt int64, seen time.Time) store.LiveOrder {
	return store.LiveOrder{ItemID: k.ItemID, City: k.LocationID, Quality: int(k.Quality), Side: k.Side,
		Price: price, Amount: amt, FirstSeen: seen, LastSeen: seen}
}

// withGhost 是一个带残单的卖方盘口:100 是上一轮看到的、比这一轮卖一还便宜,
// 这一轮(at 前后 1 分钟)是 104 / 105 / 110。
func withGhost(k model.QuoteKey, at time.Time) []store.LiveOrder {
	return []store.LiveOrder{
		lo(k, 100, 5, at.Add(-10*time.Minute)),
		lo(k, 104, 4, at.Add(-time.Minute)),
		lo(k, 105, 7, at),
		lo(k, 110, 3, at),
	}
}

func TestStoreBooks_一次读完并整理成扫描的形状(t *testing.T) {
	a, b := capKeys()
	aQ2 := a
	aQ2.Quality = 2 // 没请求的品质:BookOrders 会顺带回来,不能混进结果
	empty := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1, Side: model.SideOffer}
	orders := append(withGhost(a, capT), lo(aQ2, 50, 9, capT), lo(b, 200, 1, capT))
	lr := &fakeLadder{orders: orders}
	sb := &storeBooks{st: lr, slack: 2 * time.Minute, window: 6 * time.Hour, levels: 2}
	since := capT.Add(-6 * time.Hour)

	got, err := sb.CaptureBooks(context.Background(), []model.QuoteKey{a, b, empty}, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(lr.calls) != 1 || len(lr.calls[0].keys) != 3 || !lr.calls[0].since.Equal(since) {
		t.Fatalf("应一次查询读完全部 key、since 原样传下去,得到 %+v", lr.calls)
	}
	if len(got) != 2 {
		t.Fatalf("只该有 a、b 两个盘口边(没请求的 q2、没数据的 Lymhurst 都不在),得到 %d", len(got))
	}
	cs := got[a]
	// book_levels=2:档数截到 2,合计和档数不截
	if len(cs.Levels) != 2 || cs.Levels[0].Price != 104 || cs.Levels[0].Qty != 4 || cs.Levels[1].Price != 105 ||
		!cs.LevelSeen[0].Equal(capT.Add(-time.Minute)) || cs.LevelCount != 3 || cs.QtyTotal != 14 {
		t.Fatalf("阶梯应是 104×4 / 105×7(共 3 档 14 件),得到 %+v", cs)
	}
	if cs.Ghosts != 1 || !cs.Newest.Equal(capT) || cs.Conflicted || cs.PageTruncated || cs.StaleOrders != 0 {
		t.Fatalf("应剔 1 张残单、Newest=T、没串城、没满页,得到 %+v", cs)
	}
	if cb := got[b]; len(cb.Levels) != 1 || cb.Levels[0].Price != 200 {
		t.Fatalf("b 应是 200×1,得到 %+v", cb)
	}
}

// 串过城的盘口,最近一眼可能就是错归的单:暂停残单剔除(slack 放到窗口 + slack),
// 并标出来让扫描摘要能报个数。还是一次查询,只是整理时 slack 不同
func TestStoreBooks_串过城的盘口暂停剔除(t *testing.T) {
	a, b := capKeys()
	since := capT.Add(-6 * time.Hour)
	lr := &fakeLadder{orders: append(withGhost(a, capT), withGhost(b, capT)...)}
	sb := &storeBooks{
		st: lr, slack: 2 * time.Minute, window: 6 * time.Hour, levels: 128,
		conflicts: fakeConflicts{
			a: capT.Add(-time.Hour),
			b: capT.Add(-7 * time.Hour), // 窗口之前的串城不算
		},
	}
	got, err := sb.CaptureBooks(context.Background(), []model.QuoteKey{a, b}, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(lr.calls) != 1 {
		t.Fatalf("串城的 key 不用另读,应只查一次,得到 %d 次", len(lr.calls))
	}
	if ca := got[a]; !ca.Conflicted || ca.Ghosts != 0 || ca.Levels[0].Price != 100 || ca.LevelCount != 4 {
		t.Fatalf("a 串过城:应暂停剔除、卖一留着 100,得到 %+v", ca)
	}
	if cb := got[b]; cb.Conflicted || cb.Ghosts != 1 || cb.Levels[0].Price != 104 {
		t.Fatalf("b 的串城在窗口之前:照常剔除,得到 %+v", cb)
	}
}

type nopDirty struct{}

func (nopDirty) MarkDirty(model.QuoteKey) {}

// 真 Ingestor 接进来:同一张单先报 Martlock(3008)再报 Thetford(0007),
// 两个盘口都该被当成串过城、暂停剔除。钉住的是两边盘口键的口径一致
// (ingest 收敛后的城市名 == 扫描拼 key 用的城市名)
func TestStoreBooks_接真Ingestor的串城记录(t *testing.T) {
	ing, err := ingest.New(nil, nopDirty{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, loc := range []string{"3008", "0007"} {
		ing.Submit(model.UploadBatch{Reporter: "测试", Orders: []model.MarketOrder{{
			OrderID: 42, ItemID: "T5_CLOTH", LocationID: loc, Quality: 1,
			Side: model.SideOffer, UnitPrice: 100, Amount: 1, ObservedAt: now.Add(-time.Minute),
		}}})
	}
	a, b := capKeys()
	other := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1, Side: model.SideOffer}
	var orders []store.LiveOrder
	for _, k := range []model.QuoteKey{a, b, other} {
		orders = append(orders, withGhost(k, now)...)
	}
	lr := &fakeLadder{orders: orders}
	sb := &storeBooks{st: lr, conflicts: ing, slack: 2 * time.Minute, window: 6 * time.Hour, levels: 128}
	got, err := sb.CaptureBooks(context.Background(), []model.QuoteKey{a, b, other}, now.Add(-6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got[a].Conflicted || !got[b].Conflicted || got[other].Conflicted {
		t.Fatalf("Martlock、Thetford 应标串城,Lymhurst 不该,得到 %v %v %v",
			got[a].Conflicted, got[b].Conflicted, got[other].Conflicted)
	}
	if got[a].Ghosts != 0 || got[b].Ghosts != 0 || got[other].Ghosts != 1 || len(lr.calls) != 1 {
		t.Fatalf("串城的两个 key 暂停剔除、Lymhurst 照常剔,且只查一次,得到 %d/%d/%d 残单、%d 次查询",
			got[a].Ghosts, got[b].Ghosts, got[other].Ghosts, len(lr.calls))
	}
}

func TestStoreBooks_读簿出错原样返回(t *testing.T) {
	a, _ := capKeys()
	sb := &storeBooks{st: &fakeLadder{err: errors.New("boom")}, slack: time.Minute, window: time.Hour, levels: 8}
	if _, err := sb.CaptureBooks(context.Background(), []model.QuoteKey{a}, capT); err == nil {
		t.Fatal("读簿出错应返回错误,扫描那边据此退回纯 AODP")
	}
}

func TestStoreBooks_列抓包物品转换形状(t *testing.T) {
	sb := &storeBooks{st: &fakeLadder{}}
	got, err := sb.CapturedItems(context.Background(), []string{"Martlock"}, []int{1}, capT)
	if err != nil || len(got) != 1 || got[0].ItemID != "T7_LEATHER" || got[0].Qty != 60 {
		t.Fatalf("应转成 scan.CapturedItem,得到 %+v / %v", got, err)
	}
}

func TestServiceBooks_没有库时不给读簿来源(t *testing.T) {
	s := &Service{}
	if s.books() != nil {
		t.Fatal("没有库时应返回 nil 接口,不能是装着 nil 指针的非 nil 接口")
	}
	if q, err := s.BestQuotes(context.Background(), []model.QuoteKey{{ItemID: "x"}}, time.Minute); q != nil || err != nil {
		t.Fatalf("没有库时报价应为空,得到 %v / %v", q, err)
	}
}

// WS 推送和 /api/quotes 的最优价走和扫描同一套读簿:整理之后的第一档。
// 审查 E 轮原样:第一眼卖一 2900、2905,125s 后第二眼只剩 2905、2910。
// At 取这一边的最近一眼,不取最优档自己的 last_seen——新卖一 2905 的 last_seen
// 比 2910 早的话,Fanout 的乱序闸门会把这条更新吞掉
func TestBestQuotes_剔除残单后的第一档且时间取最近一眼(t *testing.T) {
	a, b := capKeys()
	now := time.Now().UTC()
	second := now.Add(-time.Minute)
	first := second.Add(-125 * time.Second)
	lr := &fakeLadder{orders: []store.LiveOrder{
		lo(a, 2900, 5, first), // 第二眼没再看到:已被买走
		lo(a, 2905, 12, second.Add(-30*time.Second)),
		lo(a, 2905, 8, second.Add(-30*time.Second)),
		lo(a, 2910, 7, second),
		lo(a, 2800, 9, now.Add(-40*time.Minute)), // fresh 窗口外:读都不读
		// b 最近串过城:20 分钟前那张 700 不剔
		lo(b, 700, 1, now.Add(-20*time.Minute)),
		lo(b, 777, 1, now),
	}}
	cfg := conf.Default()
	s := &Service{Cfg: cfg, ladder: lr, Conflicts: fakeConflicts{b: now.Add(-5 * time.Minute)}}

	got, err := s.BestQuotes(context.Background(), []model.QuoteKey{a, b}, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Key != a.String() || got[1].Key != b.String() {
		t.Fatalf("应按 key 排出两条,得到 %+v", got)
	}
	if q := got[0]; q.Price != 2905 || q.Depth != 20 || q.Orders != 2 || !q.At.Equal(second) {
		t.Fatalf("应是第一档 2905×20(2 张),时间取最近一眼 %v,得到 %+v", second, q)
	}
	if q := got[1]; q.Price != 700 {
		t.Fatalf("串过城的 b 应暂停剔除、卖一 700,得到 %+v", q)
	}
	// 一次查询,窗口 = now − fresh
	if len(lr.calls) != 1 || len(lr.calls[0].keys) != 2 {
		t.Fatalf("正常 key 和串城 key 应一次读完,得到 %+v", lr.calls)
	}
	if d := now.Add(-30 * time.Minute).Sub(lr.calls[0].since); d < -time.Second || d > time.Second {
		t.Fatalf("窗口应是 now−fresh,得到 since=%v", lr.calls[0].since)
	}
}
