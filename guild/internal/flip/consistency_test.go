package flip

import (
	"context"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/book"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/model"
	"albion-guild/internal/scan"
	"albion-guild/internal/store"
)

// 同一批挂单、同一条 AODP 价,机会页(扫描读簿 + scan.MergeSide)、WS 报价
// (BestQuotes)、查价格子(buildGrid)和查价右栏(buildBook)必须报出同一个最优价。
// 用户多次反馈过"卡片和面板对不上",就是因为这几处以前各有一套规则。
//
// 卖方挑的是两套旧规则分歧最大的形状:续页晚到。第 1 页(100..149,满页)10 分钟前、
// 第 2 页(150..169)30 秒前到,另有一张 40 分钟前的 99(残单)。
//   - 旧扫描(SQL,slack 120s、认不得续页)把第 1 页整页当幽灵,卖一报 150
//   - 旧查价(slack 5 分钟、认得续页)卖一 100,但格子按"谁新用谁"选了 5 分钟前的 AODP 120
//
// 现在都是 100:续页保留第 1 页,抓包只比 AODP 旧 5 分钟、在 prefer_slack 以内。
// 买方是反例:抓包 30 分钟前、AODP 1 分钟前且价不同 → 几处都用 AODP
func TestSameBook_扫描报价查价三处同一个最优价(t *testing.T) {
	cfg := conf.Default()
	now := time.Now().UTC() // BestQuotes 自己取 time.Now,这里跟着真钟走
	sellKey := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Martlock", Quality: 1, Side: model.SideOffer}
	buyKey := sellKey
	buyKey.Side = model.SideRequest

	var orders []store.LiveOrder
	for i := int64(0); i < book.PageSize; i++ {
		orders = append(orders, lo(sellKey, 100+i, 1, now.Add(-10*time.Minute)))
	}
	for i := int64(0); i < 20; i++ {
		orders = append(orders, lo(sellKey, 150+i, 2, now.Add(-30*time.Second)))
	}
	orders = append(orders, lo(sellKey, 99, 7, now.Add(-40*time.Minute)), lo(buyKey, 80, 10, now.Add(-30*time.Minute)))

	rec := aodp.PriceRecord{ItemID: sellKey.ItemID, City: sellKey.LocationID, Quality: 1,
		SellPriceMin: 120, SellPriceMinDate: stamp(now.Add(-5 * time.Minute)),
		BuyPriceMax: 85, BuyPriceMaxDate: stamp(now.Add(-time.Minute))}
	const wantSell, wantBuy = 100, 85

	s := &Service{Cfg: cfg, ladder: &fakeLadder{orders: orders}}
	ctx := context.Background()

	// ① 机会页:扫描读簿 → 逐边融合
	books, err := s.storeBooks().CaptureBooks(ctx, []model.QuoteKey{sellKey, buyKey}, now.Add(-cfg.CaptureWindow()))
	if err != nil {
		t.Fatal(err)
	}
	ask := scan.MergeSide(rec.SellPriceMin, rec.SellPriceMinDate, books[sellKey], cfg, now)
	bid := scan.MergeSide(rec.BuyPriceMax, rec.BuyPriceMaxDate, books[buyKey], cfg, now)
	if !ask.UsedCapture || ask.Price != wantSell || books[sellKey].Ghosts != 1 || books[sellKey].PrevPage != book.PageSize {
		t.Fatalf("扫描卖方应用抓包 %d(剔 99、保留第 1 页),得到 %+v / %+v", wantSell, ask, books[sellKey])
	}
	if bid.UsedCapture || !bid.Superseded || bid.Price != wantBuy {
		t.Fatalf("扫描买方应被 AODP %d 盖过,得到 %+v", wantBuy, bid)
	}

	// ② WS 报价 / /api/quotes:只看抓包,最优档就是扫描读簿的第一档
	quotes, err := s.BestQuotes(ctx, []model.QuoteKey{sellKey}, 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(quotes) != 1 || quotes[0].Price != books[sellKey].Levels[0].Price || quotes[0].Price != wantSell {
		t.Fatalf("报价应和扫描读簿同一档 %d,得到 %+v", wantSell, quotes)
	}

	// ③ 查价格子:同一批单、同一条 AODP
	g := buildGrid(gridInput{
		Item:   catalog.Item{ItemID: sellKey.ItemID, MaxQuality: 1},
		Cities: lookupCities(cfg.Cities), Now: now, Cfg: cfg, Window: cfg.CaptureWindow(),
		Orders: orders, Prices: []aodp.PriceRecord{rec},
	})
	c := findCell(g, sellKey.LocationID, 1)
	if c == nil || c.Sell.Pick != "capture" || c.Sell.Best != ask.Price || c.Sell.Capture.Best != wantSell ||
		c.Sell.Capture.DroppedOrders != 1 || c.Sell.Capture.PrevPageOrders != book.PageSize {
		t.Fatalf("查价卖侧应和扫描一样是抓包 %d,得到 %+v", ask.Price, c)
	}
	if c.Buy.Pick != "aodp" || c.Buy.Best != bid.Price {
		t.Fatalf("查价买侧应和扫描一样是 AODP %d,得到 %+v", bid.Price, c.Buy)
	}
	// 龄也同一个口径:选中抓包时按 MergeSide 给的时间戳
	if c.Sell.AgeHours == nil || !near(*c.Sell.AgeHours, now.Sub(ask.At.T).Hours()) {
		t.Fatalf("查价卖侧的龄应和扫描一致,得到 %v / %v", c.Sell.AgeHours, ask.At)
	}

	// ④ 查价右栏:阶梯第一档
	b := buildBook(sellKey.ItemID, sellKey.LocationID, 1, orders, now, 0, cfg, nil)
	if b.Sell.Support.Best != wantSell || b.Sell.LevelCount != books[sellKey].LevelCount ||
		b.Sell.QtyTotal != books[sellKey].QtyTotal {
		t.Fatalf("右栏阶梯应和扫描读簿同一份(卖一 %d、%d 档、%d 件),得到 %d / %d / %d",
			wantSell, books[sellKey].LevelCount, books[sellKey].QtyTotal,
			b.Sell.Support.Best, b.Sell.LevelCount, b.Sell.QtyTotal)
	}
}
