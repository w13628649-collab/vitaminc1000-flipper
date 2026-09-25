package scan

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/depth"
	"albion-guild/internal/histagg"
	"albion-guild/internal/model"
	"albion-guild/internal/screen"
)

// digestSnap 是两城一物品的快照:同城各有机会,Lymhurst → Martlock 有跨城路线。
func digestSnap(items ...string) *Snapshot {
	s := &Snapshot{FetchedAt: now, ItemIDs: items, Stats: map[histagg.QualityKey]histagg.Stats{}}
	for _, item := range items {
		for _, c := range []struct {
			city      string
			sell, buy int64
			avg       float64
		}{{"Lymhurst", 1120, 1000, 1050}, {"Martlock", 1450, 1300, 1350}} {
			s.Prices = append(s.Prices, aodp.PriceRecord{ItemID: item, City: c.city, Quality: 1,
				SellPriceMin: c.sell, SellPriceMinDate: ago(time.Hour), BuyPriceMax: c.buy, BuyPriceMaxDate: ago(time.Hour)})
			s.Stats[histagg.QualityKey{ItemID: item, City: c.city, Quality: 1}] = histagg.Stats{
				AvgPrice7d: c.avg, AvgPrice30d: c.avg, DailyVolumeQty: 50_000, DailyVolumeSilver: 50_000 * c.avg,
				DaysWithData7d: 7, LastPoint: now.Add(-12 * time.Hour)}
		}
	}
	return s
}

func digestBooks(lymAsk int64) *fakeBooks {
	return &fakeBooks{data: map[model.QuoteKey]CapturedSide{
		askKey("T5_WOOD", "Lymhurst"): side(now.Add(-10*time.Minute),
			depth.Level{Price: lymAsk, Qty: 500}, depth.Level{Price: 1111, Qty: 300}),
		// 3 小时前翻到的买单,和 AODP 同价:价用抓包,深度超出可信窗口,带一条 Note
		bidKey("T5_WOOD", "Martlock"): side(now.Add(-3*time.Hour), depth.Level{Price: 1300, Qty: 50}),
	}}
}

var digestCities = []string{"Lymhurst", "Martlock"}

// 同一份快照、同一份抓包,隔 10 分钟再评估:所有数据龄都变大了,但内容没变,
// 摘要必须不变——否则界面每分钟都会白白重拉一遍整张机会板
func TestDigest_只有时间流逝时不变(t *testing.T) {
	cfg := captureCfg()
	cfg.Cities = digestCities
	cat := catalog.New([]catalog.Item{{ItemID: "T5_WOOD", NameZH: "杉木"}}, nil, "")
	snap := digestSnap("T5_WOOD")

	a := EvaluateSnapshot(context.Background(), snap, cfg, cat, digestBooks(1110), now)
	b := EvaluateSnapshot(context.Background(), snap, cfg, cat, digestBooks(1110), now.Add(10*time.Minute))

	// 先确认这个用例有牙:机会、路线、抓包 Note 都在,数据龄确实变了
	if len(a.Opportunities) == 0 || len(a.Routes) == 0 {
		t.Fatalf("前提:应有同城机会和跨城路线,得到 %d / %d(拒绝 %v)", len(a.Opportunities), len(a.Routes), a.RejectCounts)
	}
	var noted bool
	for _, o := range a.Opportunities {
		if o.City == "Martlock" && o.Bid.Note != "" && strings.Contains(strings.Join(o.Hints, ";"), o.Bid.Note) {
			noted = true
		}
	}
	if !noted {
		t.Fatalf("前提:Martlock 买方应带深度超窗的 Note 并进 hints,得到 %+v", a.Opportunities)
	}
	if !(b.Opportunities[0].DataAgeHours > a.Opportunities[0].DataAgeHours) || !(b.Routes[0].MaxAgeHours > a.Routes[0].MaxAgeHours) {
		t.Fatal("前提:10 分钟后数据龄应变大")
	}

	da, db := Digest(a), Digest(b)
	if len(da) != 32 || da != db {
		t.Fatalf("只有时间流逝,摘要应不变(32 位十六进制),得到 %q / %q", da, db)
	}
	if Digest(a) != da {
		t.Fatal("同一份结果算两次摘要应相同")
	}

	// 抓包卖价变了一档:内容变了,摘要要变
	c := EvaluateSnapshot(context.Background(), snap, cfg, cat, digestBooks(1105), now.Add(10*time.Minute))
	if Digest(c) == da {
		t.Fatal("卖价从 1110 变成 1105,摘要应改变")
	}

	// 拒绝统计也算内容
	d := *b
	d.RejectCounts = map[string]int{"stale": 1}
	if Digest(&d) == db {
		t.Fatal("拒绝统计变了,摘要应改变")
	}
	// nil 和空是同一个内容
	e1, e2 := &Result{}, &Result{RejectCounts: map[string]int{}, Rejected: []screen.Rejected{}, ItemIDs: []string{}}
	if Digest(e1) != Digest(e2) || Digest(nil) != "" {
		t.Fatal("空结果的 nil 和空切片应得到同一个摘要")
	}
	// 摘要字段本身不算内容:发布时先算摘要再填进去,填完再算一次得是同一个值
	f := *b
	f.Digest = db
	if Digest(&f) != db {
		t.Fatal("结果里已经填了 digest,不该影响摘要")
	}
}

