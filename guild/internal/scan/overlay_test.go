package scan

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/histagg"
	"albion-guild/internal/model"
	"albion-guild/internal/screen"
)

func overlayCfg() conf.Config {
	c := conf.Default()
	c.Cities = []string{"Lymhurst", "Martlock"}
	c.Capture.Enabled = true
	return c
}

func ago(d time.Duration) aodp.Stamp { return aodp.Stamp{T: now.Add(-d)} }

// side 造一个所有档同一时刻看到的盘口边。
func side(seen time.Time, lv ...depth.Level) CapturedSide {
	cs := CapturedSide{Newest: seen, LevelCount: len(lv)}
	for _, l := range lv {
		cs.Levels = append(cs.Levels, l)
		cs.LevelSeen = append(cs.LevelSeen, seen)
		cs.QtyTotal += l.Qty
	}
	return cs
}

func askKey(item, city string) model.QuoteKey {
	return model.QuoteKey{ItemID: item, LocationID: city, Quality: 1, Side: model.SideOffer}
}

func bidKey(item, city string) model.QuoteKey {
	return model.QuoteKey{ItemID: item, LocationID: city, Quality: 1, Side: model.SideRequest}
}

func woodRec(sell int64, sellAt aodp.Stamp, buy int64, buyAt aodp.Stamp) aodp.PriceRecord {
	return aodp.PriceRecord{ItemID: "T5_WOOD", City: "Lymhurst", Quality: 1,
		SellPriceMin: sell, SellPriceMinDate: sellAt, BuyPriceMax: buy, BuyPriceMaxDate: buyAt,
		SellPriceMax: 9999, BuyPriceMin: 7}
}

var woodQK = histagg.QualityKey{ItemID: "T5_WOOD", City: "Lymhurst", Quality: 1}

func TestOverlay_没有抓包时输出和输入完全相同(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(1120, ago(time.Hour), 1000, ago(time.Hour))}
	for _, books := range []map[model.QuoteKey]CapturedSide{nil, {}} {
		out, sides, sum := overlay(in, books, overlayCfg(), now)
		if !reflect.DeepEqual(out, in) || sides != nil || sum != (CaptureSummary{}) {
			t.Fatalf("应原样返回,得到 %+v / %v / %+v", out, sides, sum)
		}
	}
}

func TestOverlay_抓包更新时卖方用抓包买方仍是AODP(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(1120, ago(time.Hour), 1000, ago(time.Hour))}
	books := map[model.QuoteKey]CapturedSide{
		askKey("T5_WOOD", "Lymhurst"): side(now.Add(-10*time.Minute), depth.Level{Price: 1110, Qty: 500}, depth.Level{Price: 1111, Qty: 300}),
	}
	out, sides, sum := overlay(in, books, overlayCfg(), now)

	r := out[0]
	if r.SellPriceMin != 1110 || !r.SellPriceMinDate.T.Equal(now.Add(-10*time.Minute)) {
		t.Fatalf("卖价应取抓包 1110@T−10m,得到 %d@%v", r.SellPriceMin, r.SellPriceMinDate.T)
	}
	if r.BuyPriceMax != 1000 || !r.BuyPriceMaxDate.T.Equal(now.Add(-time.Hour)) {
		t.Fatalf("买价应保持 AODP,得到 %d@%v", r.BuyPriceMax, r.BuyPriceMaxDate.T)
	}
	if r.SellPriceMax != 9999 || r.BuyPriceMin != 7 {
		t.Fatal("SellPriceMax/BuyPriceMin 不该被动")
	}
	if in[0].SellPriceMin != 1120 {
		t.Fatal("输入切片被改了:覆盖率还要用原始 AODP 价")
	}

	s := sides[woodQK]
	if s.Ask.Source != screen.SourceCapture || s.Ask.Price != 1110 || math.Abs(s.Ask.AgeHours-1.0/6) > 1e-9 {
		t.Fatalf("ask 应为抓包 1110 / 0.167h,得到 %+v", s.Ask)
	}
	if s.Ask.Alt == nil || *s.Ask.Alt != (screen.AltQuote{Source: screen.SourceAODP, Price: 1120, AgeHours: 1}) {
		t.Fatalf("落选的 AODP 报价应记成 alt,得到 %+v", s.Ask.Alt)
	}
	if !s.Ask.Captured() || s.Ask.Depth.QtyNear != 800 || s.Ask.Depth.QtyAtBest != 500 || len(s.Ask.Levels) != 2 {
		t.Fatalf("深度应为近价 800 件、卖一 500 件、2 档,得到 %+v / %v", s.Ask.Depth, s.Ask.Levels)
	}
	if s.Bid.Source != screen.SourceAODP || s.Bid.Price != 1000 || s.Bid.Depth != nil || s.Bid.Alt != nil {
		t.Fatalf("bid 应为纯 AODP,得到 %+v", s.Bid)
	}
	if sum.AsksUsed != 1 || sum.BidsUsed != 0 || sum.Superseded != 0 || sum.Synthesized != 0 || sum.BookSides != 1 {
		t.Fatalf("汇总不对: %+v", sum)
	}
}

