package scan

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/histagg"
	"albion-guild/internal/screen"
)

// 两次全量之间:同一份 AODP 快照配上新抓到的盘口重算,不再打 AODP;
// started_at 仍是快照那一刻,evaluated_at 是这次重算
func TestEvaluateSnapshot_重算不打AODP且用上新抓包(t *testing.T) {
	cfg := captureCfg()
	f := newFakeAODP(t)
	client := aodp.New(f.srv.URL, cfg.API)
	books := &fakeBooks{}
	snap, history, err := FetchSnapshot(context.Background(), client, cfg, testCatalog(), books, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) == 0 || snap.RequestCount != 2 || !snap.FetchedAt.Equal(now) {
		t.Fatalf("快照应带历史、2 次请求、FetchedAt=now,得到 %d / %d / %v", len(history), snap.RequestCount, snap.FetchedAt)
	}

	first := EvaluateSnapshot(context.Background(), snap, cfg, testCatalog(), books, now)
	if o := findOpp(first, "T5_WOOD"); o == nil || o.SellPrice != 1120 || o.Ask.Source != screen.SourceAODP {
		t.Fatalf("还没抓包时应是 AODP 1120,得到 %+v", o)
	}

	// 5 分钟后成员翻到了 T5_WOOD 的卖单
	books.data = woodBooks().data
	later := now.Add(5 * time.Minute)
	second := EvaluateSnapshot(context.Background(), snap, cfg, testCatalog(), books, later)
	o := findOpp(second, "T5_WOOD")
	if o == nil || o.SellPrice != 1110 || o.Ask.Source != screen.SourceCapture {
		t.Fatalf("重算应用上新抓到的卖价 1110,得到 %+v", o)
	}
	if len(f.requested()) != 2 || second.RequestCount != 2 {
		t.Fatalf("重算不该打 AODP,请求 %d 次", len(f.requested()))
	}
	if !second.StartedAt.Equal(now) || !second.EvaluatedAt.Equal(later) {
		t.Fatalf("started_at 应为快照时刻、evaluated_at 为重算时刻,得到 %v / %v", second.StartedAt, second.EvaluatedAt)
	}
	// AODP 那一侧的数据龄随重算时刻变老,不能停在快照那一刻
	if o.BuyAgeHours <= 1.08 || o.BuyAgeHours >= 1.09 {
		t.Fatalf("AODP 买方数据龄应为 1h05m,得到 %v", o.BuyAgeHours)
	}
	if findOpp(first, "T5_WOOD").SellPrice != 1120 {
		t.Fatal("旧结果不该被后一次评估改掉")
	}
}

func snapFixture() *Snapshot {
	return &Snapshot{
		FetchedAt:    now,
		ItemIDs:      []string{"A"},
		Qualities:    QualitySet{"A": {1}},
		Prices:       []aodp.PriceRecord{{ItemID: "A", City: "Lymhurst", Quality: 1, SellPriceMin: 10}},
		Stats:        map[histagg.QualityKey]histagg.Stats{{ItemID: "A", City: "Lymhurst", Quality: 1}: {AvgPrice7d: 10}},
		RequestCount: 2,
	}
}

