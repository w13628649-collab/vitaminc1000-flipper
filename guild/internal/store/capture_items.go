package store

import (
	"context"
	"time"
)

// CapturedItem 是抓包窗口里有挂单的一个物品。
type CapturedItem struct {
	ItemID string
	// Qty 是窗口内这个物品的挂单件数合计(两边、各城、各品质加起来),
	// 扫描超出上限时按它排序截断:件数多说明成员真在翻它
	Qty      int64
	Orders   int
	LastSeen time.Time
}

// CapturedItems 列出 since 之后在 cities × qualities 范围内有抓包挂单的物品,件数从多到少。
//
// 给扫描扩物品集用:配置清单只有几十个物品,成员翻市场翻到的
// (T7/T8 精炼材料、附魔资源、石砌块……)大半不在里面,不并进来的话
// 抓得再多,机会板也看不见。城市和品质的过滤和扫描读簿同一套口径——
// 只认 cities 里的城市名:黑市(3003)、没收敛成城市名的原始地点 id 都进不来;
// Brecilien 在默认城市里就进得来(测试里传的 cities 不含它,所以被挡)。
//
// 不剔幽灵单:这里只决定"扫不扫这个物品",不决定价格,多扫一个物品的代价
// 只是 AODP 请求里多一个 id。走 idx_live_stale(last_seen)的范围扫描,
// 行数只和窗口内的挂单数有关,和 live 表攒了多久无关。
func (s *Store) CapturedItems(ctx context.Context, cities []string, qualities []int,
	since time.Time) ([]CapturedItem, error) {
	if len(cities) == 0 || len(qualities) == 0 {
		return nil, nil
	}
	quals := make([]int16, len(qualities))
	for i, q := range qualities {
		quals[i] = int16(q)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT item_id, SUM(amount)::bigint, COUNT(*)::int, MAX(last_seen)
		FROM market_order_live
		WHERE last_seen > $1::timestamptz AND amount > 0 AND unit_price > 0
		  AND location_id = ANY($2::text[]) AND quality = ANY($3::smallint[])
		GROUP BY item_id
		ORDER BY 2 DESC, item_id`, since, cities, quals)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CapturedItem
	for rows.Next() {
		var c CapturedItem
		if err := rows.Scan(&c.ItemID, &c.Qty, &c.Orders, &c.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
