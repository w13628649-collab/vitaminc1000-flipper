// Package store 封装 PostgreSQL 读写。
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"albion-guild/internal/model"
)

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析 DSN: %w", err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("连接数据库: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping 数据库: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// WriteOrders 把一批**状态确实变了**的挂单写进两张表。
//
// 调用方(ingest)负责去重,这里不再判断——数据库层面挡重复做不到,
// 因为 hypertable 的唯一约束必须包含时间列,
// ON CONFLICT (order_id, unit_price, amount) 根本建不出来。
func (s *Store) WriteOrders(ctx context.Context, reporter string, orders []model.MarketOrder) error {
	if len(orders) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// ① 当前态:同一张单反复上报就更新价格/数量/last_seen
	liveRows := make([][]any, 0, len(orders))
	// ② 历史流:纯 append
	evtRows := make([][]any, 0, len(orders))
	for _, o := range orders {
		liveRows = append(liveRows, []any{
			o.OrderID, o.ItemID, o.LocationID, o.Quality, o.Enchant, int16(o.Side),
			o.UnitPrice, o.Amount, o.ExpiresAt, o.ObservedAt, o.ObservedAt, reporter,
			o.RawLocationID,
		})
		evtRows = append(evtRows, []any{
			o.OrderID, o.ObservedAt, o.ItemID, o.LocationID, o.Quality, o.Enchant,
			int16(o.Side), o.UnitPrice, o.Amount, reporter, o.RawLocationID,
		})
	}

	// live 表没法直接 CopyFrom(要 upsert),走临时表再 merge
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE tmp_live (LIKE market_order_live) ON COMMIT DROP`); err != nil {
		return fmt.Errorf("建临时表: %w", err)
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"tmp_live"},
		[]string{"order_id", "item_id", "location_id", "quality", "enchant", "side",
			"unit_price", "amount", "expires_at", "first_seen", "last_seen", "reporter",
			"raw_location"},
		pgx.CopyFromRows(liveRows)); err != nil {
		return fmt.Errorf("拷入临时表: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO market_order_live
		SELECT DISTINCT ON (order_id) * FROM tmp_live ORDER BY order_id, last_seen DESC
		ON CONFLICT (order_id) DO UPDATE SET
			-- 身份字段也要刷新。order_id 只在单个服务器内唯一,跨服可能重复;
			-- 客户端解析出错时也会撞。只更价格不更物品的话,
			-- 库里会静默出现"T4 的单挂着 T6 的价"这种脏数据,查起来极难。
			item_id     = EXCLUDED.item_id,
			location_id = EXCLUDED.location_id,
			quality     = EXCLUDED.quality,
			enchant     = EXCLUDED.enchant,
			side        = EXCLUDED.side,
			unit_price  = EXCLUDED.unit_price,
			amount      = EXCLUDED.amount,
			expires_at  = EXCLUDED.expires_at,
			last_seen   = GREATEST(market_order_live.last_seen, EXCLUDED.last_seen),
			reporter    = EXCLUDED.reporter,
			raw_location = EXCLUDED.raw_location`); err != nil {
		return fmt.Errorf("合并 live: %w", err)
	}

	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"market_order_event"},
		[]string{"order_id", "observed_at", "item_id", "location_id", "quality", "enchant",
			"side", "unit_price", "amount", "reporter", "raw_location"},
		pgx.CopyFromRows(evtRows)); err != nil {
		return fmt.Errorf("写入 event: %w", err)
	}
	return tx.Commit(ctx)
}

