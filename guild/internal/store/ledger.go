package store

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"albion-guild/internal/model"
)

// 个人账本的存取(0010_ledger.sql)。

// TradeRow 是要入库的一笔:抓包来的 MyTrade,加上入库时算好的城市、税、创建费(api 层算)。
type TradeRow struct {
	model.MyTrade
	City string `json:"city"`
	Tax  int64  `json:"tax"`
	Fee  int64  `json:"fee"`
}

// TradeRecord 是查出来的一笔。
type TradeRecord struct {
	ID int64 `json:"id"`
	TradeRow
	ReceivedAt time.Time `json:"received_at"`
}

// SaveMyTrades 存一批,按自然键去重:已经有的跳过。返回新写入的笔数。
func (s *Store) SaveMyTrades(ctx context.Context, reporter string, rows []TradeRow) (int, error) {
	if len(rows) == 0 {
		return 0, nil
	}
	batch := &pgx.Batch{}
	for _, r := range rows {
		batch.Queue(`
			INSERT INTO my_trade (key, kind, character, item_id, quality, amount, order_amount,
			                      unit_price, total, tax, fee, city, raw_location, order_id, mail_id,
			                      observed_at, reporter)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,0),NULLIF($15,0),$16,$17)
			ON CONFLICT (key) DO NOTHING`,
			r.Key, r.Kind, r.Character, r.ItemID, r.Quality, r.Amount, r.OrderAmount,
			r.UnitPrice, r.Total, r.Tax, r.Fee, r.City, r.Location, r.OrderID, r.MailID,
			r.ObservedAt, reporter)
	}
	res := s.pool.SendBatch(ctx, batch)
	defer res.Close()
	n := 0
	for range rows {
		tag, err := res.Exec()
		if err != nil {
			return n, fmt.Errorf("写入成交: %w", err)
		}
		n += int(tag.RowsAffected())
	}
	return n, nil
}

