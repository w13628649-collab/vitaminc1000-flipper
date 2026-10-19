package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"albion-guild/internal/conf"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

type fakeLedger struct {
	trades []store.TradeRow
	wallet []model.WalletObs
	focus  []model.FocusObs
	spec   []model.SpecObs
	query  store.TradeQuery
}

func (f *fakeLedger) SaveMyTrades(_ context.Context, _ string, rows []store.TradeRow) (int, error) {
	f.trades = append(f.trades, rows...)
	return len(rows), nil
}
func (f *fakeLedger) SaveWallet(_ context.Context, o []model.WalletObs) error {
	f.wallet = append(f.wallet, o...)
	return nil
}
func (f *fakeLedger) SaveFocus(_ context.Context, o []model.FocusObs) error {
	f.focus = append(f.focus, o...)
	return nil
}
func (f *fakeLedger) SaveSpec(_ context.Context, o []model.SpecObs) error {
	f.spec = append(f.spec, o...)
	return nil
}
func (f *fakeLedger) MyTrades(_ context.Context, q store.TradeQuery) ([]store.TradeRecord, error) {
	f.query = q
	return nil, nil
}
func (f *fakeLedger) LedgerSummary(_ context.Context, since time.Time, _ int) (store.LedgerSummary, error) {
	sum := store.LedgerSummary{Since: since}
	if len(f.spec) > 0 {
		sum.Spec = &f.spec[len(f.spec)-1]
	}
	return sum, nil
}

// 税和创建费的口径:销售税按成交总额,只有卖出才有;创建费按挂单价 × 挂单总件数,只有挂单才有,
// 到期没卖完也照收
func TestLedgerCharges(t *testing.T) {
	e := conf.Economics{MarketTax: 0.04, SetupFee: 0.025, BuyOrderSetupFee: true}
	for _, c := range []struct {
		name     string
		t        model.MyTrade
		tax, fee int64
	}{
		{"秒买", model.MyTrade{Kind: model.TradeInstantBuy, Amount: 10, UnitPrice: 1000, Total: 10000}, 0, 0},
		{"秒卖", model.MyTrade{Kind: model.TradeInstantSell, Amount: 10, UnitPrice: 1000, Total: 10000}, 400, 0},
		{"卖单卖完", model.MyTrade{Kind: model.TradeOrderSold, Amount: 8, UnitPrice: 1300, Total: 10400}, 416, 260},
		{"买单买齐", model.MyTrade{Kind: model.TradeOrderBought, Amount: 4, UnitPrice: 1250, Total: 5000}, 0, 125},
		// 卖单 5 件卖掉 2 件:税按卖掉的 3000,创建费按挂的 5 件 7500
		{"卖单到期卖了一部分", model.MyTrade{Kind: model.TradeSellExpired, Amount: 2, OrderAmount: 5, UnitPrice: 1500, Total: 3000}, 120, 188},
		{"卖单到期一件没卖", model.MyTrade{Kind: model.TradeSellExpired, Amount: 0, OrderAmount: 5}, 0, 0},
		{"买单到期买了一部分", model.MyTrade{Kind: model.TradeBuyExpired, Amount: 3, OrderAmount: 10, UnitPrice: 1120, Total: 3360}, 0, 280},
		{"黑市卖单到期", model.MyTrade{Kind: model.TradeBMSellExpired, Amount: 1, OrderAmount: 1, UnitPrice: 250000, Total: 250000}, 10000, 6250},
	} {
		tax, fee := ledgerCharges(c.t, e)
		if tax != c.tax || fee != c.fee {
			t.Errorf("%s:税 %d 创建费 %d,想要 %d %d", c.name, tax, fee, c.tax, c.fee)
		}
	}
	// 挂买单不收创建费的配置
	if _, fee := ledgerCharges(model.MyTrade{Kind: model.TradeOrderBought, Amount: 4, UnitPrice: 1250, Total: 5000},
		conf.Economics{MarketTax: 0.04, SetupFee: 0.025}); fee != 0 {
		t.Errorf("BuyOrderSetupFee=false 时买单不该收创建费,得到 %d", fee)
	}
}

