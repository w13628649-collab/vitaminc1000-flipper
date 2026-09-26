package flip

import (
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/histagg"
	"albion-guild/internal/screen"
)

// 查价页"进游戏照抄的两个数 / 单件净赚"和机会页同城走同一个函数(econ.QuoteBanded):
// 同一个盘口、同一份成交历史,两边的挂价、毛利、可疑判定必须是同一个结论。
// 以前查价页按盘口价压一档,机会页封过之后两边对不上
func TestGridSpread_挂单带宽和机会页同城一致(t *testing.T) {
	cfg := gridCfg()
	at := stamp(t0.Add(-time.Hour))
	hist := func(city string, avg int64) aodp.HistorySeries {
		s := series(city, 1, 10, 500, avg)
		s.ItemID = "T4_CLOTH_LEVEL3@3"
		return s
	}
	in := gridInput{
		Item:   catalog.Item{ItemID: "T4_CLOTH_LEVEL3@3", MaxQuality: 1},
		Cities: lookupCities(cfg.Cities), Now: t0, Cfg: cfg, Window: 6 * time.Hour,
		Prices: []aodp.PriceRecord{
			// 特异细布.3 那种:买一只有均价的 0.54 倍 → 挂买价抬到 8,000
			{ItemID: "T4_CLOTH_LEVEL3@3", City: "Martlock", Quality: 1,
				SellPriceMin: 10700, SellPriceMinDate: at, BuyPriceMax: 5401, BuyPriceMaxDate: at},
			// 特异松木.3 @ Thetford 那种:卖一 9,996 是均价 4,770 的 2.1 倍 → 挂卖价封到 5,724;
			// 按盘口价算的同城毛利 100.3% 超过 max_margin,两页都该判可疑
			{ItemID: "T4_CLOTH_LEVEL3@3", City: "Thetford", Quality: 1,
				SellPriceMin: 9996, SellPriceMinDate: at, BuyPriceMax: 4550, BuyPriceMaxDate: at},
		},
		History: []aodp.HistorySeries{hist("Martlock", 10_000), hist("Thetford", 4770)},
	}
	g := buildGrid(in)

	for _, tc := range []struct {
		city          string
		bid, ask      int64
		buyC, sellC   bool
		wantSuspect   bool
		screenVerdict string // 机会页同城的结论:上榜("")或拒绝原因
	}{
		{"Martlock", 8000, 10699, true, false, false, ""},
		{"Thetford", 4551, 5724, false, true, true, "implausible_margin"},
	} {
		c := findCell(g, tc.city, 1)
		if c == nil || c.Spread == nil || c.History == nil {
			t.Fatalf("%s:格子、spread、成交历史都应在,得到 %+v", tc.city, c)
		}
		sp := c.Spread
		if sp.MyBid != tc.bid || sp.MyAsk != tc.ask || sp.Clamp == nil ||
			sp.Clamp.BuyClamped != tc.buyC || sp.Clamp.SellClamped != tc.sellC || sp.Suspect != tc.wantSuspect {
			t.Fatalf("%s:照抄的两个数应为 %d → %d(钳买 %v 钳卖 %v 可疑 %v),得到 %+v / %+v",
				tc.city, tc.bid, tc.ask, tc.buyC, tc.sellC, tc.wantSuspect, sp, sp.Clamp)
		}
		// Raw 是盘口本身的价差,不钳
		if raw := float64(c.Sell.Best)/float64(c.Buy.Best) - 1; !near(sp.Raw, raw) {
			t.Fatalf("%s:raw 应是盘口价差 %v,得到 %v", tc.city, raw, sp.Raw)
		}

		// 同一份输入喂给机会页同城
		var rec aodp.PriceRecord
		for _, p := range in.Prices {
			if p.City == tc.city {
				rec = p
			}
		}
		stats := histagg.AggregateByQuality([]aodp.HistorySeries{hist(tc.city, map[string]int64{"Martlock": 10_000, "Thetford": 4770}[tc.city])},
			t0, cfg.Sizing.BaselineDays, cfg.Sizing.HistoryDays)[histagg.QualityKey{ItemID: rec.ItemID, City: tc.city, Quality: 1}]
		if stats.AvgPrice7d != c.History.Avg7d {
			t.Fatalf("%s:前提:两页的 7 日均价应是同一个数,得到 %v / %v", tc.city, stats.AvgPrice7d, c.History.Avg7d)
		}
		opp, rej := screen.Evaluate(rec, &stats, cfg, nil, t0)
		switch {
		case tc.screenVerdict == "" && rej != nil:
			t.Fatalf("%s:机会页应上榜,却被 %s 拒了: %s", tc.city, rej.Reason, rej.Detail)
		case tc.screenVerdict == "":
			if opp.MyBid != sp.MyBid || opp.MyAsk != sp.MyAsk || !near(opp.Margin, sp.Margin) ||
				!near(opp.ProfitPerUnit, sp.ProfitPerUnit) {
				t.Fatalf("%s:两页照抄的数应一致,机会页 %d → %d / %v,查价页 %d → %d / %v",
					tc.city, opp.MyBid, opp.MyAsk, opp.Margin, sp.MyBid, sp.MyAsk, sp.Margin)
			}
		case rej == nil || rej.Reason != tc.screenVerdict:
			t.Fatalf("%s:机会页应判 %s,得到 %+v / %+v", tc.city, tc.screenVerdict, opp, rej)
		}
	}
}