func TestOverlay_AODP更新而且价不同时AODP胜(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(1200, ago(5*time.Minute), 1000, ago(time.Hour))}
	books := map[model.QuoteKey]CapturedSide{
		askKey("T5_WOOD", "Lymhurst"): side(now.Add(-30*time.Minute), depth.Level{Price: 1190, Qty: 40}),
	}
	out, sides, sum := overlay(in, books, overlayCfg(), now)
	if out[0].SellPriceMin != 1200 || !out[0].SellPriceMinDate.T.Equal(now.Add(-5*time.Minute)) {
		t.Fatalf("应保留 AODP 1200@T−5m,得到 %d@%v", out[0].SellPriceMin, out[0].SellPriceMinDate.T)
	}
	if sum.Superseded != 1 || sum.AsksUsed != 0 {
		t.Fatalf("应记一次 superseded,得到 %+v", sum)
	}
	a := sides[woodQK].Ask
	if a.Source != screen.SourceAODP || a.Depth != nil {
		t.Fatalf("AODP 胜出那边不带深度,得到 %+v", a)
	}
	if a.Alt == nil || a.Alt.Source != screen.SourceCapture || a.Alt.Price != 1190 || math.Abs(a.Alt.AgeHours-0.5) > 1e-9 {
		t.Fatalf("落选的抓包报价应记成 alt,得到 %+v", a.Alt)
	}
}

func TestOverlay_两边同价时用抓包时间戳取较新的(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(1200, ago(5*time.Minute), 1000, ago(time.Hour))}
	books := map[model.QuoteKey]CapturedSide{
		askKey("T5_WOOD", "Lymhurst"): side(now.Add(-30*time.Minute), depth.Level{Price: 1200, Qty: 40}),
	}
	out, sides, sum := overlay(in, books, overlayCfg(), now)
	if out[0].SellPriceMin != 1200 || !out[0].SellPriceMinDate.T.Equal(now.Add(-5*time.Minute)) {
		t.Fatalf("价 1200、时间戳应取 AODP 那个较新的 T−5m,得到 %d@%v", out[0].SellPriceMin, out[0].SellPriceMinDate.T)
	}
	a := sides[woodQK].Ask
	if a.Source != screen.SourceCapture || math.Abs(a.AgeHours-5.0/60) > 1e-9 || sum.Superseded != 0 || sum.AsksUsed != 1 {
		t.Fatalf("应判抓包、数据龄 5 分钟,得到 %+v / %+v", a, sum)
	}
	// 深度看的是抓包自己那一眼(30 分钟前,在 2h 深度窗口内)
	if !a.Captured() || a.Depth.QtyNear != 40 {
		t.Fatalf("深度应照常统计,得到 %+v", a.Depth)
	}
}