// 上传:校验形状、算好税和城市;邮件的时间是游戏给的不纠偏,即时买卖的是客户端钟要纠偏
func TestUpload_个人账本(t *testing.T) {
	fl := &fakeLedger{}
	s := New(nil, nil, nil, time.Minute, nil)
	s.Ledger = fl
	s.Econ = conf.Economics{MarketTax: 0.04, SetupFee: 0.025, BuyOrderSetupFee: true}

	now := time.Now().UTC().Truncate(time.Second)
	skew := 10 * time.Minute // 客户端钟慢 10 分钟
	mailAt := now.Add(-48 * time.Hour)
	batch := model.UploadBatch{Reporter: "甲", SentAt: now.Add(-skew),
		Trades: []model.MyTrade{
			{Key: "instant:1:1", Kind: model.TradeInstantSell, ItemID: "T4_PLANKS", Amount: 10, UnitPrice: 300,
				Total: 3000, Location: "0007", ObservedAt: now.Add(-skew - time.Minute)},
			{Key: "mail:9", Kind: model.TradeOrderSold, ItemID: "T5_METALBAR", Amount: 8, UnitPrice: 1300,
				Total: 10400, Location: "3005", MailID: 9, ObservedAt: mailAt},
			{Key: "", Kind: model.TradeInstantBuy, ItemID: "X"},                           // 没有自然键
			{Key: "k", Kind: "随便写", ItemID: "X"},                                          // 种类不认识
			{Key: "k2", Kind: model.TradeInstantBuy, ItemID: "X", Amount: -1, Total: 1},   // 负件数
			{Key: "k3", Kind: model.TradeInstantBuy, ItemID: "", Amount: 1, UnitPrice: 1}, // 没物品
		},
		Wallet: []model.WalletObs{{Character: "甲", Silver: 100, ObservedAt: now.Add(-skew)}, {Silver: -5}},
		Focus:  []model.FocusObs{{Character: "甲", Focus: 30000, ObservedAt: now.Add(-skew)}},
		Spec: []model.SpecObs{
			{Character: "甲", ObservedAt: now.Add(-skew), Levels: map[string][5]int{"FIBER": {60, 100, 100, 1, 1}}},
			{Character: "甲", Levels: map[string][5]int{"FIBER": {101, 0, 0, 0, 0}}}, // 超满级
			{Character: "甲", Levels: map[string][5]int{"GOLD": {1, 0, 0, 0, 0}}},    // 线不认识
			{Character: "甲"}, // 没有等级
		},
	}
	body, _ := json.Marshal(batch)
	out := postUpload(t, s, string(body))
	if out["trades"] != 2 || len(fl.trades) != 2 {
		t.Fatalf("只该存 2 笔合规的,返回 %v、存了 %+v", out, fl.trades)
	}
	sold, mail := fl.trades[0], fl.trades[1]
	if sold.Tax != 120 || sold.Fee != 0 || sold.City != "Thetford" {
		t.Fatalf("秒卖: %+v", sold)
	}
	if d := sold.ObservedAt.Sub(now.Add(-time.Minute)); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("即时买卖的时间要按钟差纠偏到 %v 附近,得到 %v", now.Add(-time.Minute), sold.ObservedAt)
	}
	if !mail.ObservedAt.Equal(mailAt) || mail.Tax != 416 || mail.Fee != 260 || mail.City != "Caerleon" {
		t.Fatalf("邮件: %+v", mail)
	}
	if len(fl.wallet) != 1 || len(fl.focus) != 1 {
		t.Fatalf("银币 %+v、专注 %+v", fl.wallet, fl.focus)
	}
	if len(fl.spec) != 1 {
		t.Fatalf("专精只该存 1 条合规的: %+v", fl.spec)
	}
	if d := fl.spec[0].ObservedAt.Sub(now); d < -2*time.Second || d > 2*time.Second {
		t.Fatalf("专精时间要按钟差纠偏,得到 %v", fl.spec[0].ObservedAt)
	}

	// 汇总接口把专精换算成 FCE 和解锁 tier,界面直接用
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ledger/summary?days=1&items=1", nil))
	var sum struct {
		Spec struct {
			Character string `json:"character"`
			Lines     map[string]struct {
				Levels       [5]int             `json:"levels"`
				FCE          map[string]float64 `json:"fce"`
				UnlockedTier int                `json:"unlocked_tier"`
			} `json:"lines"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sum); err != nil {
		t.Fatal(err)
	}
	f := sum.Spec.Lines["FIBER"]
	if sum.Spec.Character != "甲" || f.UnlockedTier != 8 || f.FCE["5"] != 32860 || f.FCE["8"] != 8110 {
		t.Fatalf("汇总里的专精: %s", rec.Body)
	}
}

func TestLedger_查询接口(t *testing.T) {
	fl := &fakeLedger{}
	s := New(nil, nil, nil, time.Minute, nil)
	s.Ledger = fl
	get := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		return rec
	}
	if rec := get("/api/ledger/trades?days=7&kind=order_sold&item=T5_METALBAR&limit=50"); rec.Code != 200 {
		t.Fatalf("状态 %d: %s", rec.Code, rec.Body)
	}
	if fl.query.Kind != model.TradeOrderSold || fl.query.ItemID != "T5_METALBAR" || fl.query.Limit != 50 ||
		time.Since(fl.query.Since) < 7*24*time.Hour-time.Minute {
		t.Fatalf("查询条件: %+v", fl.query)
	}
	for url, code := range map[string]int{
		"/api/ledger/trades?days=0":     400,
		"/api/ledger/trades?kind=乱写":    400,
		"/api/ledger/summary?days=9999": 400,
		"/api/ledger/summary":           200,
	} {
		if rec := get(url); rec.Code != code {
			t.Errorf("%s:状态 %d,想要 %d", url, rec.Code, code)
		}
	}
	s.Ledger = nil
	if rec := get("/api/ledger/summary"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("没有存储应该 503,得到 %d", rec.Code)
	}
}