// 除了机会和路线,界面还从 /api/scan 画这些:覆盖率的新鲜度分桶、被拒明细的 detail。
// 它们里面有几样只是随 now 往下掉(AODP 时间戳两次全量之间不变),不能让摘要每分钟都变
func TestDigest_随时间走的分桶和拒绝文案不进摘要(t *testing.T) {
	cfg := captureCfg()
	cfg.Cities = digestCities
	cat := catalog.New([]catalog.Item{{ItemID: "T5_WOOD"}, {ItemID: "T4_WOOD"}, {ItemID: "T6_WOOD"}, {ItemID: "T7_WOOD"}}, nil, "")
	snap := digestSnap("T5_WOOD")
	snap.ItemIDs = append(snap.ItemIDs, "T4_WOOD", "T6_WOOD", "T7_WOOD")
	// T4_WOOD:AODP 8 小时前的价 → stale,detail 是 "8.0h > 6h",10 分钟后是 "8.2h > 6h"
	snap.Prices = append(snap.Prices, aodp.PriceRecord{ItemID: "T4_WOOD", City: "Lymhurst", Quality: 1,
		SellPriceMin: 1120, SellPriceMinDate: ago(8 * time.Hour), BuyPriceMax: 1000, BuyPriceMaxDate: ago(8 * time.Hour)})
	// T6_WOOD:成交历史停在 5 天多以前 → stale_history,"5.0d" 10 分钟后变 "5.1d"
	snap.Prices = append(snap.Prices, aodp.PriceRecord{ItemID: "T6_WOOD", City: "Lymhurst", Quality: 1,
		SellPriceMin: 1120, SellPriceMinDate: ago(time.Hour), BuyPriceMax: 1000, BuyPriceMaxDate: ago(time.Hour)})
	snap.Stats[histagg.QualityKey{ItemID: "T6_WOOD", City: "Lymhurst", Quality: 1}] = histagg.Stats{
		AvgPrice7d: 1050, AvgPrice30d: 1050, DailyVolumeQty: 50_000, DailyVolumeSilver: 50_000 * 1050,
		DaysWithData7d: 7, LastPoint: now.Add(-(5*24*time.Hour + 66*time.Minute))}
	// T7_WOOD:只有卖单、1h55m 前的 → one_sided,但它会从 within_2h 滑出去
	snap.Prices = append(snap.Prices, aodp.PriceRecord{ItemID: "T7_WOOD", City: "Lymhurst", Quality: 1,
		SellPriceMin: 1120, SellPriceMinDate: ago(115 * time.Minute)})

	a := EvaluateSnapshot(context.Background(), snap, cfg, cat, digestBooks(1110), now)
	b := EvaluateSnapshot(context.Background(), snap, cfg, cat, digestBooks(1110), now.Add(10*time.Minute))

	detail := func(res *Result, item string) string {
		for _, r := range res.Rejected {
			if r.ItemID == item {
				return r.Reason + ":" + r.Detail
			}
		}
		return ""
	}
	lym := func(res *Result) CityCoverage {
		for _, c := range res.Coverage {
			if c.City == "Lymhurst" {
				return c
			}
		}
		return CityCoverage{}
	}
	// 先确认这个用例有牙:文案和分桶确实随时间变了
	if da, db := detail(a, "T4_WOOD"), detail(b, "T4_WOOD"); !strings.HasPrefix(da, "stale:") || da == db {
		t.Fatalf("前提:T4_WOOD 应是 stale 且 detail 随时间变,得到 %q / %q", da, db)
	}
	if da, db := detail(a, "T6_WOOD"), detail(b, "T6_WOOD"); !strings.HasPrefix(da, "stale_history:") || da == db {
		t.Fatalf("前提:T6_WOOD 应是 stale_history 且 detail 随时间变,得到 %q / %q", da, db)
	}
	if ca, cb := lym(a), lym(b); ca.Within2h == cb.Within2h || ca.WithData != cb.WithData {
		t.Fatalf("前提:Lymhurst 的 within_2h 应随时间变、with_data 不变,得到 %+v / %+v", ca, cb)
	}
	if len(a.Opportunities) == 0 || len(a.Routes) == 0 {
		t.Fatalf("前提:应有机会和路线,得到 %d / %d", len(a.Opportunities), len(a.Routes))
	}

	if Digest(a) != Digest(b) {
		t.Fatal("只有时间流逝:分桶和 stale 文案跟着变,摘要应不变")
	}
}