func TestOverlay_AODP没有的格子合成一行(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(1120, ago(time.Hour), 1000, ago(time.Hour))}
	seen := now.Add(-10 * time.Minute)
	books := map[model.QuoteKey]CapturedSide{
		askKey("T5_WOOD", "Martlock"): side(seen, depth.Level{Price: 1150, Qty: 9}),
		bidKey("T4_WOOD", "Martlock"): side(seen, depth.Level{Price: 300, Qty: 9}),
		// 不在 cfg.Cities 里:不合成、不计数
		askKey("T5_WOOD", "Brecilien"): side(seen, depth.Level{Price: 1, Qty: 9}),
		// 请求了但窗口内没货
		askKey("T5_WOOD", "Lymhurst"): {},
	}
	out, sides, sum := overlay(in, books, overlayCfg(), now)
	if len(out) != 3 || sum.Synthesized != 2 {
		t.Fatalf("应合成 2 行,得到 %d 行 / %+v", len(out), sum)
	}
	// 合成行按 key 排序,和 map 遍历顺序无关
	if out[1].ItemID != "T4_WOOD" || out[2].ItemID != "T5_WOOD" || out[2].City != "Martlock" {
		t.Fatalf("合成行顺序不对: %+v", out[1:])
	}
	if out[1].BuyPriceMax != 300 || out[1].SellPriceMin != 0 || out[1].Quality != 1 {
		t.Fatalf("T4_WOOD 合成行只有买方: %+v", out[1])
	}
	if out[2].SellPriceMin != 1150 || !out[2].SellPriceMinDate.T.Equal(seen) {
		t.Fatalf("T5_WOOD@Martlock 合成行: %+v", out[2])
	}
	if out[0] != in[0] {
		t.Fatal("Lymhurst 那格没有抓包货,不该被动")
	}
	if _, ok := sides[woodQK]; ok {
		t.Fatal("没货的格子不该有 sides 条目")
	}
	if sum.BookSides != 2 {
		t.Fatalf("有货的边应为 2(Brecilien 不算,空边不算),得到 %d", sum.BookSides)
	}
}

func TestOverlay_抓包时间超前钳到now(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(1120, ago(time.Hour), 1000, ago(time.Hour))}
	books := map[model.QuoteKey]CapturedSide{
		askKey("T5_WOOD", "Lymhurst"): side(now.Add(3*time.Second), depth.Level{Price: 1110, Qty: 5}),
	}
	out, sides, _ := overlay(in, books, overlayCfg(), now)
	if !out[0].SellPriceMinDate.T.Equal(now) {
		t.Fatalf("时间戳应钳到 now,得到 %v", out[0].SellPriceMinDate.T)
	}
	if a := sides[woodQK].Ask; a.AgeHours != 0 || !a.Captured() {
		t.Fatalf("数据龄应为 0 且深度照常,得到 %+v", a)
	}
}

func TestOverlay_最优档超出深度窗口只用价不用深度(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(0, aodp.Stamp{}, 1000, ago(time.Hour))}
	books := map[model.QuoteKey]CapturedSide{
		askKey("T5_WOOD", "Lymhurst"): side(now.Add(-3*time.Hour), depth.Level{Price: 1110, Qty: 5}),
	}
	out, sides, _ := overlay(in, books, overlayCfg(), now)
	if out[0].SellPriceMin != 1110 {
		t.Fatalf("AODP 这边没价,应用抓包价,得到 %d", out[0].SellPriceMin)
	}
	a := sides[woodQK].Ask
	if a.Source != screen.SourceCapture || a.Depth != nil || a.Captured() || a.Levels != nil {
		t.Fatalf("深度快照 3h 前,不该参与判定,得到 %+v", a)
	}
	if !strings.Contains(a.Note, "3.0h") {
		t.Fatalf("Note 应说明深度快照多旧,得到 %q", a.Note)
	}
	if a.Alt != nil {
		t.Fatalf("AODP 这边没价,不该有 alt,得到 %+v", a.Alt)
	}
}

