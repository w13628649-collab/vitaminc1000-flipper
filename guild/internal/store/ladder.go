package store

import (
	"context"
	"math"
	"time"

	"albion-guild/internal/model"
)

// BookSide 是一个盘口一边剔除幽灵单之后的阶梯。
type BookSide struct {
	// Levels 从优到劣(卖单升序、买单降序),最多 maxLevels 档
	Levels []BookLevel
	// Newest 是窗口内这一边最近一次被看到的时刻,也就是"最近一眼"
	Newest time.Time
	// QtyTotal、LevelCount 是剔除幽灵单后**全部**档的合计,不受 maxLevels 截断。
	// LevelCount > len(Levels) 说明阶梯被截断了
	QtyTotal   int64
	LevelCount int
	// Ghosts 是被判为已成交/已撤单而剔掉的挂单张数
	Ghosts int
}

// bookSidesSQL 一次读出一批盘口的阶梯,顺带剔除幽灵单。
//
// 幽灵单是什么:live 表里的单只在被看到时刷新 last_seen,成交或撤单后
// 不会有任何消息,它就一直躺着,直到窗口过期。拿它算深度,会看到早就没了的
// 便宜货。规则:
//   - 最近一眼 = last_seen 在 newest − slack 以内的单(slack 盖住翻页、
//     客户端攒批和服务端 flush 的时间差)
//   - 这一眼覆盖的是从最优价到 view_edge(这一眼里最差的价)这一段
//   - 落在这一段里、却没出现在最近一眼里的旧单 → 视为已成交或已撤单,剔掉
//   - 比 view_edge 更差(或正好等于它)的旧单,可能只是没翻到那一页 → 保留
//
// 这条规则不依赖游戏怎么分页,只假设一次观测覆盖从最优价开始的连续一段
// (还没实机验证,见 capture-plan「仍待核实」)。前提不成立时把 slack 调到
// 不小于窗口,就退回按窗口过滤的旧行为。
//
// 另一个前提是"最近一眼"本身可信。多开串城的错归单带着新时间戳进来,
// 会成为被串入那座城的 newest 和 view_edge,把那座城上一眼的真实挂单当幽灵
// 剔掉。所以调用方对 ingest.ConflictedSince 报出来的 key 要把 slack 放到窗口大小。
//
// 用到的列都在 idx_live_book 里(键 item_id,location_id,quality,side,unit_price,
// INCLUDE amount,last_seen),能走 index-only。**不取 reporter**:它不在 INCLUDE
// 里,取了就要回表。
//
// $6 是秒数(float8),不传 interval 字符串,免得依赖 Duration.String() 的写法。
// unit_price > 0 是防御:0 价的单只可能是解析出错,留着会被当成最优卖价。
//
// 幽灵张数用窗口聚合(fg)随行带下去,**不要**改回"单独 GROUP BY 再 JOIN 回来":
// unnest 那一步的行数估计恒为 1,规划器会把那个 JOIN 做成嵌套循环,
// 每一档都重扫一遍全部 key 的聚合结果,792 个 key 实测 1.45s,且随档数平方涨。
// 每个 key 至少有"最近一眼"那几张单不是幽灵,所以 WHERE NOT ghost 之后
// ghosts 这一列一定还在。
const bookSidesSQL = `
WITH want AS (
  SELECT * FROM unnest($1::text[], $2::text[], $3::smallint[], $4::smallint[])
         AS t(item_id, location_id, quality, side)
), o AS (
  SELECT w.item_id, w.location_id, w.quality, w.side, l.unit_price, l.amount, l.last_seen,
         MAX(l.last_seen) OVER k AS newest
  FROM want w JOIN market_order_live l
    ON l.item_id = w.item_id AND l.location_id = w.location_id
   AND l.quality = w.quality AND l.side = w.side
  WHERE l.last_seen > $5::timestamptz AND l.amount > 0 AND l.unit_price > 0
  WINDOW k AS (PARTITION BY w.item_id, w.location_id, w.quality, w.side)
), v AS (
  SELECT o.*, CASE WHEN side = 0
    THEN MAX(unit_price) FILTER (WHERE last_seen >= newest - $6::float8 * interval '1 second') OVER k
    ELSE MIN(unit_price) FILTER (WHERE last_seen >= newest - $6::float8 * interval '1 second') OVER k
  END AS view_edge
  FROM o WINDOW k AS (PARTITION BY item_id, location_id, quality, side)
), f AS (
  SELECT v.*, NOT (last_seen >= newest - $6::float8 * interval '1 second'
                   OR (side = 0 AND unit_price >= view_edge)
                   OR (side = 1 AND unit_price <= view_edge)) AS ghost
  FROM v
), fg AS (
  SELECT f.*, (COUNT(*) FILTER (WHERE ghost) OVER k)::int AS ghosts
  FROM f WINDOW k AS (PARTITION BY item_id, location_id, quality, side)
), lv AS (
  SELECT item_id, location_id, quality, side, unit_price,
         SUM(amount)::bigint AS qty, COUNT(*)::int AS orders,
         MAX(last_seen) AS seen, MIN(newest) AS newest, MIN(ghosts) AS ghosts
  FROM fg WHERE NOT ghost GROUP BY 1, 2, 3, 4, 5
), r AS (
  SELECT lv.*,
    row_number() OVER (PARTITION BY item_id, location_id, quality, side
      ORDER BY CASE WHEN side = 0 THEN unit_price ELSE -unit_price END) AS rk,
    (SUM(qty) OVER p)::bigint AS key_qty, (COUNT(*) OVER p)::int AS key_levels
  FROM lv WINDOW p AS (PARTITION BY item_id, location_id, quality, side)
)
SELECT item_id, location_id, quality, side, unit_price, qty, orders,
       seen, newest, key_qty, key_levels, ghosts
FROM r
WHERE rk <= $7::bigint
ORDER BY item_id, location_id, quality, side, rk`