// SaveWallet 存银币余额观测。同一时刻的重复观测跳过。
func (s *Store) SaveWallet(ctx context.Context, obs []model.WalletObs) error {
	if len(obs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, o := range obs {
		batch.Queue(`INSERT INTO my_wallet (character, silver, observed_at) VALUES ($1,$2,$3)
			ON CONFLICT DO NOTHING`, o.Character, o.Silver, o.ObservedAt)
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

// SaveFocus 存专注余额观测。同一时刻的重复观测跳过。
func (s *Store) SaveFocus(ctx context.Context, obs []model.FocusObs) error {
	if len(obs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, o := range obs {
		batch.Queue(`INSERT INTO my_focus (character, focus, observed_at) VALUES ($1,$2,$3)
			ON CONFLICT DO NOTHING`, o.Character, o.Focus, o.ObservedAt)
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

// SaveSpec 存精炼专精观测。同一时刻的重复观测跳过。
func (s *Store) SaveSpec(ctx context.Context, obs []model.SpecObs) error {
	if len(obs) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, o := range obs {
		lv, err := json.Marshal(o.Levels)
		if err != nil {
			return fmt.Errorf("专精等级序列化: %w", err)
		}
		batch.Queue(`INSERT INTO my_spec (character, levels, observed_at) VALUES ($1,$2,$3)
			ON CONFLICT DO NOTHING`, o.Character, lv, o.ObservedAt)
	}
	return s.pool.SendBatch(ctx, batch).Close()
}

// TradeQuery 是查成交的条件。零值字段不筛。
type TradeQuery struct {
	Since  time.Time
	Kind   string
	ItemID string
	Limit  int // 0 用 200,最多 5000
}

// MyTrades 按成交时刻倒序查。
func (s *Store) MyTrades(ctx context.Context, q TradeQuery) ([]TradeRecord, error) {
	if q.Limit <= 0 {
		q.Limit = 200
	}
	q.Limit = min(q.Limit, 5000)
	rows, err := s.pool.Query(ctx, `
		SELECT id, key, kind, character, item_id, quality, amount, order_amount, unit_price, total,
		       tax, fee, city, raw_location, COALESCE(order_id, 0), COALESCE(mail_id, 0),
		       observed_at, received_at
		FROM my_trade
		WHERE observed_at >= $1 AND ($2 = '' OR kind = $2) AND ($3 = '' OR item_id = $3)
		ORDER BY observed_at DESC, id DESC
		LIMIT $4`, q.Since, q.Kind, q.ItemID, q.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TradeRecord
	for rows.Next() {
		var r TradeRecord
		if err := rows.Scan(&r.ID, &r.Key, &r.Kind, &r.Character, &r.ItemID, &r.Quality, &r.Amount,
			&r.OrderAmount, &r.UnitPrice, &r.Total, &r.Tax, &r.Fee, &r.City, &r.Location,
			&r.OrderID, &r.MailID, &r.ObservedAt, &r.ReceivedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// 哪些种类算买、哪些算卖
var (
	buyKinds  = []string{model.TradeInstantBuy, model.TradeOrderBought, model.TradeBuyExpired}
	sellKinds = []string{model.TradeInstantSell, model.TradeOrderSold, model.TradeSellExpired, model.TradeBMSellExpired}
)

// ItemFlow 是一个物品在一段时间里的进出:买了多少花了多少、卖了多少收了多少。
//
// 这是现金流口径,不是配对利润:买进还没卖掉的货不算成本也不算收入,所以单看 Net
// 会把"囤货期"算成亏。配对利润要做先进先出,下一步
type ItemFlow struct {
	ItemID    string `json:"item_id"`
	BoughtQty int64  `json:"bought_qty"`
	BuyCost   int64  `json:"buy_cost"` // 买入总额 + 买单创建费
	SoldQty   int64  `json:"sold_qty"`
	SellNet   int64  `json:"sell_net"` // 卖出总额 − 销售税 − 卖单创建费
	Net       int64  `json:"net"`      // SellNet − BuyCost
	Trades    int    `json:"trades"`
}

// KindTotal 是一个种类的合计。
type KindTotal struct {
	Kind   string `json:"kind"`
	Count  int    `json:"count"`
	Amount int64  `json:"amount"`
	Total  int64  `json:"total"`
	Tax    int64  `json:"tax"`
	Fee    int64  `json:"fee"`
	// Unfilled 是挂了一件都没成交就到期的单数:白付了创建费
	Unfilled int `json:"unfilled"`
}

// LedgerSummary 是一段时间的账本汇总。
type LedgerSummary struct {
	Since  time.Time        `json:"since"`
	ByKind []KindTotal      `json:"by_kind"`
	ByItem []ItemFlow       `json:"by_item"`
	Wallet *model.WalletObs `json:"wallet,omitempty"` // 最近一次银币余额
	Focus  *model.FocusObs  `json:"focus,omitempty"`  // 最近一次专注余额
	Spec   *model.SpecObs   `json:"spec,omitempty"`   // 最近一次精炼专精
}

// Summary 汇总 since 以来的账本。物品按 |Net| 从大到小,最多 itemLimit 个。
func (s *Store) LedgerSummary(ctx context.Context, since time.Time, itemLimit int) (LedgerSummary, error) {
	out := LedgerSummary{Since: since}
	if itemLimit <= 0 {
		itemLimit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT kind, count(*), COALESCE(sum(amount),0), COALESCE(sum(total),0),
		       COALESCE(sum(tax),0), COALESCE(sum(fee),0),
		       count(*) FILTER (WHERE amount = 0)
		FROM my_trade WHERE observed_at >= $1
		GROUP BY kind ORDER BY kind`, since)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var k KindTotal
		if err := rows.Scan(&k.Kind, &k.Count, &k.Amount, &k.Total, &k.Tax, &k.Fee, &k.Unfilled); err != nil {
			rows.Close()
			return out, err
		}
		out.ByKind = append(out.ByKind, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	rows, err = s.pool.Query(ctx, `
		SELECT item_id,
		       COALESCE(sum(amount) FILTER (WHERE kind = ANY($2)), 0),
		       COALESCE(sum(total + fee) FILTER (WHERE kind = ANY($2)), 0),
		       COALESCE(sum(amount) FILTER (WHERE kind = ANY($3)), 0),
		       COALESCE(sum(total - tax - fee) FILTER (WHERE kind = ANY($3)), 0),
		       count(*)
		FROM my_trade WHERE observed_at >= $1
		GROUP BY item_id`, since, buyKinds, sellKinds)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var f ItemFlow
		if err := rows.Scan(&f.ItemID, &f.BoughtQty, &f.BuyCost, &f.SoldQty, &f.SellNet, &f.Trades); err != nil {
			rows.Close()
			return out, err
		}
		f.Net = f.SellNet - f.BuyCost
		out.ByItem = append(out.ByItem, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	sortFlows(out.ByItem)
	if len(out.ByItem) > itemLimit {
		out.ByItem = out.ByItem[:itemLimit]
	}

	var w model.WalletObs
	err = s.pool.QueryRow(ctx, `SELECT character, silver, observed_at FROM my_wallet
		ORDER BY observed_at DESC LIMIT 1`).Scan(&w.Character, &w.Silver, &w.ObservedAt)
	switch {
	case err == nil:
		out.Wallet = &w
	case err != pgx.ErrNoRows:
		return out, err
	}
	var f model.FocusObs
	err = s.pool.QueryRow(ctx, `SELECT character, focus, observed_at FROM my_focus
		ORDER BY observed_at DESC LIMIT 1`).Scan(&f.Character, &f.Focus, &f.ObservedAt)
	switch {
	case err == nil:
		out.Focus = &f
	case err != pgx.ErrNoRows:
		return out, err
	}
	// 专精不看 since:等级只涨不掉,一个月前读到的也比没有强(界面写明是什么时候读的)
	var sp model.SpecObs
	var lv []byte
	err = s.pool.QueryRow(ctx, `SELECT character, levels, observed_at FROM my_spec
		ORDER BY observed_at DESC LIMIT 1`).Scan(&sp.Character, &lv, &sp.ObservedAt)
	switch {
	case err == nil:
		if err := json.Unmarshal(lv, &sp.Levels); err != nil {
			return out, fmt.Errorf("专精等级: %w", err)
		}
		out.Spec = &sp
	case err != pgx.ErrNoRows:
		return out, err
	}
	return out, nil
}

func sortFlows(fs []ItemFlow) {
	abs := func(n int64) int64 {
		if n < 0 {
			return -n
		}
		return n
	}
	slices.SortStableFunc(fs, func(a, b ItemFlow) int { return cmp.Compare(abs(b.Net), abs(a.Net)) })
}