// 深度窗口内外的档混在一起时,只有窗口内的档算近价件数;合计照读簿给的全量
func TestOverlay_深度只统计窗口内的档(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(1120, ago(time.Hour), 1000, ago(time.Hour))}
	cs := side(now.Add(-10*time.Minute),
		depth.Level{Price: 1100, Qty: 5}, depth.Level{Price: 1101, Qty: 7}, depth.Level{Price: 1102, Qty: 900})
	cs.LevelSeen[2] = now.Add(-5 * time.Hour) // 5 小时前看到的那一大档,现在可能早没了
	books := map[model.QuoteKey]CapturedSide{askKey("T5_WOOD", "Lymhurst"): cs}
	_, sides, _ := overlay(in, books, overlayCfg(), now)
	d := sides[woodQK].Ask.Depth
	if d == nil || d.QtyNear != 12 || d.LevelsNear != 2 || d.QtyTotal != 912 {
		t.Fatalf("近价应只算窗口内 12 件 / 2 档,合计 912,得到 %+v", d)
	}
	if len(sides[woodQK].Ask.Levels) != 2 {
		t.Fatalf("Levels 只带窗口内的档,得到 %v", sides[woodQK].Ask.Levels)
	}
}

func TestOverlay_买方镜像与汇总计数(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(1120, ago(time.Hour), 1000, ago(time.Hour))}
	bid := side(now.Add(-time.Minute), depth.Level{Price: 1005, Qty: 10}, depth.Level{Price: 1000, Qty: 20})
	bid.Ghosts, bid.Conflicted = 3, true
	ask := side(now.Add(-time.Minute), depth.Level{Price: 1115, Qty: 1})
	ask.Ghosts = 2
	books := map[model.QuoteKey]CapturedSide{
		bidKey("T5_WOOD", "Lymhurst"): bid,
		askKey("T5_WOOD", "Lymhurst"): ask,
	}
	out, sides, sum := overlay(in, books, overlayCfg(), now)
	if out[0].BuyPriceMax != 1005 || out[0].SellPriceMin != 1115 {
		t.Fatalf("两边都应取抓包,得到 买 %d 卖 %d", out[0].BuyPriceMax, out[0].SellPriceMin)
	}
	if b := sides[woodQK].Bid; b.Depth == nil || b.Depth.QtyNear != 30 || b.Depth.Best != 1005 {
		t.Fatalf("买方近价应为 30 件,得到 %+v", b.Depth)
	}
	if sum.AsksUsed != 1 || sum.BidsUsed != 1 || sum.Ghosts != 5 || sum.ConflictKeys != 1 {
		t.Fatalf("汇总不对: %+v", sum)
	}
}

func TestCaptureKeys(t *testing.T) {
	keys := captureKeys([]string{"A", "B", "C"}, []string{"Lymhurst", "Martlock"}, []int{1, 2})
	if len(keys) != 3*2*2*2 {
		t.Fatalf("应为 物品×城市×品质×2 = 24,得到 %d", len(keys))
	}
	seen := map[model.QuoteKey]bool{}
	for _, k := range keys {
		if k.LocationID != "Lymhurst" && k.LocationID != "Martlock" {
			t.Fatalf("混进了配置外的地点 %q", k.LocationID)
		}
		if seen[k] {
			t.Fatalf("重复的 key %v", k)
		}
		seen[k] = true
	}
}