// 用户要的是"所有东西都能在各个页面显示出来,而且可以实时更新"。界面按协议只在摘要
// 变了时重拉 /api/scan,所以界面画的任何一块单独变了,摘要都必须变
func TestDigest_界面渲染的每一块单独变了都要变(t *testing.T) {
	cfg := captureCfg()
	cfg.Cities = []string{"Lymhurst", "Martlock", "Brecilien"}
	cat := catalog.New([]catalog.Item{{ItemID: "T5_WOOD", NameZH: "杉木"}}, nil, "")
	snap := digestSnap("T5_WOOD")
	// Brecilien:AODP 只有卖单(one_sided),没有历史
	snap.Prices = append(snap.Prices, aodp.PriceRecord{ItemID: "T5_WOOD", City: "Brecilien", Quality: 1,
		SellPriceMin: 1500, SellPriceMinDate: ago(3 * time.Hour)})
	base := EvaluateSnapshot(context.Background(), snap, cfg, cat, digestBooks(1110), now)
	d0 := Digest(base)
	if len(base.Rejected) == 0 || base.Rejected[0].City != "Brecilien" || len(base.Coverage) != 3 {
		t.Fatalf("前提:Brecilien 那格被拒、三城都有覆盖率行,得到 %+v / %d", base.Rejected, len(base.Coverage))
	}

	// 审查里抓到的那一例:成员在 Brecilien 列表页翻到了卖单(只抓得到卖方),格子仍是
	// one_sided,机会、路线、拒绝计数都没变;变的是抓包汇总、覆盖率抓包列、被拒明细的来源
	books := digestBooks(1110)
	books.data[askKey("T5_WOOD", "Brecilien")] = side(now.Add(-time.Minute), depth.Level{Price: 1480, Qty: 40})
	got := EvaluateSnapshot(context.Background(), snap, cfg, cat, books, now)
	if got.Capture.AsksUsed == base.Capture.AsksUsed || len(got.Opportunities) != len(base.Opportunities) ||
		len(got.Routes) != len(base.Routes) || got.RejectCounts["one_sided"] != base.RejectCounts["one_sided"] {
		t.Fatalf("前提:只有抓包汇总这一侧变了,得到 asks_used %d→%d", base.Capture.AsksUsed, got.Capture.AsksUsed)
	}
	if Digest(got) == d0 {
		t.Fatal("Brecilien 抓到了卖单:抓包汇总、覆盖率、被拒明细来源都变了,摘要应改变")
	}

	cases := map[string]func(r *Result){
		"capture.error 横幅":      func(r *Result) { r.Capture.AddError(errors.New("列抓包物品: boom")) },
		"capture.extra_pending": func(r *Result) { r.Capture.ExtraPending = 3 },
		"capture.backfilled_at": func(r *Result) { r.Capture.BackfilledAt = now },
		"started_at(新的全量)":      func(r *Result) { r.StartedAt = r.StartedAt.Add(30 * time.Minute) },
		"request_count":         func(r *Result) { r.RequestCount++ },
		"price_rows":            func(r *Result) { r.PriceRows++ },
		"item_ids":              func(r *Result) { r.ItemIDs = append(append([]string(nil), r.ItemIDs...), "T6_BAG") },
		"extra_item_ids":        func(r *Result) { r.ExtraItemIDs = []string{"T6_BAG"} },
		"missing_item_ids":      func(r *Result) { r.MissingItemIDs = []string{"T9_NOPE"} },
		"coverage.capture_used": func(r *Result) {
			r.Coverage = append([]CityCoverage(nil), r.Coverage...)
			r.Coverage[0].CaptureUsed++
		},
		"coverage.with_data": func(r *Result) {
			r.Coverage = append([]CityCoverage(nil), r.Coverage...)
			r.Coverage[0].WithData++
		},
		"rejected[].ask_source": func(r *Result) {
			r.Rejected = append([]screen.Rejected(nil), r.Rejected...)
			r.Rejected[0].AskSource = "capture"
		},
		"rejected[].detail(非数据龄文案)": func(r *Result) {
			r.Rejected = append([]screen.Rejected(nil), r.Rejected...)
			r.Rejected[0].Reason, r.Rejected[0].Detail = "one_sided", "只有买单"
		},
	}
	for name, mutate := range cases {
		r := *base
		mutate(&r)
		if Digest(&r) == d0 {
			t.Errorf("%s 变了,摘要应改变", name)
		}
	}
	// 反过来:evaluated_at 不算内容
	r := *base
	r.EvaluatedAt = r.EvaluatedAt.Add(time.Minute)
	if Digest(&r) != d0 {
		t.Fatal("只有 evaluated_at 变了,摘要应不变")
	}
}

