package store

import (
	"context"
	"fmt"
	"time"
)

// Trade 是一笔记账。计划和实际分开记——两者的差就是模型误差,
// 而模型误差才是这张表真正要采集的东西。
type Trade struct {
	ID      int64  `json:"id"`
	Owner   string `json:"owner"`
	ItemID  string `json:"item_id"`
	Quality int    `json:"quality"`

	Kind     string `json:"kind"`
	BuyCity  string `json:"buy_city"`
	SellCity string `json:"sell_city"`
	Mode     string `json:"mode"`

	PlannedQty     int64   `json:"planned_qty"`
	PlannedBuy     int64   `json:"planned_buy"`
	PlannedSell    int64   `json:"planned_sell"`
	MarketDailyQty float64 `json:"market_daily_qty"`

	FilledBuyQty    *int64 `json:"filled_buy_qty,omitempty"`
	FilledBuyPrice  *int64 `json:"filled_buy_price,omitempty"`
	FilledSellQty   *int64 `json:"filled_sell_qty,omitempty"`
	FilledSellPrice *int64 `json:"filled_sell_price,omitempty"`
	RealizedProfit  *int64 `json:"realized_profit,omitempty"`

	Status   string     `json:"status"`
	Note     string     `json:"note"`
	OpenedAt time.Time  `json:"opened_at"`
	ClosedAt *time.Time `json:"closed_at,omitempty"`
}

func (s *Store) OpenTrade(ctx context.Context, t Trade) (int64, error) {
	if t.Owner == "" || t.ItemID == "" || t.PlannedQty <= 0 {
		return 0, fmt.Errorf("owner / item_id / planned_qty 必填")
	}
	var id int64
	err := s.pool.QueryRow(ctx, `INSERT INTO trade
		(owner, item_id, quality, kind, buy_city, sell_city, mode,
		 planned_qty, planned_buy, planned_sell, market_daily_qty, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id`,
		t.Owner, t.ItemID, int16(t.Quality), t.Kind, t.BuyCity, t.SellCity, t.Mode,
		t.PlannedQty, t.PlannedBuy, t.PlannedSell, t.MarketDailyQty, t.Note).Scan(&id)
	return id, err
}

// CloseTrade 填实际成交。status 由填了多少自动判,不让调用方瞎填。
func (s *Store) CloseTrade(ctx context.Context, id int64, owner string, t Trade) error {
	status := "abandoned"
	if t.FilledBuyQty != nil && *t.FilledBuyQty > 0 {
		status = "partial"
		if t.FilledSellQty != nil && *t.FilledSellQty >= *t.FilledBuyQty {
			status = "filled"
		}
	}
	tag, err := s.pool.Exec(ctx, `UPDATE trade SET
		filled_buy_qty = $3, filled_buy_price = $4,
		filled_sell_qty = $5, filled_sell_price = $6,
		realized_profit = $7, note = COALESCE(NULLIF($8,''), note),
		status = $9, closed_at = now()
		WHERE id = $1 AND owner = $2`,
		id, owner, t.FilledBuyQty, t.FilledBuyPrice,
		t.FilledSellQty, t.FilledSellPrice, t.RealizedProfit, t.Note, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("没有这笔记录,或者不是你的")
	}
	return nil
}

