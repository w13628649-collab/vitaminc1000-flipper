package store

import (
	"context"
	"strings"
	"time"

	"albion-guild/internal/model"
)

// pagedNulls 是 pagedSQL 里"整页统计"那一路在 o 的其余列上的占位,类型要和 o 那一路对上。
var pagedNulls = map[string]string{
	"item_id": "NULL::text", "quality": "NULL::smallint", "unit_price": "NULL::bigint",
	"amount": "NULL::integer", "first_seen": "NULL::timestamptz",
}

// pagedSQL 把 ctes(最后一个 CTE 叫 o,是要返回的那批单,列是 cols)包成带 page / page_worst
// 的查询:给 o 里每张单数它所在那次响应的整页 —— 同一 城市 × 方向 × last_seen 的全部单,
// **跨物品、跨品质**,对整张表数,不只数请求到的那几个物品(一页跨物品,见 book.respKey)。
// page_worst 是整页最差的价:卖单最高、买单最低,book.Build 判续页用。
//
// 查法:先取 o 里出现过的响应时刻(resp),按 last_seen 回表(idx_live_stale)把每一页数成一行,
// 和 o 并成一个集合,用窗口函数把那一行的数分给同一页里的单。**不用 JOIN 把数出来的页接回 o**:
// o 是 unnest 连出来的,规划器对它的行数估计差三个数量级(估 10 行、实际 4 万),会把那个 JOIN
// 做成嵌套循环 + 连接过滤,实测 1750 个 key 要 56 秒。并集 + 窗口只有一种做法(按响应排序),
// 不看估计;先把每页数成一行,排序的行数就是 o 加上页数,而不是 o 加上那几页的全部单。
// since 条件是冗余的(等值 last_seen 已经在窗口里),写上是让规划器改走哈希连接时只扫窗口那一段
func pagedSQL(ctes string, cols []string, since, orderBy string) string {
	own := strings.Join(cols, ", ")
	stat := make([]string, len(cols))
	for i, c := range cols {
		switch c {
		case "location_id", "side", "last_seen":
			stat[i] = "g." + c
		default:
			stat[i] = pagedNulls[c]
		}
	}
	q := ctes + `, resp AS (
  SELECT DISTINCT location_id, side, last_seen FROM o
), u AS (
  SELECT ` + own + `, NULL::int AS page, NULL::bigint AS page_worst FROM o
  UNION ALL
  SELECT ` + strings.Join(stat, ", ") + `, g.page, g.page_worst
  FROM (
    SELECT x.location_id, x.side, x.last_seen, count(*)::int AS page,
           CASE WHEN x.side = 1 THEN min(x.unit_price) ELSE max(x.unit_price) END AS page_worst
    FROM resp r JOIN market_order_live x
      ON x.last_seen = r.last_seen AND x.location_id = r.location_id AND x.side = r.side
    WHERE x.last_seen > ` + since + `::timestamptz AND x.amount > 0 AND x.unit_price > 0
    GROUP BY x.location_id, x.side, x.last_seen
  ) g
)
SELECT ` + own + `, page, page_worst FROM (
  SELECT ` + own + `, page IS NULL AS mine,
         max(page) OVER w AS page, max(page_worst) OVER w AS page_worst
  FROM u WINDOW w AS (PARTITION BY location_id, side, last_seen)
) z WHERE mine`
	if orderBy != "" {
		q += "\nORDER BY " + orderBy
	}
	return q
}

// bookOrdersSQL 一次读出一批盘口边的原始挂单,**只取数、不剔除**。
// 剔幽灵单、认续页和截断在 Go 里做(book.Build),扫描、WS 报价和查价页共用那一份规则。
// 以前这里是一条自带幽灵剔除的 SQL(BookSides),和查价页的 Go 规则是两套,
// 同一个盘口在机会页和查价页上能报出两个最优价。
//
// page / page_worst 由 pagedSQL 对整张表数,所以这里只取请求的品质就够了——
// 默认 qualities=[1] 时装备那几档别的品质不用经 WSL 端口转发传回来。
//
// 取挂单那一步用到的列都在 idx_live_book 里(键 item_id,location_id,quality,side,unit_price,
// INCLUDE amount,last_seen),能走 index-only。**不取 reporter、first_seen**:
// 它们不在 INCLUDE 里,取了就要回表。
//
// unit_price > 0 是防御:0 价的单只可能是解析出错,留着会被当成最优卖价。
// 品质过滤故意不用"JOIN 回请求的 key":unnest 的行数估计靠不住,规划器会把那个
// JOIN 做成嵌套循环、每行重扫一遍请求列表。按品质集合过滤会多回几行别的 key 的单
// (某个 key 要 q2、另一个要 q1 时),调用方按请求的 key 丢掉就是
var bookOrdersSQL = pagedSQL(`
WITH scope AS (
  SELECT * FROM unnest($1::text[], $2::text[], $3::smallint[]) AS t(item_id, location_id, side)
), o AS (
  SELECT l.item_id, l.location_id, l.quality, l.side, l.unit_price, l.amount, l.last_seen
  FROM scope s JOIN market_order_live l
    ON l.item_id = s.item_id AND l.location_id = s.location_id AND l.side = s.side
  WHERE l.last_seen > $4::timestamptz AND l.amount > 0 AND l.unit_price > 0
    AND l.quality = ANY($5::smallint[])
)`, []string{"item_id", "location_id", "quality", "side", "unit_price", "amount", "last_seen"}, "$4", "")

type bookScope struct {
	item, loc string
	side      model.Side
}

// BookOrders 一次批量读出多个盘口边在 since 之后还看得到的全部挂单,Page / PageWorst
// 已按整次响应(跨物品、跨品质)数好。since 由调用方给(扫描用自己的 now 减窗口),
// 不用库里的 now():扫描、测试和库各用各的钟会对不齐。
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
			&o.LastSeen, &o.Page, &o.PageWorst); err != nil {
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