// 日收益并列的两条路线(两个物品一模一样)每次都要按同一个顺序出来。
// 以前遍历 map,重算一次就可能换位置:界面的行乱跳,摘要也跟着变
func TestFindRoutes_并列时顺序固定(t *testing.T) {
	cfg := captureCfg()
	cfg.Cities = digestCities
	cat := catalog.New([]catalog.Item{{ItemID: "T5_WOOD"}, {ItemID: "T4_WOOD"}, {ItemID: "T6_WOOD"}}, nil, "")
	snap := digestSnap("T6_WOOD", "T4_WOOD", "T5_WOOD")
	first := ""
	for i := 0; i < 30; i++ {
		res := EvaluateSnapshot(context.Background(), snap, cfg, cat, nil, now)
		var order []string
		for _, r := range res.Routes {
			order = append(order, r.ItemID+":"+r.FromCity+">"+r.ToCity)
		}
		got := strings.Join(order, ",")
		if i == 0 {
			first = got
			if !strings.HasPrefix(got, "T4_WOOD:Lymhurst>Martlock,T5_WOOD:Lymhurst>Martlock,T6_WOOD:Lymhurst>Martlock") {
				t.Fatalf("并列时应按物品 id 排,得到 %s", got)
			}
			continue
		}
		if got != first {
			t.Fatalf("第 %d 次路线顺序变了:\n%s\n%s", i, first, got)
		}
	}
}
