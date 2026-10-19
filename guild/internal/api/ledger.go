package api

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strings"
	"time"

	"albion-guild/internal/conf"
	"albion-guild/internal/gamedata"
	"albion-guild/internal/ingest"
	"albion-guild/internal/model"
	"albion-guild/internal/refine"
	"albion-guild/internal/store"
	"albion-guild/internal/world"
)

// 个人账本:收上传里自己的成交、银币、专注;给界面查明细和汇总。

type ledgerStore interface {
	SaveMyTrades(ctx context.Context, reporter string, rows []store.TradeRow) (int, error)
	SaveWallet(ctx context.Context, obs []model.WalletObs) error
	SaveFocus(ctx context.Context, obs []model.FocusObs) error
	SaveSpec(ctx context.Context, obs []model.SpecObs) error
	MyTrades(ctx context.Context, q store.TradeQuery) ([]store.TradeRecord, error)
	LedgerSummary(ctx context.Context, since time.Time, itemLimit int) (store.LedgerSummary, error)
}

const (
	defaultLedgerDays = 30
	maxLedgerDays     = 3650
)

func (s *Server) registerLedger(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ledger/trades", s.handleLedgerTrades)
	mux.HandleFunc("GET /api/ledger/summary", s.handleLedgerSummary)
}

var tradeKinds = map[string]bool{
	model.TradeInstantBuy: true, model.TradeInstantSell: true, model.TradeOrderSold: true,
	model.TradeOrderBought: true, model.TradeSellExpired: true, model.TradeBuyExpired: true,
	model.TradeBMSellExpired: true,
}

// tradeRejected 校验一笔上传的成交,不合规返回原因。/api/upload 不鉴权,形状要卡
func tradeRejected(t model.MyTrade) string {
	switch {
	case !tradeKinds[t.Kind]:
		return "种类不认识"
	case t.Key == "" || len(t.Key) > 200:
		return "自然键为空或太长"
	case t.ItemID == "" || len(t.ItemID) > 100:
		return "物品为空或太长"
	case t.Amount < 0 || t.OrderAmount < 0 || t.UnitPrice < 0 || t.Total < 0:
		return "件数或银币为负"
	}
	return ""
}

// ledgerCharges 按税率算一笔的销售税和创建费,入库时算好存下来,以后改税率不动历史账目。
//
//   - 销售税:卖出才有,按成交总额
//   - 创建费:挂单才有,按挂单价 × 挂单总件数,不管成没成交——到期没卖掉也已经付了。
//     挂单价不知道时(卖单到期一件没卖掉,邮件里没有单价)算不出,记 0
func ledgerCharges(t model.MyTrade, e conf.Economics) (tax, fee int64) {
	pct := func(rate float64, base int64) int64 { return int64(math.Round(rate * float64(base))) }
	switch t.Kind {
	case model.TradeInstantSell, model.TradeOrderSold, model.TradeSellExpired, model.TradeBMSellExpired:
		tax = pct(e.MarketTax, t.Total)
	}
	qty := int64(t.OrderAmount)
	if qty == 0 {
		qty = int64(t.Amount)
	}
	listed := t.UnitPrice * qty
	switch t.Kind {
	case model.TradeOrderSold, model.TradeSellExpired, model.TradeBMSellExpired:
		fee = pct(e.SetupFee, listed)
	case model.TradeOrderBought, model.TradeBuyExpired:
		if e.BuyOrderSetupFee {
			fee = pct(e.SetupFee, listed)
		}
	}
	return tax, fee
}

func (s *Server) economics() conf.Economics {
	if s.Econ.MarketTax > 0 {
		return s.Econ
	}
	return conf.Default().Economics
}

// saveLedger 存一批上传里的个人账本。失败只记日志,同 saveCaptureExtras。
//
// 时间纠偏:即时买卖、银币、专注是客户端的钟,按挂单同一套口径平移;
// 邮件的时间是游戏服务端给的收到时间,本来就准,不动
func (s *Server) saveLedger(ctx context.Context, batch model.UploadBatch) (trades int) {
	if s.Ledger == nil {
		return 0
	}
	now := time.Now()
	shift := func(at time.Time) time.Time { return ingest.ShiftObserved(at, batch.SentAt, now) }
	e := s.economics()
	if len(batch.Trades) > 0 {
		rows := make([]store.TradeRow, 0, len(batch.Trades))
		rejected := 0
		for _, t := range batch.Trades {
			if why := tradeRejected(t); why != "" {
				rejected++
				slog.Debug("上传的成交不合规,丢弃", "key", t.Key, "why", why)
				continue
			}
			if t.MailID == 0 {
				t.ObservedAt = shift(t.ObservedAt)
			} else if t.ObservedAt.IsZero() {
				t.ObservedAt = now
			}
			t.Character, t.ItemID, t.Location = stripNUL(t.Character), stripNUL(t.ItemID), stripNUL(t.Location)
			tax, fee := ledgerCharges(t, e)
			rows = append(rows, store.TradeRow{MyTrade: t, City: world.City(t.Location), Tax: tax, Fee: fee})
		}
		if rejected > 0 {
			slog.Warn("上传的成交不合规,丢弃", "count", rejected, "reporter", batch.Reporter)
		}
		n, err := s.Ledger.SaveMyTrades(ctx, batch.Reporter, rows)
		if err != nil {
			slog.Error("写入成交失败", "err", err, "count", len(rows), "reporter", batch.Reporter)
		}
		trades = n
	}
	if len(batch.Wallet) > 0 {
		w := make([]model.WalletObs, 0, len(batch.Wallet))
		for _, o := range batch.Wallet {
			if o.Silver < 0 {
				continue
			}
			o.Character, o.ObservedAt = stripNUL(o.Character), shift(o.ObservedAt)
			w = append(w, o)
		}
		if err := s.Ledger.SaveWallet(ctx, w); err != nil {
			slog.Error("写入银币余额失败", "err", err, "count", len(w))
		}
	}
	if len(batch.Focus) > 0 {
		f := make([]model.FocusObs, 0, len(batch.Focus))
		for _, o := range batch.Focus {
			if o.Focus < 0 || math.IsNaN(o.Focus) || math.IsInf(o.Focus, 0) {
				continue
			}
			o.Character, o.ObservedAt = stripNUL(o.Character), shift(o.ObservedAt)
			f = append(f, o)
		}
		if err := s.Ledger.SaveFocus(ctx, f); err != nil {
			slog.Error("写入专注余额失败", "err", err, "count", len(f))
		}
	}
	if len(batch.Spec) > 0 {
		sp := make([]model.SpecObs, 0, len(batch.Spec))
		for _, o := range batch.Spec {
			if why := specRejected(o); why != "" {
				slog.Warn("上传的专精不合规,丢弃", "why", why, "reporter", batch.Reporter)
				continue
			}
			o.Character, o.ObservedAt = stripNUL(o.Character), shift(o.ObservedAt)
			sp = append(sp, o)
		}
		if err := s.Ledger.SaveSpec(ctx, sp); err != nil {
			slog.Error("写入精炼专精失败", "err", err, "count", len(sp))
		}
	}
	return trades
}

