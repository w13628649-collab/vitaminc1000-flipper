package scan

import (
	"context"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/depth"
	"albion-guild/internal/histagg"
	"albion-guild/internal/model"
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
	e1, e2 := &Result{}, &Result{RejectCounts: map[string]int{}}
	if Digest(e1) != Digest(e2) || Digest(nil) != "" {
		t.Fatal("空结果的 nil 和空切片应得到同一个摘要")
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
