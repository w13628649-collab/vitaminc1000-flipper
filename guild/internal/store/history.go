package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"albion-guild/internal/aodp"
	"albion-guild/internal/rank"
)

// TimescaleDaily 是 market_history.timescale 里"日线"那一档。
const TimescaleDaily = 1

// SourceAODP / SourceCapture 标记这条历史是从哪来的。
const (
	SourceAODP    = "aodp"
	SourceCapture = "capture"
)

// WriteHistory 写入(或覆盖)日线。
//
// 同一天重复抓到以最新一次为准——history 会被反复抓,
// 累加的话销量会翻倍。
func (s *Store) WriteHistory(ctx context.Context, seriesList []aodp.HistorySeries, source string) (int, error) {
	observed := time.Now().UTC()
	rows := make([][]any, 0, 256)
	for _, series := range seriesList {
		for _, p := range series.Data {
			if !p.Timestamp.Valid() {
				continue
			}
			rows = append(rows, []any{
				series.ItemID, series.Location, int16(series.Quality),
				int16(TimescaleDaily), p.Timestamp.T.UTC().Truncate(24 * time.Hour),
				p.ItemCount, nil, p.AvgPrice, source, observed,
			})
		}
	}
	if len(rows) == 0 {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// CopyFrom 进不了 ON CONFLICT,所以先落到临时表再合并。
	// 一次几万行,比逐条 INSERT 快一个数量级
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE hist_in
		(LIKE market_history INCLUDING DEFAULTS) ON COMMIT DROP`); err != nil {
		return 0, err
	}
	cols := []string{"item_id", "location_id", "quality", "timescale", "bucket",
		"item_count", "silver_total", "avg_price", "source", "observed_at"}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"hist_in"}, cols, pgx.CopyFromRows(rows)); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO market_history
		SELECT DISTINCT ON (item_id, location_id, quality, timescale, bucket, source) *
		FROM hist_in
		ON CONFLICT (item_id, location_id, quality, timescale, bucket, source)
		DO UPDATE SET item_count = EXCLUDED.item_count,
		              avg_price  = EXCLUDED.avg_price,
		              observed_at = EXCLUDED.observed_at`)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// DailyHistory 取榜单要的那一段日线。
//
// 同一个桶可能同时有抓包和 AODP 两条——它们是**同一份服务端数据**,
// 取平均会重复计数,所以这里每个桶只留一条,优先抓包。
func (s *Store) DailyHistory(ctx context.Context, first, last time.Time,
	quality int, city string) ([]rank.Daily, error) {

	const q = `
		SELECT DISTINCT ON (item_id, location_id, bucket)
		       item_id, location_id, quality, bucket, item_count, avg_price
		FROM market_history
		WHERE timescale = $1 AND quality = $2
		  AND bucket BETWEEN $3 AND $4
		  AND ($5 = '' OR location_id = $5)
		ORDER BY item_id, location_id, bucket,
		         CASE source WHEN 'capture' THEN 0 ELSE 1 END`

	if city == rank.AllCities {
		city = ""
	}
	rows, err := s.pool.Query(ctx, q, int16(TimescaleDaily), int16(quality), first, last, city)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []rank.Daily
	for rows.Next() {
		var d rank.Daily
		var quality int16
		if err := rows.Scan(&d.ItemID, &d.City, &quality, &d.Day, &d.ItemCount, &d.AvgPrice); err != nil {
			return nil, err
		}
		d.Quality = int(quality)
		d.Day = d.Day.UTC().Truncate(24 * time.Hour)
		out = append(out, d)
	}
	return out, rows.Err()
}

// HistoryCoverage 是库里现在攒了多少历史。
type HistoryCoverage struct {
	Rows     int64  `json:"rows"`
	Items    int64  `json:"items"`
	Days     int64  `json:"days"`
	FirstDay string `json:"first_day"`
	LastDay  string `json:"last_day"`
}

func (s *Store) HistoryCoverage(ctx context.Context) (HistoryCoverage, error) {
	var c HistoryCoverage
	var first, last *time.Time
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(DISTINCT item_id),
		COUNT(DISTINCT bucket), MIN(bucket), MAX(bucket)
		FROM market_history WHERE timescale = $1`, int16(TimescaleDaily)).
		Scan(&c.Rows, &c.Items, &c.Days, &first, &last)
	if err != nil {
		return c, err
	}
	if first != nil {
		c.FirstDay = first.UTC().Format("2006-01-02")
	}
	if last != nil {
		c.LastDay = last.UTC().Format("2006-01-02")
	}
	return c, nil
}

// CitiesWithHistory 是库里有历史数据的城市,给界面下拉框用。
func (s *Store) CitiesWithHistory(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT location_id FROM market_history ORDER BY location_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var city string
		if err := rows.Scan(&city); err != nil {
			return nil, err
		}
		out = append(out, city)
	}
	return out, rows.Err()
}

// CityQuote 是某城某品质的盘口一行。深度是抓包才有的东西,AODP 给不了。
type CityQuote struct {
	City     string `json:"city"`
	Quality  int    `json:"quality"`
	SellMin  int64  `json:"sell_min"`
	SellQty  int64  `json:"sell_qty"`
	BuyMax   int64  `json:"buy_max"`
	BuyQty   int64  `json:"buy_qty"`
	LastSeen string `json:"last_seen"`
}

// QuotesByCity 把一个物品在各城各品质的最优价一次查出来,给查价页用。
//
// 卖单取最低价、买单取最高价,**按品质分开**——不分开的话,
// 一件优秀品质的高价会盖过更便宜的杰出品质,那是两件不同的货。
func (s *Store) QuotesByCity(ctx context.Context, itemID string, fresh time.Duration) ([]CityQuote, error) {
	const q = `
		WITH live AS (
		    SELECT location_id, quality, side, unit_price, amount, last_seen
		    FROM market_order_live
		    WHERE item_id = $1 AND last_seen > now() - $2::interval
		),
		best AS (
		    SELECT location_id, quality, side,
		           MIN(unit_price) FILTER (WHERE side = 0) AS sell_min,
		           MAX(unit_price) FILTER (WHERE side = 1) AS buy_max
		    FROM live GROUP BY location_id, quality, side
		)
		SELECT b.location_id, b.quality,
		       COALESCE(MAX(b.sell_min), 0),
		       COALESCE(SUM(l.amount) FILTER (WHERE l.side = 0 AND l.unit_price = b.sell_min), 0),
		       COALESCE(MAX(b.buy_max), 0),
		       COALESCE(SUM(l.amount) FILTER (WHERE l.side = 1 AND l.unit_price = b.buy_max), 0),
		       MAX(l.last_seen)
		FROM best b
		JOIN live l ON l.location_id = b.location_id AND l.quality = b.quality
		GROUP BY b.location_id, b.quality
		ORDER BY b.location_id, b.quality`

	rows, err := s.pool.Query(ctx, q, itemID, fresh.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []CityQuote
	for rows.Next() {
		var c CityQuote
		var quality int16
		var lastSeen *time.Time
		if err := rows.Scan(&c.City, &quality, &c.SellMin, &c.SellQty,
			&c.BuyMax, &c.BuyQty, &lastSeen); err != nil {
			return nil, err
		}
		c.Quality = int(quality)
		if lastSeen != nil {
			c.LastSeen = lastSeen.UTC().Format(time.RFC3339)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