// specRejected 校验一次上传的专精:线只认 gamedata.Lines,等级在 0~100
func specRejected(o model.SpecObs) string {
	if len(o.Levels) == 0 {
		return "没有等级"
	}
	for l, lv := range o.Levels {
		if !slices.Contains(gamedata.Lines, l) {
			return "线不认识: " + l
		}
		for _, n := range lv {
			if n < 0 || n > gamedata.MaxSpecLevel {
				return fmt.Sprintf("%s 的等级 %d 超出 0~%d", l, n, gamedata.MaxSpecLevel)
			}
		}
	}
	return ""
}

// specView 是汇总接口里的专精:每条线的等级,和按它算好的 FCE、解锁到 T 几,
// 精炼页直接拿来填(公式只在 refine 里有一份)
type specView struct {
	Character  string              `json:"character"`
	ObservedAt time.Time           `json:"observed_at"`
	Lines      map[string]specLine `json:"lines"`
}

type specLine struct {
	Levels       [5]int          `json:"levels"` // T4~T8
	FCE          map[int]float64 `json:"fce"`
	UnlockedTier int             `json:"unlocked_tier"` // 3 = T4 都没解锁
}

func newSpecView(o *model.SpecObs) *specView {
	if o == nil {
		return nil
	}
	v := &specView{Character: o.Character, ObservedAt: o.ObservedAt, Lines: map[string]specLine{}}
	for l, lv := range o.Levels {
		v.Lines[l] = specLine{Levels: lv, FCE: refine.SpecFCE(lv), UnlockedTier: refine.SpecUnlockedTier(lv)}
	}
	return v
}

func ledgerSince(r *http.Request) (time.Time, int, bool) {
	days := intOr(r.URL.Query().Get("days"), defaultLedgerDays)
	if days <= 0 || days > maxLedgerDays {
		return time.Time{}, days, false
	}
	return time.Now().Add(-time.Duration(days) * 24 * time.Hour), days, true
}

// handleLedgerTrades:GET /api/ledger/trades?days=&kind=&item=&limit=
func (s *Server) handleLedgerTrades(w http.ResponseWriter, r *http.Request) {
	since, days, ok := ledgerSince(r)
	if !ok {
		http.Error(w, "days 只能是 1~3650", http.StatusBadRequest)
		return
	}
	if s.Ledger == nil {
		http.Error(w, "没有存储", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	kind := strings.TrimSpace(q.Get("kind"))
	if kind != "" && !tradeKinds[kind] {
		http.Error(w, "kind 不认识", http.StatusBadRequest)
		return
	}
	rows, err := s.Ledger.MyTrades(r.Context(), store.TradeQuery{Since: since, Kind: kind,
		ItemID: strings.TrimSpace(q.Get("item")), Limit: intOr(q.Get("limit"), 200)})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []store.TradeRecord{}
	}
	writeJSON(w, map[string]any{"days": days, "trades": rows})
}

// handleLedgerSummary:GET /api/ledger/summary?days=&items=
func (s *Server) handleLedgerSummary(w http.ResponseWriter, r *http.Request) {
	since, days, ok := ledgerSince(r)
	if !ok {
		http.Error(w, "days 只能是 1~3650", http.StatusBadRequest)
		return
	}
	if s.Ledger == nil {
		http.Error(w, "没有存储", http.StatusServiceUnavailable)
		return
	}
	sum, err := s.Ledger.LedgerSummary(r.Context(), since, intOr(r.URL.Query().Get("items"), 100))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"days": days, "summary": sum, "spec": newSpecView(sum.Spec),
		"notes": []string{
			"物品进出是现金流口径:买进还没卖掉的货算在花出去的钱里,囤货期看起来是亏的",
			"市场邮件不带品质,挂单成交的品质记 0",
			"卖单到期一件没卖掉时邮件里没有单价,创建费算不出,记 0",
		}})
}
