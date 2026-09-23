package store

import (
	"context"
	"time"

	"albion-guild/internal/model"
)

// 查价页专用的两个读函数。单独放一个文件,是因为 store.go / history.go
// 另有一条后端线在改,这里只加不改。
//
// 两个函数都**显式收一个时间点**(since / first),不收 fresh 时长:
// 调用方用自己的 now 算窗口,库时钟和应用时钟不会混在一条查询里,
// 测试也能把时间钉死。

// itemOrdersCap 是单个物品一次最多读多少张单。
// 热门物品翻过几轮也就几千张;这个上限只防一个坏掉的上报把一次查价拖死。
const itemOrdersCap = 20000

// LiveOrder 是一张活跃挂单。**不按价位聚合**:判"这张单是不是上一轮
// 浏览留下来的"要看每张单自己的 last_seen,聚合以后就分不开了。
type LiveOrder struct {
	City      string
	Quality   int
	Side      model.Side
	Price     int64
	Amount    int64
	FirstSeen time.Time
	LastSeen  time.Time
}

// ItemOrders 读一个物品在所有城市、所有品质、两个方向上 since 之后还看得到的挂单。
//
// 走 idx_live_book 的 item_id 前缀;first_seen 不在索引的 INCLUDE 里要回表,
// 单个物品几百到几千行,可以接受。
func (s *Store) ItemOrders(ctx context.Context, itemID string, since time.Time) ([]LiveOrder, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT location_id, quality, side, unit_price, amount, first_seen, last_seen
		FROM market_order_live
		WHERE item_id = $1 AND last_seen > $2 AND amount > 0
		ORDER BY location_id, quality, side, unit_price
		LIMIT $3`, itemID, since, itemOrdersCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LiveOrder
	for rows.Next() {
		var o LiveOrder
		var quality, side int16
		var amount int32
		if err := rows.Scan(&o.City, &quality, &side, &o.Price, &amount,
			&o.FirstSeen, &o.LastSeen); err != nil {
			return nil, err
		}
		o.Quality = int(quality)
		o.Side = model.Side(side)
		o.Amount = int64(amount)
		o.FirstSeen = o.FirstSeen.UTC()
		o.LastSeen = o.LastSeen.UTC()
		out = append(out, o)
	}
	return out, rows.Err()
}

// HistoryRow 是 market_history 里的一根日线。
type HistoryRow struct {
	City      string
	Quality   int
	Day       time.Time
	ItemCount int64
	AvgPrice  int64
	Source    string
}

// ItemHistory 读一个物品 first 之后的全部日线,**两个来源都返回**,
// 由调用方按 城市×品质 整条选一边——同一份服务端成交,取平均会重复计数。
func (s *Store) ItemHistory(ctx context.Context, itemID string, first time.Time) ([]HistoryRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT location_id, quality, bucket, item_count, avg_price, source
		FROM market_history
		WHERE item_id = $1 AND timescale = $2 AND bucket >= $3
		ORDER BY location_id, quality, bucket`, itemID, int16(TimescaleDaily), first)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []HistoryRow
	for rows.Next() {
		var h HistoryRow
		var quality int16
		if err := rows.Scan(&h.City, &quality, &h.Day, &h.ItemCount, &h.AvgPrice, &h.Source); err != nil {
			return nil, err
		}
		h.Quality = int(quality)
		h.Day = h.Day.UTC().Truncate(24 * time.Hour)
		out = append(out, h)
	}
	return out, rows.Err()
}