func TestMergeSide_截断判定(t *testing.T) {
	cfg := overlayCfg()
	seen := now.Add(-time.Minute)
	near := side(seen, depth.Level{Price: 100, Qty: 1}, depth.Level{Price: 101, Qty: 1}, depth.Level{Price: 102, Qty: 1})

	cut := near
	cut.LevelCount = 50 // 读簿截在 3 档,实际有 50 档,读到的 3 档全在近价窗口内
	if m := MergeSide(0, aodp.Stamp{}, cut, cfg, now); !m.Side.Depth.Truncated {
		t.Fatal("截断且读到的档全在近价内,应标 truncated")
	}
	if m := MergeSide(0, aodp.Stamp{}, near, cfg, now); m.Side.Depth.Truncated {
		t.Fatal("没截断,不该标 truncated")
	}
	far := side(seen, depth.Level{Price: 100, Qty: 1}, depth.Level{Price: 101, Qty: 1}, depth.Level{Price: 200, Qty: 1})
	far.LevelCount = 50
	if m := MergeSide(0, aodp.Stamp{}, far, cfg, now); m.Side.Depth.Truncated {
		t.Fatal("读到的档已经走出近价窗口,近价件数是全的,不该标 truncated")
	}
}

func TestPickCapture_五条规则(t *testing.T) {
	slack := 10 * time.Minute
	at := func(d time.Duration) CapturedSide {
		return side(now.Add(-d), depth.Level{Price: 1190, Qty: 1})
	}
	for _, tc := range []struct {
		name          string
		px            int64
		stamp         aodp.Stamp
		cs            CapturedSide
		use, replaced bool
	}{
		{"抓包没数据", 1200, ago(time.Minute), CapturedSide{}, false, false},
		{"只有零件档也算没数据", 1200, ago(time.Minute), side(now, depth.Level{Price: 1190, Qty: 0}), false, false},
		{"AODP没价", 0, ago(time.Minute), at(time.Hour), true, false},
		{"AODP没时间戳", 1200, aodp.Stamp{}, at(time.Hour), true, false},
		{"抓包更新", 1200, ago(time.Hour), at(time.Minute), true, false},
		{"抓包旧但在宽容度内", 1200, ago(5 * time.Minute), at(14 * time.Minute), true, false},
		{"抓包旧过宽容度但同价", 1190, ago(5 * time.Minute), at(time.Hour), true, false},
		{"抓包旧过宽容度且价不同", 1200, ago(5 * time.Minute), at(time.Hour), false, true},
	} {
		use, replaced := PickCapture(tc.px, tc.stamp, tc.cs, slack)
		if use != tc.use || replaced != tc.replaced {
			t.Fatalf("%s:得到 use=%v superseded=%v,想要 %v/%v", tc.name, use, replaced, tc.use, tc.replaced)
		}
	}
}

func TestFreshness_超前时间戳不可用(t *testing.T) {
	rec := woodRec(1120, aodp.Stamp{T: now.Add(time.Hour)}, 1000, ago(time.Hour))
	if _, ok := freshness(rec, now); ok {
		t.Fatal("卖方时间戳超前 1h,跨城不该当成刚更新")
	}
	rec = woodRec(1120, ago(time.Hour), 0, aodp.Stamp{T: now.Add(time.Minute)})
	if _, ok := freshness(rec, now); ok {
		t.Fatal("没价那一侧的时间戳超前也不该放行")
	}
	if age, ok := freshness(woodRec(1120, ago(2*time.Hour), 1000, ago(time.Hour)), now); !ok || age != 2 {
		t.Fatalf("正常情况取较旧那侧,得到 %v/%v", age, ok)
	}
}

func TestCoverage_超前时间戳不算新鲜(t *testing.T) {
	in := []aodp.PriceRecord{woodRec(1120, aodp.Stamp{T: now.Add(time.Hour)}, 1000, ago(time.Hour))}
	cov := coverageByCity(in, []string{"Lymhurst"}, now, 6)
	c := cov[0]
	if c.WithData != 2 || c.Within2h != 1 || c.WithinThreshold != 1 || c.MedianAgeHours != 1 {
		t.Fatalf("超前的那个点算有数据、不算新鲜、不进中位数,得到 %+v", c)
	}
}