func TestSnapshotWith_补拉并入且不动原快照(t *testing.T) {
	s := snapFixture()
	at := now.Add(3 * time.Minute)
	ext := &Extension{
		FetchedAt: at,
		ItemIDs:   []string{"A", "B", "B"},
		Qualities: QualitySet{"A": {1}, "B": {1}},
		Prices: []aodp.PriceRecord{
			{ItemID: "A", City: "Lymhurst", Quality: 1, SellPriceMin: 99}, // 快照里已有:不能盖掉、不能重复
			{ItemID: "B", City: "Lymhurst", Quality: 1, SellPriceMin: 20},
		},
		Stats: map[histagg.QualityKey]histagg.Stats{
			{ItemID: "A", City: "Lymhurst", Quality: 1}: {AvgPrice7d: 99},
			{ItemID: "B", City: "Lymhurst", Quality: 1}: {AvgPrice7d: 20},
		},
		RequestCount: 2,
	}
	n := s.With(ext)
	if strings.Join(n.ItemIDs, ",") != "A,B" || strings.Join(n.ExtraItemIDs, ",") != "B" ||
		!slices.Equal(n.Qualities["B"], []int{1}) {
		t.Fatalf("应并进 B 一次,得到 %v / %v / %v", n.ItemIDs, n.ExtraItemIDs, n.Qualities)
	}
	if len(n.Prices) != 2 || n.Prices[0].SellPriceMin != 10 || n.Prices[1].ItemID != "B" {
		t.Fatalf("价格表应为原 A + 新 B,得到 %+v", n.Prices)
	}
	if n.Stats[histagg.QualityKey{ItemID: "A", City: "Lymhurst", Quality: 1}].AvgPrice7d != 10 ||
		n.Stats[histagg.QualityKey{ItemID: "B", City: "Lymhurst", Quality: 1}].AvgPrice7d != 20 {
		t.Fatalf("统计应保留原 A、加上 B,得到 %+v", n.Stats)
	}
	if n.RequestCount != 4 || !n.BackfilledAt.Equal(at) || !n.FetchedAt.Equal(now) {
		t.Fatalf("请求数累加、记补拉时刻、全量时刻不变,得到 %d / %v / %v", n.RequestCount, n.BackfilledAt, n.FetchedAt)
	}
	if len(s.ItemIDs) != 1 || len(s.Prices) != 1 || len(s.Stats) != 1 || s.RequestCount != 2 ||
		!s.BackfilledAt.IsZero() || len(s.Qualities) != 1 {
		t.Fatal("原快照被改了:正在用它评估的那一轮会读到半新半旧的数据")
	}
	if s.With(nil) != s {
		t.Fatal("没有补拉数据时原样返回")
	}
	if s.With(&Extension{ItemIDs: []string{"A"}, Qualities: QualitySet{"A": {1}}}) != s {
		t.Fatal("补拉的组合快照里全都有时原样返回")
	}
}

// 已有物品抓到新品质:只添那一档的价和统计,物品不重复进 ItemIDs,清单物品也不算进 ExtraItemIDs
func TestSnapshotWith_已有物品补新品质(t *testing.T) {
	s := snapFixture()
	ext := &Extension{
		FetchedAt: now.Add(time.Minute),
		ItemIDs:   []string{"A"},
		Qualities: QualitySet{"A": {1, 4}},
		Prices: []aodp.PriceRecord{
			{ItemID: "A", City: "Lymhurst", Quality: 1, SellPriceMin: 99}, // 已有的 q1:不动
			{ItemID: "A", City: "Lymhurst", Quality: 4, SellPriceMin: 400},
		},
		Stats: map[histagg.QualityKey]histagg.Stats{
			{ItemID: "A", City: "Lymhurst", Quality: 1}: {AvgPrice7d: 99},
			{ItemID: "A", City: "Lymhurst", Quality: 4}: {AvgPrice7d: 400},
		},
	}
	n := s.With(ext)
	if strings.Join(n.ItemIDs, ",") != "A" || len(n.ExtraItemIDs) != 0 || !slices.Equal(n.Qualities["A"], []int{1, 4}) {
		t.Fatalf("A 只添 q4,得到 %v / %v / %v", n.ItemIDs, n.ExtraItemIDs, n.Qualities)
	}
	if len(n.Prices) != 2 || n.Prices[0].SellPriceMin != 10 || n.Prices[1].Quality != 4 ||
		n.Stats[histagg.QualityKey{ItemID: "A", City: "Lymhurst", Quality: 1}].AvgPrice7d != 10 ||
		n.Stats[histagg.QualityKey{ItemID: "A", City: "Lymhurst", Quality: 4}].AvgPrice7d != 400 {
		t.Fatalf("q1 保留原值、q4 并进来,得到 %+v / %+v", n.Prices, n.Stats)
	}
	if !slices.Equal(s.Qualities["A"], []int{1}) {
		t.Fatalf("原快照的品质表被改了,得到 %v", s.Qualities["A"])
	}
}