// BookSides 一次批量读出多个盘口边剔除幽灵单后的阶梯。
//
// since 由调用方给(扫描用自己的 now 减窗口),不用库里的 now():
// 扫描、测试和库各用各的钟会对不齐。slack 是"同一眼"的宽容度,
// maxLevels ≤ 0 表示不截断。
//
// 请求了但窗口内没有任何挂单的 key 不出现在结果里,调用方当成"没有抓包"。
// 重复的 key 只查一次——不去重的话 JOIN 会把件数翻倍。
func (s *Store) BookSides(ctx context.Context, keys []model.QuoteKey, since time.Time,
	slack time.Duration, maxLevels int) (map[model.QuoteKey]BookSide, error) {
	out := map[model.QuoteKey]BookSide{}
	if len(keys) == 0 {
		return out, nil
	}
	if maxLevels <= 0 {
		maxLevels = math.MaxInt32
	}
	if slack < 0 {
		slack = 0
	}

	dedup := make(map[model.QuoteKey]struct{}, len(keys))
	items := make([]string, 0, len(keys))
	locs := make([]string, 0, len(keys))
	quals := make([]int16, 0, len(keys))
	sides := make([]int16, 0, len(keys))
	for _, k := range keys {
		if _, dup := dedup[k]; dup {
			continue
		}
		dedup[k] = struct{}{}
		items = append(items, k.ItemID)
		locs = append(locs, k.LocationID)
		quals = append(quals, k.Quality)
		sides = append(sides, int16(k.Side))
	}

	rows, err := s.pool.Query(ctx, bookSidesSQL,
		items, locs, quals, sides, since, slack.Seconds(), int64(maxLevels))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			k         model.QuoteKey
			side      int16
			l         BookLevel
			newest    time.Time
			keyQty    int64
			keyLevels int
			ghosts    int
		)
		if err := rows.Scan(&k.ItemID, &k.LocationID, &k.Quality, &side,
			&l.Price, &l.Depth, &l.Orders, &l.Seen, &newest, &keyQty, &keyLevels, &ghosts); err != nil {
			return nil, err
		}
		k.Side = model.Side(side)
		b := out[k]
		b.Levels = append(b.Levels, l) // SQL 已按 key、从优到劣排好
		b.Newest, b.QtyTotal, b.LevelCount, b.Ghosts = newest, keyQty, keyLevels, ghosts
		out[k] = b
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
