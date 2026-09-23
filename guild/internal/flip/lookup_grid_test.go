package flip

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

func stamp(t time.Time) aodp.Stamp { return aodp.Stamp{T: t} }

func TestLookupCities(t *testing.T) {
	got := lookupCities([]string{"Thetford", "Black Market", "Martlock", "Thetford"})
	want := []string{"Thetford", "Martlock", "Brecilien", "Black Market"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = lookupCities(append(conf.DefaultCities, "Brecilien"))
	if got[len(got)-1] != BlackMarket || len(got) != len(conf.DefaultCities)+2 {
		t.Fatalf("黑市应在最后一行且只出现一次: %v", got)
	}
}

func TestMergeSidePicksNewerSide(t *testing.T) {
	seen := t0.Add(-10 * time.Minute)
	cb := buildSide([]store.LiveOrder{sellAt(100, 5, seen)}, model.SideOffer, t0, lookupSlack, lookupNearPct, nil)

	cases := []struct {
		name string
		date aodp.Stamp
		pick string
		best int64
	}{
		{"抓包更新", stamp(t0.Add(-2 * time.Hour)), "capture", 100},
		{"AODP 更新", stamp(t0.Add(-1 * time.Minute)), "aodp", 120},
		{"平手用抓包", stamp(seen), "capture", 100},
		{"AODP 没日期", aodp.Stamp{}, "capture", 100},
	}
	for _, c := range cases {
		g := mergeSide(&cb, 120, c.date, 150, c.date, t0)
		if g.Pick != c.pick || g.Source != c.pick || g.Best != c.best {
			t.Errorf("%s: pick=%s best=%d", c.name, g.Pick, g.Best)
		}
		if g.Capture == nil || g.AODP == nil {
			t.Errorf("%s: 被比下去的那一路也要留着", c.name)
		}
	}

	g := mergeSide(nil, 120, stamp(t0.Add(-time.Hour)), 0, aodp.Stamp{}, t0)
	if g.Pick != "aodp" || g.AgeHours == nil || !near(*g.AgeHours, 1) || g.AODP.Far != 0 {
		t.Fatalf("只有 AODP: %+v", g)
	}
	g = mergeSide(nil, 0, aodp.Stamp{}, 0, aodp.Stamp{}, t0)
	if g.Pick != "" || g.Best != 0 || g.AgeHours != nil {
		t.Fatalf("两边都没有: %+v", g)
	}
}

func gridCfg() conf.Config {
	cfg := conf.Default()
	cfg.Cities = []string{"Thetford", "Martlock"}
	return cfg
}

func findCell(g *LookupGrid, city string, q int) *GridCell {
	for i := range g.Cells {
		if g.Cells[i].City == city && g.Cells[i].Quality == q {
			return &g.Cells[i]
		}
	}
	return nil
}

// 列表页只发卖单请求,所以"只抓到卖侧"是常态。整格融合会把 AODP 的买价一起丢掉;
// 逐边融合要保住它。
func TestBuildGridMergesPerSide(t *testing.T) {
	cfg := gridCfg()
	seen := t0.Add(-5 * time.Minute)
	in := gridInput{
		Item:   catalog.Item{ItemID: "T4_LEATHER", MaxQuality: 1},
		Cities: lookupCities(cfg.Cities), Now: t0, Cfg: cfg, Window: 6 * time.Hour,
		Orders: []store.LiveOrder{
			sellAt(310, 15736, seen), sellAt(311, 50, seen),
			ord("3003", 1, model.SideRequest, 999, 1, seen, seen), // 没收敛的地点
		},
		Prices: []aodp.PriceRecord{
			{ItemID: "T4_LEATHER", City: "Martlock", Quality: 1,
				SellPriceMin: 330, SellPriceMinDate: stamp(t0.Add(-3 * time.Hour)),
				BuyPriceMax: 301, BuyPriceMaxDate: stamp(t0.Add(-2 * time.Hour)),
				BuyPriceMin: 1, BuyPriceMinDate: stamp(t0.Add(-2 * time.Hour))},
			// AODP 对没数据的组合也回一行,全是 0
			{ItemID: "T4_LEATHER", City: "Thetford", Quality: 1},
			{ItemID: "T4_LEATHER", City: BlackMarket, Quality: 1,
				BuyPriceMax: 400, BuyPriceMaxDate: stamp(t0.Add(-time.Hour))},
		},
	}
	g := buildGrid(in)

	if !reflect.DeepEqual(g.Qualities, []int{1}) {
		t.Fatalf("qualities=%v", g.Qualities)
	}
	if g.Capture.Orders != 2 || g.Capture.OtherLocations["3003"] != 1 {
		t.Fatalf("capture=%+v", g.Capture)
	}
	if len(g.Cells) != 2 {
		t.Fatalf("只该输出有数据的格子,得到 %d 格", len(g.Cells))
	}
	m := findCell(g, "Martlock", 1)
	if m == nil || m.Sell.Pick != "capture" || m.Sell.Best != 310 {
		t.Fatalf("卖侧应取抓包: %+v", m.Sell)
	}
	if m.Sell.Capture.QtyAtBest != 15736 || m.Sell.Capture.QtyNear != 15786 || m.Sell.AODP.Best != 330 {
		t.Fatalf("卖侧细节: cap=%+v aodp=%+v", m.Sell.Capture, m.Sell.AODP)
	}
	if m.Buy.Pick != "aodp" || m.Buy.Best != 301 || m.Buy.Capture != nil || m.Buy.AODP.Far != 1 {
		t.Fatalf("买侧应保住 AODP: %+v", m.Buy)
	}
	bk := econ.Book{SellMin: 310, BuyMax: 301}
	u := econ.Quote(bk, bk, makerMaker, cfg.Economics)
	if m.Spread == nil || !near(m.Spread.Margin, u.Margin) || m.Spread.Suspect {
		t.Fatalf("spread=%+v", m.Spread)
	}
	bm := findCell(g, BlackMarket, 1)
	if bm == nil || bm.Sell.Pick != "" || bm.Buy.Best != 400 || bm.Spread != nil {
		t.Fatalf("黑市: %+v", bm)
	}
	if findCell(g, "Thetford", 1) != nil {
		t.Fatal("AODP 全 0 的格子不该输出")
	}
	if !g.AODP.PricesOK || g.AODP.Error != "" {
		t.Fatalf("aodp=%+v", g.AODP)
	}
	if !near(g.Params.Breakeven, 1.025/0.935-1) || g.Params.CaptureWindowHours != 6 || g.Params.MinBidDepth != lookupMinBidDepth {
		t.Fatalf("params=%+v", g.Params)
	}
}

func TestBuildGridSuspectAndQualityExtension(t *testing.T) {
	cfg := gridCfg()
	in := gridInput{
		// 目录说只有一档,但 AODP 在 3 档有数据:信数据
		Item:   catalog.Item{ItemID: "T4_2H_FIRESTAFF", MaxQuality: 1},
		Cities: lookupCities(cfg.Cities), Now: t0, Cfg: cfg, Window: 6 * time.Hour,
		Prices: []aodp.PriceRecord{{City: "Thetford", Quality: 3,
			SellPriceMin: 30000, SellPriceMinDate: stamp(t0.Add(-time.Hour)),
			BuyPriceMax: 10000, BuyPriceMaxDate: stamp(t0.Add(-time.Hour))}},
	}
	g := buildGrid(in)
	if !reflect.DeepEqual(g.Qualities, []int{1, 2, 3}) {
		t.Fatalf("qualities=%v", g.Qualities)
	}
	c := findCell(g, "Thetford", 3)
	if c == nil || c.Spread == nil || !c.Spread.Suspect || !near(c.Spread.Raw, 2) {
		t.Fatalf("价差 200%% 应标可疑: %+v", c)
	}
}

func hp(day time.Time, qty, avg int64) aodp.HistoryPoint {
	return aodp.HistoryPoint{ItemCount: qty, AvgPrice: avg, Timestamp: stamp(day)}
}

func TestDailySeriesDropsTodayAndMergesDays(t *testing.T) {
	today := t0.Truncate(24 * time.Hour)
	pts := []aodp.HistoryPoint{
		hp(today, 999, 1),                     // 今天没走完
		hp(today.AddDate(0, 0, -1), 100, 300), // 昨天两条,按件数加权
		hp(today.AddDate(0, 0, -1).Add(6*time.Hour), 300, 340),
		hp(today.AddDate(0, 0, -3), 10, 250),
		hp(today.AddDate(0, 0, -40), 10, 250), // 窗口外
	}
	got := dailySeries(pts, t0, 30)
	want := []SeriesPoint{
		{Day: today.AddDate(0, 0, -3).Format("2006-01-02"), Qty: 10, AvgPrice: 250},
		{Day: today.AddDate(0, 0, -1).Format("2006-01-02"), Qty: 400, AvgPrice: 330},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	raw, _ := json.Marshal(got[0])
	if string(raw) != `["`+want[0].Day+`",10,250]` {
		t.Fatalf("json=%s", raw)
	}
}

func dayRows(city string, q int, source string, days int, qty, avg int64) []store.HistoryRow {
	var out []store.HistoryRow
	today := t0.Truncate(24 * time.Hour)
	for i := 1; i <= days; i++ {
		out = append(out, store.HistoryRow{City: city, Quality: q,
			Day: today.AddDate(0, 0, -i), ItemCount: qty, AvgPrice: avg, Source: source})
	}
	return out
}

func series(city string, q int, days int, qty, avg int64) aodp.HistorySeries {
	s := aodp.HistorySeries{Location: city, ItemID: "T4_LEATHER", Quality: q}
	today := t0.Truncate(24 * time.Hour)
	for i := 1; i <= days; i++ {
		s.Data = append(s.Data, hp(today.AddDate(0, 0, -i), qty, avg))
	}
	return s
}

// 成交历史按 城市 × 品质 整条选一边:现场 AODP 优先,缺了用抓包,
// AODP 请求失败时才退到库里存下的 AODP。
func TestBuildGridHistoryPriority(t *testing.T) {
	cfg := gridCfg()
	base := gridInput{
		Item:   catalog.Item{ItemID: "T4_LEATHER", MaxQuality: 1},
		Cities: lookupCities(cfg.Cities), Now: t0, Cfg: cfg, Window: 6 * time.Hour,
		History: []aodp.HistorySeries{series("Martlock", 1, 10, 1000, 300)},
		DBHistory: append(append(
			dayRows("Martlock", 1, store.SourceCapture, 3, 5, 999),
			dayRows("Thetford", 1, store.SourceCapture, 3, 20, 350)...),
			dayRows("Lymhurst", 1, store.SourceAODP, 5, 40, 360)...),
	}
	base.Cities = []string{"Martlock", "Thetford", "Lymhurst"}

	g := buildGrid(base)
	m := findCell(g, "Martlock", 1)
	if m == nil || m.History == nil || m.History.Source != "aodp" || m.History.Stored || !near(m.History.Avg7d, 300) {
		t.Fatalf("有 AODP 就用 AODP,不和抓包取平均: %+v", m)
	}
	if m.History.Days30d != 10 || len(m.History.Series) != 10 || m.History.DailyQty30d != 1000 {
		t.Fatalf("history=%+v", m.History)
	}
	if m.History.PriceMin != 300 || m.History.PriceMax != 300 || m.History.LastQty != 1000 {
		t.Fatalf("区间/最后一天: %+v", m.History)
	}
	th := findCell(g, "Thetford", 1)
	if th == nil || th.History == nil || th.History.Source != "capture" || !near(th.History.Avg7d, 350) {
		t.Fatalf("AODP 没有这一格就用抓包: %+v", th)
	}
	if th.Sell.Pick != "" || th.Spread != nil {
		t.Fatalf("只有历史的格子也要输出,但没有价: %+v", th)
	}
	if findCell(g, "Lymhurst", 1) != nil {
		t.Fatal("AODP 请求成功时不该用库里的旧 AODP")
	}

	failed := base
	failed.History, failed.HistoryErr = nil, errors.New("HTTP 502")
	g = buildGrid(failed)
	ly := findCell(g, "Lymhurst", 1)
	if ly == nil || ly.History == nil || ly.History.Source != "aodp" || !ly.History.Stored {
		t.Fatalf("AODP 失败时退到库里的 AODP: %+v", ly)
	}
	m = findCell(g, "Martlock", 1)
	if m == nil || m.History.Source != "capture" {
		t.Fatalf("AODP 失败时 Martlock 应退到抓包: %+v", m)
	}
	if g.AODP.HistoryOK || g.AODP.Error == "" {
		t.Fatalf("aodp=%+v", g.AODP)
	}
}
