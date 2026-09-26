package flip

import (
	"context"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

func findPrice(recs []aodp.PriceRecord, item, city string, q int) *aodp.PriceRecord {
	for i := range recs {
		if recs[i].ItemID == item && recs[i].City == city && recs[i].Quality == q {
			return &recs[i]
		}
	}
	return nil
}

// 审查 medium:残单规则和择边函数统一了,喂进去的 AODP 却不是同一份 —— 卡片用全量那份快照
// 重算,面板现取 AODP。实测全量后 13 分钟 57 边里 2 条价不同、1 条来源不同。
// 现在查价页现取回来的价并进重算用的快照,查价格子也把快照并进来,逐字段新的胜:
// 下一次重算之后卡片和面板用的是同一条 AODP
func TestFreshAODP_查价现取的价并进重算且格子和卡片同一份(t *testing.T) {
	s, a, books := newRVService(t, rvConfig())
	ctx := context.Background()
	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if o := rvOpp(s.LastScan(), "T5_CLOTH"); o == nil || o.SellPrice != 1450 {
		t.Fatalf("全量后应是快照里 AODP 的卖价 1450,得到 %+v", o)
	}
	// Windows 上 time.Now() 刻度粗,等过一个刻度,"查价在全量之后"才分得出先后
	time.Sleep(20 * time.Millisecond)
	now := time.Now().UTC()
	snapRec := findPrice(s.snap.Load().Prices, "T5_CLOTH", "Lymhurst", 1)
	if snapRec == nil {
		t.Fatal("快照里应有 T5_CLOTH @ Lymhurst")
	}

	// 查价页现取:卖价 1分钟前更新成 1420;买价这条比快照里的还旧(不能倒退)
	fetched := []aodp.PriceRecord{
		{ItemID: "T5_CLOTH", City: "Lymhurst", Quality: 1,
			SellPriceMin: 1420, SellPriceMinDate: stamp(now.Add(-time.Minute)),
			BuyPriceMax: 990, BuyPriceMaxDate: stamp(snapRec.BuyPriceMaxDate.T.Add(-time.Hour))},
		{ItemID: "T5_CLOTH", City: "Lymhurst", Quality: 2}, // 快照里没有的格子:格子要,重算不添
	}
	grid := s.gridPrices("T5_CLOTH", fetched, now)
	g := findPrice(grid, "T5_CLOTH", "Lymhurst", 1)
	if g == nil || g.SellPriceMin != 1420 || g.BuyPriceMax != snapRec.BuyPriceMax ||
		!g.BuyPriceMaxDate.T.Equal(snapRec.BuyPriceMaxDate.T) || findPrice(grid, "T5_CLOTH", "Lymhurst", 2) == nil {
		t.Fatalf("格子应取卖价 1420(现取的新)、买价取快照的(更新),得到 %+v", grid)
	}

	// 重算:不打 AODP,卖价用上查价现取的 1420
	before := len(a.requests())
	res, err := s.Reevaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.requests()) != before {
		t.Fatal("重算不该打 AODP")
	}
	o := rvOpp(res, "T5_CLOTH")
	if o == nil || o.SellPrice != 1420 || o.BuyPrice != g.BuyPriceMax {
		t.Fatalf("重算应用上查价现取的卖价 1420、买价仍是快照的,得到 %+v", o)
	}
	if findPrice(s.snap.Load().Prices, "T5_CLOTH", "Lymhurst", 2) != nil {
		t.Fatal("快照里没有的格子不添")
	}
	if p := findPrice(s.snap.Load().Prices, "T5_CLOTH", "Lymhurst", 1); p.SellPriceMin != 1450 {
		t.Fatalf("s.snap 存的是全量原样,评估时才并,得到 %d", p.SellPriceMin)
	}

	// 同一份 AODP 喂同一个择边:格子卖侧和卡片卖侧同价同来源
	books.setAsk("T5_CLOTH", 1400, 50, now.Add(-30*time.Minute)) // 抓包比现取的 AODP 旧 29 分钟且价不同 → AODP
	res, _ = s.Reevaluate(ctx)
	o = rvOpp(res, "T5_CLOTH")
	k := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1, Side: model.SideOffer}
	cell := findCell(buildGrid(gridInput{
		Item: catalog.Item{ItemID: "T5_CLOTH", MaxQuality: 1}, Cities: []string{"Lymhurst"}, Now: now,
		Cfg: s.Cfg, Window: s.Cfg.CaptureWindow(), Prices: grid,
		Orders: []store.LiveOrder{lo(k, 1400, 50, now.Add(-30*time.Minute))},
	}), "Lymhurst", 1)
	if o == nil || cell == nil || o.SellPrice != cell.Sell.Best || o.Ask.Source != cell.Sell.Pick || cell.Sell.Pick != "aodp" {
		t.Fatalf("卡片和格子应同是 AODP 1420,得到 卡片 %+v / 格子 %+v", o, cell)
	}

	// 查价这次没取到 AODP:格子用快照 ∪ 以前现取过的,也就是卡片在用的那份
	if g := findPrice(s.gridPrices("T5_CLOTH", nil, now), "T5_CLOTH", "Lymhurst", 1); g == nil || g.SellPriceMin != 1420 {
		t.Fatalf("没取到时应退到快照 ∪ 现取过的,得到 %+v", g)
	}

	// 下一次全量:它开始拉之前记下的现取价淘汰(快照不会比它旧)
	time.Sleep(20 * time.Millisecond)
	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if left := s.fresh.since(s.snap.Load().FetchedAt, ""); len(left) != 0 {
		t.Fatalf("全量之后旧的现取价应淘汰,还剩 %d 条", len(left))
	}
}

func TestFreshPrices_逐字段新的胜且按时长淘汰(t *testing.T) {
	var f freshPrices
	t0 := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	f.note([]aodp.PriceRecord{{ItemID: "A", City: "X", Quality: 1,
		SellPriceMin: 100, SellPriceMinDate: stamp(t0), BuyPriceMax: 80, BuyPriceMaxDate: stamp(t0)}}, t0)
	f.note([]aodp.PriceRecord{{ItemID: "A", City: "X", Quality: 1,
		SellPriceMin: 90, SellPriceMinDate: stamp(t0.Add(time.Minute)), // 更新
		BuyPriceMax: 70, BuyPriceMaxDate: stamp(t0.Add(-time.Minute)), // 更旧:不倒退
	}, {ItemID: "B", City: "X", Quality: 1}}, t0.Add(time.Minute))
	got := f.since(time.Time{}, "A")
	if len(got) != 1 || got[0].SellPriceMin != 90 || got[0].BuyPriceMax != 80 {
		t.Fatalf("应逐字段取新的,得到 %+v", got)
	}
	if len(f.since(time.Time{}, "")) != 2 {
		t.Fatal("item 为空时返回全部")
	}
	// 超过 freshKeep 没再记过的,下一次 note 时淘汰
	f.note([]aodp.PriceRecord{{ItemID: "C", City: "X", Quality: 1}}, t0.Add(freshKeep+2*time.Minute))
	if left := f.since(time.Time{}, ""); len(left) != 1 || left[0].ItemID != "C" {
		t.Fatalf("超过 %v 的应淘汰,得到 %+v", freshKeep, left)
	}
}