func TestPendingExtras_按剩余名额(t *testing.T) {
	cfg := captureCfg()
	cfg.Capture.MaxExtraItems = 2
	s := &Snapshot{ItemIDs: []string{"T5_CLOTH", "T5_WOOD"}, ExtraItemIDs: []string{"T5_WOOD"},
		Qualities: QualitySet{"T5_CLOTH": {1}, "T5_WOOD": {1}}}
	captured := []CapturedItem{{"T5_WOOD", 1, 900}, {"T9_UNKNOWN", 1, 800}, {"T5_ORE", 1, 5}}
	if got := s.PendingExtras(captured, cfg, testCatalog()); strings.Join(got.Items, ",") != "T5_ORE" || got.Count() != 1 {
		t.Fatalf("快照里没有、目录里有的只剩 T5_ORE,得到 %+v", got)
	}
	cfg.Capture.MaxExtraItems = 1
	if got := s.PendingExtras(captured, cfg, testCatalog()); got.Count() != 0 {
		t.Fatalf("名额已用完,得到 %+v", got)
	}
	// 名额用完了,已有物品抓到的新品质照样要补:不占名额
	withQ := append(captured, CapturedItem{"T5_WOOD", 3, 40}, CapturedItem{"T5_CLOTH", 2, 1})
	got := s.PendingExtras(withQ, cfg, testCatalog())
	if strings.Join(got.Items, ",") != "T5_WOOD,T5_CLOTH" || !slices.Equal(got.Qualities["T5_WOOD"], []int{3}) ||
		!slices.Equal(got.Qualities["T5_CLOTH"], []int{2}) || got.Count() != 2 {
		t.Fatalf("应补 T5_WOOD@3、T5_CLOTH@2(按件数排),得到 %+v", got)
	}
	cfg.Capture.MaxExtraItems, cfg.Capture.Enabled = 2, false
	if got := s.PendingExtras(withQ, cfg, testCatalog()); got.Count() != 0 {
		t.Fatalf("开关关着不补,得到 %+v", got)
	}
}

func TestListCaptured_开关和上限控制查不查库(t *testing.T) {
	cfg := captureCfg()
	books := &fakeBooks{items: []CapturedItem{{"T5_WOOD", 1, 1}}}
	if got, err := ListCaptured(context.Background(), cfg, books, now); err != nil || len(got) != 1 ||
		!books.itemSince.Equal(now.Add(-6*time.Hour)) {
		t.Fatalf("应按抓包窗口查一次,得到 %v / %v / %v", got, err, books.itemSince)
	}
	off := cfg
	off.Capture.Enabled = false
	zero := cfg
	zero.Capture.MaxExtraItems = 0
	books.itemCalls = 0
	if got, _ := ListCaptured(context.Background(), off, books, now); got != nil || books.itemCalls != 0 {
		t.Fatal("开关关着不该查库")
	}
	if got, _ := ListCaptured(context.Background(), zero, books, now); got != nil || books.itemCalls != 0 {
		t.Fatal("上限为 0 不该查库")
	}
	if got, _ := ListCaptured(context.Background(), cfg, nil, now); got != nil {
		t.Fatal("没有来源时返回 nil")
	}
}

func TestFetchExtension_只算本次请求(t *testing.T) {
	cfg := captureCfg()
	f := newFakeAODP(t)
	client := aodp.New(f.srv.URL, cfg.API)
	if _, _, err := FetchSnapshot(context.Background(), client, cfg, testCatalog(), nil, now); err != nil {
		t.Fatal(err)
	}
	ext, history, err := FetchExtension(context.Background(), client, cfg,
		Pairs{Items: []string{"T5_WOOD"}, Qualities: QualitySet{"T5_WOOD": {1}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if ext.RequestCount != 2 || len(history) == 0 || !ext.FetchedAt.Equal(now) {
		t.Fatalf("补拉应只算自己的 2 次请求并带历史,得到 %d / %d", ext.RequestCount, len(history))
	}
	paths := f.requested()
	for _, p := range paths[2:] {
		if !strings.Contains(p, "T5_WOOD") || strings.Contains(p, "T5_CLOTH") {
			t.Fatalf("补拉只该请求新物品: %s", p)
		}
	}
}
