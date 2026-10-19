package store

import (
	"context"
	"testing"
	"time"

	"albion-guild/internal/model"
)

func TestLedger_存取和汇总(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	row := func(key, kind, item string, amount, orderAmount int32, unit, tax, fee int64, at time.Time) TradeRow {
		return TradeRow{MyTrade: model.MyTrade{Key: key, Kind: kind, Character: "甲", ItemID: item,
			Amount: amount, OrderAmount: orderAmount, UnitPrice: unit, Total: unit * int64(amount),
			Location: "0007", ObservedAt: at}, City: "Thetford", Tax: tax, Fee: fee}
	}
	rows := []TradeRow{
		// T5_METALBAR:秒买 10 件 @1000,挂卖单卖掉 8 件 @1300(税 4%、创建费 2.5%)
		row("instant:1:1", model.TradeInstantBuy, "T5_METALBAR", 10, 0, 1000, 0, 0, t0),
		row("mail:9001", model.TradeOrderSold, "T5_METALBAR", 8, 0, 1300, 416, 260, t0.Add(time.Hour)),
		// T4_PLANKS:挂的卖单一件没卖掉就到期,白付创建费
		row("mail:9002", model.TradeSellExpired, "T4_PLANKS", 0, 5, 0, 0, 0, t0.Add(2*time.Hour)),
		// 很早以前的一笔,汇总窗口外
		row("instant:2:2", model.TradeInstantBuy, "T8_OLD", 1, 0, 99999, 0, 0, t0.Add(-48*time.Hour)),
	}
	n, err := st.SaveMyTrades(ctx, "甲", rows)
	if err != nil || n != 4 {
		t.Fatalf("写入 %d 笔,err %v", n, err)
	}
	// 同一批再传一次(邮件重读、退回队列重发):一笔都不新增
	if n, err := st.SaveMyTrades(ctx, "甲", rows); err != nil || n != 0 {
		t.Fatalf("重复写入新增了 %d 笔,err %v", n, err)
	}

	got, err := st.MyTrades(ctx, TradeQuery{Since: t0.Add(-time.Hour)})
	if err != nil || len(got) != 3 || got[0].Key != "mail:9002" || got[2].Key != "instant:1:1" {
		t.Fatalf("查询: %+v err %v", got, err)
	}
	if got[1].Tax != 416 || got[1].Fee != 260 || got[1].City != "Thetford" || got[1].MailID != 0 {
		// MailID 在这批里没填,入库是 NULL、查出来是 0
		t.Fatalf("字段: %+v", got[1])
	}
	only, _ := st.MyTrades(ctx, TradeQuery{Since: t0.Add(-time.Hour), ItemID: "T5_METALBAR", Kind: model.TradeOrderSold})
	if len(only) != 1 || only[0].Key != "mail:9001" {
		t.Fatalf("按物品和种类筛: %+v", only)
	}

	if err := st.SaveWallet(ctx, []model.WalletObs{{Character: "甲", Silver: 100, ObservedAt: t0},
		{Character: "甲", Silver: 250, ObservedAt: t0.Add(time.Minute)}}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveFocus(ctx, []model.FocusObs{{Character: "甲", Focus: 30000, ObservedAt: t0}}); err != nil {
		t.Fatal(err)
	}

	sum, err := st.LedgerSummary(ctx, t0.Add(-time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]KindTotal{}
	for _, k := range sum.ByKind {
		kinds[k.Kind] = k
	}
	if kinds[model.TradeInstantBuy].Total != 10000 || kinds[model.TradeOrderSold].Tax != 416 ||
		kinds[model.TradeSellExpired].Unfilled != 1 || len(kinds) != 3 {
		t.Fatalf("按种类: %+v", sum.ByKind)
	}
	// 金属锭:花 10000,卖 10400 − 416 − 260 = 9724,净 −276(还有 2 件没卖,现金流口径算亏)
	var bar ItemFlow
	for _, f := range sum.ByItem {
		if f.ItemID == "T5_METALBAR" {
			bar = f
		}
	}
	if bar.BoughtQty != 10 || bar.BuyCost != 10000 || bar.SoldQty != 8 || bar.SellNet != 9724 || bar.Net != -276 {
		t.Fatalf("物品进出: %+v", bar)
	}
	if sum.Wallet == nil || sum.Wallet.Silver != 250 || sum.Focus == nil || sum.Focus.Focus != 30000 {
		t.Fatalf("余额: %+v %+v", sum.Wallet, sum.Focus)
	}
	if sum.Spec != nil {
		t.Fatalf("没存过专精应当是 nil: %+v", sum.Spec)
	}
}

// 专精:取最新的一次,不受汇总窗口限制(等级只涨不掉,旧读数也比没有强)
func TestLedger_专精(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	old := model.SpecObs{Character: "甲", ObservedAt: t0.Add(-30 * 24 * time.Hour),
		Levels: map[string][5]int{"ORE": {76, 100, 90, 1, 1}}}
	cur := model.SpecObs{Character: "甲", ObservedAt: t0,
		Levels: map[string][5]int{"ORE": {76, 100, 91, 1, 1}, "FIBER": {60, 100, 100, 1, 1}}}
	if err := st.SaveSpec(ctx, []model.SpecObs{old, cur, cur}); err != nil {
		t.Fatal(err)
	}
	sum, err := st.LedgerSummary(ctx, t0.Add(-time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Spec == nil || !sum.Spec.ObservedAt.Equal(t0) || sum.Spec.Levels["ORE"] != [5]int{76, 100, 91, 1, 1} ||
		sum.Spec.Levels["FIBER"] != [5]int{60, 100, 100, 1, 1} {
		t.Fatalf("专精: %+v", sum.Spec)
	}
}