func (s *Store) Trades(ctx context.Context, owner, status string, limit int) ([]Trade, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT id, owner, item_id, quality, kind,
		buy_city, sell_city, mode, planned_qty, planned_buy, planned_sell, market_daily_qty,
		filled_buy_qty, filled_buy_price, filled_sell_qty, filled_sell_price,
		realized_profit, status, note, opened_at, closed_at
		FROM trade
		WHERE ($1 = '' OR owner = $1) AND ($2 = '' OR status = $2)
		ORDER BY opened_at DESC LIMIT $3`, owner, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Trade
	for rows.Next() {
		var t Trade
		var quality int16
		if err := rows.Scan(&t.ID, &t.Owner, &t.ItemID, &quality, &t.Kind,
			&t.BuyCity, &t.SellCity, &t.Mode, &t.PlannedQty, &t.PlannedBuy,
			&t.PlannedSell, &t.MarketDailyQty, &t.FilledBuyQty, &t.FilledBuyPrice,
			&t.FilledSellQty, &t.FilledSellPrice, &t.RealizedProfit,
			&t.Status, &t.Note, &t.OpenedAt, &t.ClosedAt); err != nil {
			return nil, err
		}
		t.Quality = int(quality)
		out = append(out, t)
	}
	return out, rows.Err()
}

// Calibration 是从真实成交记录里反推出来的模型修正。
//
// 这是整个项目唯一能自我修正的地方。所有别的数字都来自市场快照,
// 只有这张表知道"照着模型下的单,实际成交了多少"。
type Calibration struct {
	Trades   int     `json:"trades"`
	Closed   int     `json:"closed"`
	FillRate float64 `json:"fill_rate"`
	// SuggestedAbsorbRatio 是实际吃下的量 / 模型当时估的日成交量。
	// 现在配置里那个 0.20 是拍脑袋的,这个数才是实测出来的
	SuggestedAbsorbRatio float64 `json:"suggested_absorb_ratio"`
	HasSuggestion        bool    `json:"has_suggestion"`

	// PlannedProfit/RealizedProfit 的比值就是模型高估了多少倍
	PlannedProfit  int64   `json:"planned_profit"`
	RealizedProfit int64   `json:"realized_profit"`
	Accuracy       float64 `json:"accuracy"`
	HasAccuracy    bool    `json:"has_accuracy"`
}

// Calibrate 汇总一个人(owner 为空则全公会)的实盘反馈。
func (s *Store) Calibrate(ctx context.Context, owner string) (Calibration, error) {
	var c Calibration
	var plannedQty, filledQty, marketQty *float64
	var planned, realized *int64

	err := s.pool.QueryRow(ctx, `SELECT
		COUNT(*),
		COUNT(*) FILTER (WHERE status IN ('filled','partial','abandoned')),
		SUM(planned_qty)      FILTER (WHERE closed_at IS NOT NULL),
		SUM(COALESCE(filled_buy_qty,0)) FILTER (WHERE closed_at IS NOT NULL),
		SUM(market_daily_qty) FILTER (WHERE closed_at IS NOT NULL AND market_daily_qty > 0),
		SUM((planned_sell - planned_buy) * planned_qty) FILTER (WHERE closed_at IS NOT NULL),
		SUM(realized_profit)  FILTER (WHERE realized_profit IS NOT NULL)
		FROM trade WHERE ($1 = '' OR owner = $1)`, owner).
		Scan(&c.Trades, &c.Closed, &plannedQty, &filledQty, &marketQty, &planned, &realized)
	if err != nil {
		return c, err
	}

	if plannedQty != nil && *plannedQty > 0 && filledQty != nil {
		c.FillRate = *filledQty / *plannedQty
	}
	// 实际吃下的量占市场日成交量的比例——这就是实测的 absorb_ratio
	if marketQty != nil && *marketQty > 0 && filledQty != nil && *filledQty > 0 {
		c.SuggestedAbsorbRatio = *filledQty / *marketQty
		c.HasSuggestion = true
	}
	if planned != nil {
		c.PlannedProfit = *planned
	}
	if realized != nil {
		c.RealizedProfit = *realized
	}
	if c.PlannedProfit > 0 && realized != nil {
		c.Accuracy = float64(c.RealizedProfit) / float64(c.PlannedProfit)
		c.HasAccuracy = true
	}
	return c, nil
}

// DeleteTrade 删掉一笔。记错了总要能撤——
// 校准是拿这些记录反推模型的,留着一笔错的比没有更糟。
func (s *Store) DeleteTrade(ctx context.Context, id int64, owner string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM trade WHERE id = $1 AND owner = $2`, id, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("没有这笔记录,或者不是你的")
	}
	return nil
}
