package store

import (
	"context"
	"time"

	"albion-guild/internal/model"
)

// bookOrdersSQL 一次读出一批盘口边的原始挂单,**只取数、不剔除**。
// 剔幽灵单、认续页和截断在 Go 里做(book.Build),扫描、WS 报价和查价页共用那一份规则。
// 以前这里是一条自带幽灵剔除的 SQL(BookSides),和查价页的 Go 规则是两套,
// 同一个盘口在机会页和查价页上能报出两个最优价。
//
// 按 物品 × 城市 × 方向 取**所有品质**:"这一页满没满"要按整次响应数单,
// 装备不筛品质时一页 50 单横跨几个品质(见 book.respKey)。page 就是这个数,
// 在 SQL 里数完再按请求的品质过滤——只回请求的品质,默认 qualities=[1] 时
// 装备那几档别的品质不用经 WSL 端口转发传回来。
//
// 用到的列都在 idx_live_book 里(键 item_id,location_id,quality,side,unit_price,
// INCLUDE amount,last_seen),能走 index-only。**不取 reporter、first_seen**:
// 它们不在 INCLUDE 里,取了就要回表。
//
// unit_price > 0 是防御:0 价的单只可能是解析出错,留着会被当成最优卖价。
// 品质过滤故意不用"JOIN 回请求的 key":unnest 的行数估计靠不住,规划器会把那个
// JOIN 做成嵌套循环、每行重扫一遍请求列表。按品质集合过滤会多回几行别的 key 的单
// (某个 key 要 q2、另一个要 q1 时),调用方按请求的 key 丢掉就是
const bookOrdersSQL = `
WITH scope AS (
  SELECT * FROM unnest($1::text[], $2::text[], $3::smallint[]) AS t(item_id, location_id, side)
), o AS (
  SELECT l.item_id, l.location_id, l.quality, l.side, l.unit_price, l.amount, l.last_seen,
         (COUNT(*) OVER (PARTITION BY l.item_id, l.location_id, l.side, l.last_seen))::int AS page
  FROM scope s JOIN market_order_live l
    ON l.item_id = s.item_id AND l.location_id = s.location_id AND l.side = s.side
  WHERE l.last_seen > $4::timestamptz AND l.amount > 0 AND l.unit_price > 0
)
SELECT item_id, location_id, quality, side, unit_price, amount, last_seen, page
FROM o WHERE quality = ANY($5::smallint[])`

type bookScope struct {
	item, loc string
	side      model.Side
}

// BookOrders 一次批量读出多个盘口边在 since 之后还看得到的全部挂单,Page 已按整次
// 响应(跨品质)数好。since 由调用方给(扫描用自己的 now 减窗口),不用库里的 now():
// 扫描、测试和库各用各的钟会对不齐。
//
// 返回的单不聚合、不排序、不剔除;可能多出几张别的 key 的单(见 bookOrdersSQL),
// 调用方按请求的 key 取。请求了但窗口内没有挂单的 key 就是没有单。
// 重复的 key 只查一次——不去重的话 JOIN 会把件数翻倍。
func (s *Store) BookOrders(ctx context.Context, keys []model.QuoteKey, since time.Time) ([]LiveOrder, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	scopes := make(map[bookScope]struct{}, len(keys))
	qset := map[int16]struct{}{}
	items := make([]string, 0, len(keys))
	locs := make([]string, 0, len(keys))
	sides := make([]int16, 0, len(keys))
	var quals []int16
	for _, k := range keys {
		if _, ok := qset[k.Quality]; !ok {
			qset[k.Quality] = struct{}{}
			quals = append(quals, k.Quality)
		}
		sc := bookScope{k.ItemID, k.LocationID, k.Side}
		if _, dup := scopes[sc]; dup {
			continue
		}
		scopes[sc] = struct{}{}
		items = append(items, k.ItemID)
		locs = append(locs, k.LocationID)
		sides = append(sides, int16(k.Side))
	}

	rows, err := s.pool.Query(ctx, bookOrdersSQL, items, locs, sides, since, quals)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LiveOrder
	for rows.Next() {
		var (
			o             LiveOrder
			quality, side int16
			amount        int32
		)
		if err := rows.Scan(&o.ItemID, &o.City, &quality, &side, &o.Price, &amount,
			&o.LastSeen, &o.Page); err != nil {
			return nil, err
		}
		o.Quality = int(quality)
		o.Side = model.Side(side)
		o.Amount = int64(amount)
		o.LastSeen = o.LastSeen.UTC()
		out = append(out, o)
	}
	return out, rows.Err()
}
