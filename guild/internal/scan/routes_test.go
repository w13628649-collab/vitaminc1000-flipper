package scan

import (
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/histagg"
	"albion-guild/internal/model"
	"albion-guild/internal/screen"
)

// 跨城那一步拿到的是融合层给的逐边深度,不只是融合后的价:
// 销地买方来自抓包(T6 实抓阶梯),秒卖只吃得到近价那 11 件;
// 没有抓包的那几边照样由 ResolveSides 标成 AODP
func TestFindRoutes_用上融合层的逐边深度(t *testing.T) {
	item := "T6_METALBAR_LEVEL4@4"
	fresh := ago(time.Hour)
	raw := []aodp.PriceRecord{
		{ItemID: item, City: "Thetford", Quality: 1, SellPriceMin: 200_000, SellPriceMinDate: fresh},
		{ItemID: item, City: "Martlock", Quality: 1,
			SellPriceMin: 400_000, SellPriceMinDate: fresh, BuyPriceMax: 250_000, BuyPriceMaxDate: fresh},
	}
	stats := map[histagg.QualityKey]histagg.Stats{}
	for _, c := range []struct {
		city string
		avg  float64
	}{{"Thetford", 210_000}, {"Martlock", 250_000}} {
		stats[histagg.QualityKey{ItemID: item, City: c.city, Quality: 1}] = histagg.Stats{
			AvgPrice7d: c.avg, AvgPrice30d: c.avg, DailyVolumeQty: 5000,
			DailyVolumeSilver: 5000 * c.avg, DaysWithData7d: 7, LastPoint: now.Add(-12 * time.Hour),
		}
	}
	cfg := overlayCfg()
	cfg.Cities = []string{"Thetford", "Martlock"}
	books := map[model.QuoteKey]CapturedSide{
		// 卖方也翻到了:卖一是张 2 件的孤单,挂卖被 thin_book 拦,只剩秒买秒卖
		askKey(item, "Martlock"): side(now.Add(-5*time.Minute),
			depth.Level{Price: 262_000, Qty: 2}, depth.Level{Price: 300_000, Qty: 50}),
		bidKey(item, "Martlock"): side(now.Add(-5*time.Minute),
			depth.Level{Price: 260066, Qty: 1}, depth.Level{Price: 260065, Qty: 2}, depth.Level{Price: 260064, Qty: 1},
			depth.Level{Price: 260060, Qty: 2}, depth.Level{Price: 260050, Qty: 2}, depth.Level{Price: 260040, Qty: 1},
			depth.Level{Price: 260000, Qty: 2}, depth.Level{Price: 53001, Qty: 15}, depth.Level{Price: 1, Qty: 2000}),
	}
	prices, sides, _ := overlay(raw, books, cfg, now)
	cat := catalog.New([]catalog.Item{{ItemID: item, NameZH: "T6 钢条"}}, nil, "")

	var found bool
	for _, r := range findRoutes(prices, stats, sides, cat, cfg, now) {
		if r.FromCity != "Thetford" || r.ToCity != "Martlock" {
			continue
		}
		found = true
		if r.Mode != "taker-taker" {
			t.Fatalf("挂卖被拦之后应选秒买秒卖,得到 %s(%+v)", r.Mode, r.Modes)
		}
		if r.SellPrice != 260_000 || r.SellDepthQty != 11 || r.Bottleneck != "depth" {
			t.Fatalf("秒卖应吃到 260,000、盘口 11 件,得到 %d / %d / %s", r.SellPrice, r.SellDepthQty, r.Bottleneck)
		}
		if r.ToBid == nil || r.ToBid.Source != screen.SourceCapture || r.FromAsk == nil || r.FromAsk.Source != screen.SourceAODP {
			t.Fatalf("销地 bid 应是抓包、产地 ask 应补成 AODP,得到 %+v / %+v", r.ToBid, r.FromAsk)
		}
		if r.BuyLegSource != screen.SourceAODP || r.SellLegSource != screen.SourceCapture {
			t.Fatalf("两条腿来源不对:%s / %s", r.BuyLegSource, r.SellLegSource)
		}
	}
	if !found {
		t.Fatalf("应有一条 Thetford → Martlock 的路线")
	}

	// 对照:不给 sides(纯 AODP),秒卖按买一不限件数算,没有深度字段
	for _, r := range findRoutes(prices, stats, nil, cat, conf.Default(), now) {
		if r.SellDepthQty != 0 || r.BuyDepthQty != 0 || r.DepthChecked {
			t.Fatalf("没有 sides 时不该有深度字段,得到 %+v", r)
		}
	}
}