// TouchOrders 只刷新"最后一次看到"。
//
// 状态没变的重复观测走这条路——它不产生历史记录,但让我们知道这张单还活着。
// 挂单从出现到消失的存活时长,是 AODP 给不了的东西。
//
// seen 是每张单各自的观测时间(ingest 已统一到服务端时钟),不是 flush 时刻:
// last_seen 要和 WriteOrders 同一套钟,读簿时"几张单是不是同一眼看到的"
// 才判得准。只前进不后退——乱序到达的旧观测不能把 last_seen 往回拨。
func (s *Store) TouchOrders(ctx context.Context, seen map[int64]time.Time) error {
	if len(seen) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	// 按 id 排序再更新:多实例共用一个库时,两边以同样的顺序加行锁,
	// 不会互相等成死锁
	slices.Sort(ids)
	ts := make([]time.Time, len(ids))
	for i, id := range ids {
		ts[i] = seen[id]
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE market_order_live m SET last_seen = t.seen
		FROM unnest($1::bigint[], $2::timestamptz[]) AS t(order_id, seen)
		WHERE m.order_id = t.order_id AND m.last_seen < t.seen`, ids, ts)
	return err
}

// BookLevel 是订单簿上的一档。
type BookLevel struct {
	Price  int64 `json:"price"`
	Depth  int64 `json:"depth"`
	Orders int32 `json:"orders"`
}

// Book 读某个盘口的挂单深度,价格从优到劣。
//
// 只算 fresh 之内还看得到的挂单:太久没人再看到的多半已经成交或撤单了。
func (s *Store) Book(ctx context.Context, k model.QuoteKey, fresh time.Duration, limit int) ([]BookLevel, error) {
	order := "ASC" // 卖单:价低者优
	if k.Side == model.SideRequest {
		order = "DESC" // 买单:价高者优
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT unit_price, SUM(amount)::BIGINT, COUNT(*)::INT
		FROM market_order_live
		WHERE item_id = $1 AND location_id = $2 AND quality = $3 AND side = $4
		  AND last_seen > $5
		GROUP BY unit_price ORDER BY unit_price %s LIMIT $6`, order),
		k.ItemID, k.LocationID, k.Quality, int16(k.Side), time.Now().Add(-fresh), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []BookLevel
	for rows.Next() {
		var l BookLevel
		if err := rows.Scan(&l.Price, &l.Depth, &l.Orders); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// BestQuotes 批量取多个盘口的最优价,首屏和快照补齐用这个。
func (s *Store) BestQuotes(ctx context.Context, keys []model.QuoteKey, fresh time.Duration) ([]model.Quote, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	items := make([]string, len(keys))
	locs := make([]string, len(keys))
	quals := make([]int16, len(keys))
	sides := make([]int16, len(keys))
	for i, k := range keys {
		items[i], locs[i], quals[i], sides[i] = k.ItemID, k.LocationID, k.Quality, int16(k.Side)
	}

	rows, err := s.pool.Query(ctx, `
		WITH want AS (
			SELECT * FROM unnest($1::text[], $2::text[], $3::smallint[], $4::smallint[])
			         AS t(item_id, location_id, quality, side)
		), best AS (
			SELECT w.item_id, w.location_id, w.quality, w.side,
			       CASE WHEN w.side = 0 THEN MIN(o.unit_price) ELSE MAX(o.unit_price) END AS price
			FROM want w
			JOIN market_order_live o
			  ON o.item_id = w.item_id AND o.location_id = w.location_id
			 AND o.quality = w.quality AND o.side = w.side
			WHERE o.last_seen > $5
			GROUP BY w.item_id, w.location_id, w.quality, w.side
		)
		SELECT b.item_id, b.location_id, b.quality, b.side, b.price,
		       SUM(o.amount)::BIGINT, COUNT(*)::INT, MAX(o.last_seen)
		FROM best b
		JOIN market_order_live o
		  ON o.item_id = b.item_id AND o.location_id = b.location_id
		 AND o.quality = b.quality AND o.side = b.side AND o.unit_price = b.price
		WHERE o.last_seen > $5
		GROUP BY b.item_id, b.location_id, b.quality, b.side, b.price`,
		items, locs, quals, sides, time.Now().Add(-fresh))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Quote
	for rows.Next() {
		var k model.QuoteKey
		var side int16
		var q model.Quote
		if err := rows.Scan(&k.ItemID, &k.LocationID, &k.Quality, &side,
			&q.Price, &q.Depth, &q.Orders, &q.At); err != nil {
			return nil, err
		}
		k.Side = model.Side(side)
		q.Key = k.String()
		out = append(out, q)
	}
	return out, rows.Err()
}

// ItemName 取中文名,没有就回落到英文名再回落到 ID。
func (s *Store) ItemName(ctx context.Context, itemID string) (string, error) {
	var zh, en string
	err := s.pool.QueryRow(ctx,
		`SELECT name_zh, name_en FROM item WHERE item_id = $1`, itemID).Scan(&zh, &en)
	if err != nil {
		return itemID, err
	}
	if zh != "" {
		return zh, nil
	}
	if en != "" {
		return en, nil
	}
	return itemID, nil
}

// WriteDiag 存客户端报上来的诊断记录。
func (s *Store) WriteDiag(ctx context.Context, b model.DiagBatch) error {
	if len(b.Entries) == 0 {
		return nil
	}
	rows := make([][]any, 0, len(b.Entries))
	for _, e := range b.Entries {
		attrs := []byte("null")
		if len(e.Attrs) > 0 {
			if raw, err := json.Marshal(e.Attrs); err == nil {
				attrs = raw
			}
		}
		ts := e.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		rows = append(rows, []any{ts, b.ClientID, b.Character, b.Version, b.OS,
			e.Level, e.Message, attrs})
	}
	_, err := s.pool.CopyFrom(ctx, pgx.Identifier{"client_diag"},
		[]string{"reported_at", "client_id", "character", "version", "os",
			"level", "message", "attrs"},
		pgx.CopyFromRows(rows))
	return err
}

// DiagRow 是查出来的一条诊断记录。
type DiagRow struct {
	ReportedAt time.Time      `json:"reported_at"`
	ClientID   string         `json:"client_id"`
	Character  string         `json:"character"`
	Version    string         `json:"version"`
	OS         string         `json:"os"`
	Level      string         `json:"level"`
	Message    string         `json:"message"`
	Attrs      map[string]any `json:"attrs,omitempty"`
}

// RecentDiag 按时间倒序取最近的诊断,level 为空表示不筛。
func (s *Store) RecentDiag(ctx context.Context, level string, limit int) ([]DiagRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT reported_at, client_id, character, version, os, level, message, attrs
		FROM client_diag
		WHERE ($1 = '' OR level = $1)
		ORDER BY reported_at DESC LIMIT $2`, level, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []DiagRow
	for rows.Next() {
		var d DiagRow
		var attrs []byte
		if err := rows.Scan(&d.ReportedAt, &d.ClientID, &d.Character, &d.Version,
			&d.OS, &d.Level, &d.Message, &attrs); err != nil {
			return nil, err
		}
		if len(attrs) > 0 {
			_ = json.Unmarshal(attrs, &d.Attrs)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