// 审查复现(真实库只读查询):T4_CLOTH_LEVEL1@1 @ Bridgewatch 买一 417、卖一 450、7 日均价 587。
// 以前按均价把挂买价抬到 470,比卖一还高,"进游戏照抄"的数是错单。现在下沿按 min(均价, 卖一) 算,
// 360 < 418,不钳;两边都封的格子要带着 Both,界面据此说"按带宽推出来的"
func TestGridSpread_挂买价不越过卖一(t *testing.T) {
	cfg := gridCfg()
	sp := spreadOf(450, 417, &GridHistory{Avg7d: 587}, cfg)
	if sp.Clamp != nil || sp.MyBid != 418 || sp.MyAsk != 449 {
		t.Fatalf("盘口整体在均价下面,不该钳,得到 %+v / %+v", sp, sp.Clamp)
	}
	if sp := spreadOf(359, 172, &GridHistory{Avg7d: 288}, cfg); !sp.Clamp.Both() || sp.MyBid != 230 || sp.MyAsk != 346 {
		t.Fatalf("栗木板条那种应两边都封成 230 → 346,得到 %+v / %+v", sp, sp.Clamp)
	}
}

// 没有成交历史的格子不钳(参考价缺失),band = 0 时也不钳:都退回盘口价压一档
func TestGridSpread_无历史或带宽为0时不钳(t *testing.T) {
	cfg := gridCfg()
	if sp := spreadOf(9996, 4550, nil, cfg); sp.Clamp != nil || sp.MyAsk != 9995 || sp.MyBid != 4551 {
		t.Fatalf("没有历史不该钳,得到 %+v", sp)
	}
	off := conf.Default()
	off.Filters.MakerPriceBand = 0
	if sp := spreadOf(9996, 4550, &GridHistory{Avg7d: 4770}, off); sp.Clamp != nil || sp.MyAsk != 9995 {
		t.Fatalf("band=0 不该钳,得到 %+v", sp)
	}
	if g := buildGrid(gridInput{Item: catalog.Item{ItemID: "X", MaxQuality: 1}, Cities: []string{"Thetford"},
		Now: t0, Cfg: cfg, Window: time.Hour}); g.Params.MakerPriceBand != 0.2 {
		t.Fatalf("params 里要带上带宽给界面解释用,得到 %v", g.Params.MakerPriceBand)
	}
}
