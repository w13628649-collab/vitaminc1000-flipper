package flip

import (
	"context"
	"errors"
	"testing"
	"time"

	"albion-guild/internal/ingest"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

type ladderCall struct {
	keys  []model.QuoteKey
	since time.Time
	slack time.Duration
	lv    int
}

// fakeLadder 记下每次读簿的参数,每个请求到的 key 回一档 100×1。
type fakeLadder struct {
	calls []ladderCall
	err   error
}

func (f *fakeLadder) BookSides(_ context.Context, keys []model.QuoteKey, since time.Time,
	slack time.Duration, maxLevels int) (map[model.QuoteKey]store.BookSide, error) {
	f.calls = append(f.calls, ladderCall{keys, since, slack, maxLevels})
	if f.err != nil {
		return nil, f.err
	}
	out := map[model.QuoteKey]store.BookSide{}
	for _, k := range keys {
		out[k] = store.BookSide{
			Levels: []store.BookLevel{{Price: 100, Depth: 1, Orders: 1, Seen: since.Add(time.Hour)}},
			Newest: since.Add(time.Hour), QtyTotal: 7, LevelCount: 3, Ghosts: 2,
		}
	}
	return out, nil
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

func TestStoreBooks_没有串城时一次读完并转换形状(t *testing.T) {
	a, b := capKeys()
	lr := &fakeLadder{}
	sb := &storeBooks{st: lr, slack: 2 * time.Minute, window: 6 * time.Hour, levels: 128}
	since := capT.Add(-6 * time.Hour)
	got, err := sb.CaptureBooks(context.Background(), []model.QuoteKey{a, b}, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(lr.calls) != 1 || lr.calls[0].slack != 2*time.Minute || lr.calls[0].lv != 128 || !lr.calls[0].since.Equal(since) {
		t.Fatalf("应读一次、slack=2m、128 档,得到 %+v", lr.calls)
	}
	cs := got[a]
	if len(cs.Levels) != 1 || cs.Levels[0].Price != 100 || cs.Levels[0].Qty != 1 ||
		!cs.LevelSeen[0].Equal(since.Add(time.Hour)) || cs.QtyTotal != 7 || cs.LevelCount != 3 ||
		cs.Ghosts != 2 || cs.Conflicted {
		t.Fatalf("形状转换不对: %+v", cs)
	}
}

// 串过城的盘口,最近一眼可能就是错归的单:暂停幽灵剔除(slack 放到窗口大小),
// 并标出来让扫描摘要能报个数
func TestStoreBooks_串过城的盘口放宽slack(t *testing.T) {
	a, b := capKeys()
	since := capT.Add(-6 * time.Hour)
	lr := &fakeLadder{}
	sb := &storeBooks{
		st: lr, slack: 2 * time.Minute, window: 6 * time.Hour, levels: 128,
		conflicts: fakeConflicts{
			b: capT.Add(-time.Hour),
			a: capT.Add(-7 * time.Hour), // 窗口之前的串城不算
		},
	}
	got, err := sb.CaptureBooks(context.Background(), []model.QuoteKey{a, b}, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(lr.calls) != 2 {
		t.Fatalf("应分两次读,得到 %d 次", len(lr.calls))
	}
	if c := lr.calls[0]; len(c.keys) != 1 || c.keys[0] != a || c.slack != 2*time.Minute {
		t.Fatalf("第一次读正常 key、slack=2m,得到 %+v", c)
	}
	if c := lr.calls[1]; len(c.keys) != 1 || c.keys[0] != b || c.slack != 6*time.Hour+2*time.Minute {
		t.Fatalf("第二次读串城 key、slack=窗口 6h + 2m,得到 %+v", c)
	}
	if got[a].Conflicted || !got[b].Conflicted {
		t.Fatalf("只有 b 应标串城,得到 a=%v b=%v", got[a].Conflicted, got[b].Conflicted)
	}
}

type nopDirty struct{}

func (nopDirty) MarkDirty(model.QuoteKey) {}

// 真 Ingestor 接进来:同一张单先报 Martlock(3008)再报 Thetford(0007),
// 两个盘口都该被当成串过城、放宽 slack 去读。钉住的是两边盘口键的口径一致
// (ingest 收敛后的城市名 == 扫描拼 key 用的城市名)
func TestStoreBooks_接真Ingestor的串城记录(t *testing.T) {
	ing, err := ingest.New(nil, nopDirty{}, 100)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Minute)
	for _, loc := range []string{"3008", "0007"} {
		ing.Submit(model.UploadBatch{Reporter: "测试", Orders: []model.MarketOrder{{
			OrderID: 42, ItemID: "T5_CLOTH", LocationID: loc, Quality: 1,
			Side: model.SideOffer, UnitPrice: 100, Amount: 1, ObservedAt: at,
		}}})
	}
	a, b := capKeys()
	other := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1, Side: model.SideOffer}
	lr := &fakeLadder{}
	sb := &storeBooks{st: lr, conflicts: ing, slack: 2 * time.Minute, window: 6 * time.Hour, levels: 128}
	got, err := sb.CaptureBooks(context.Background(), []model.QuoteKey{a, b, other}, time.Now().Add(-6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !got[a].Conflicted || !got[b].Conflicted || got[other].Conflicted {
		t.Fatalf("Martlock、Thetford 应标串城,Lymhurst 不该,得到 %v %v %v",
			got[a].Conflicted, got[b].Conflicted, got[other].Conflicted)
	}
	if len(lr.calls) != 2 || len(lr.calls[1].keys) != 2 || lr.calls[1].slack < 6*time.Hour {
		t.Fatalf("串城的两个 key 应放宽 slack 另读一次,得到 %+v", lr.calls)
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
}
