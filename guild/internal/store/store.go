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

// Flush 把 ingest 一轮攒下的东西在**一个事务**里写完:状态变了的单
// (按上报人分组)写 live 和 event 两张表,状态没变的单只推 last_seen。
//
// 为什么必须是一个事务:读簿(BookOrders / ItemOrders 读出来交给 book.Build)
// 按 last_seen 判断哪些单是"最近一眼"看到的。同一眼里变了的单走 upsert、没变的单走 touch,以前两路分开提交,
// 中间被读到的话,变了的那几张已经把 newest 推到这一眼,没变的还停在上一眼,
// 幽灵规则会把段内这些真实挂单全判成已成交,盘口最优那几档凭空消失。
// 合成一个事务后,READ COMMITTED 下读簿是单语句快照:要么看到整眼,要么一点都看不到。
func (s *Store) Flush(ctx context.Context, byReporter map[string][]model.MarketOrder,
	touches map[int64]time.Time) error {
	n := 0
	for _, orders := range byReporter {
		n += len(orders)
	}
	if n == 0 && len(touches) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if n > 0 {
		if err := writeOrders(ctx, tx, byReporter, n); err != nil {
			return err
		}
	}
	// 顺序不能反:同一张单可能这一轮先新写入、又被 touch 过,
	// 先 upsert 行才存在,touch 才有东西可刷
	if len(touches) > 0 {
		if err := touchOrders(ctx, tx, touches); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// WriteOrders 把一个上报人的一批**状态确实变了**的挂单写进两张表。
//
// 调用方(ingest)负责去重,这里不再判断——数据库层面挡重复做不到,
// 因为 hypertable 的唯一约束必须包含时间列,
// ON CONFLICT (order_id, unit_price, amount) 根本建不出来。
// 线上入库走 Flush;这个入口留给只写不 touch 的场合(测试夹具)。
func (s *Store) WriteOrders(ctx context.Context, reporter string, orders []model.MarketOrder) error {
	return s.Flush(ctx, map[string][]model.MarketOrder{reporter: orders}, nil)
}

// TouchOrders 只刷新"最后一次看到",单独提交。线上入库走 Flush。
func (s *Store) TouchOrders(ctx context.Context, seen map[int64]time.Time) error {
	return s.Flush(ctx, nil, seen)
}

func writeOrders(ctx context.Context, tx pgx.Tx, byReporter map[string][]model.MarketOrder, n int) error {
	// 上报人排个序,写入顺序和 map 遍历无关。谁的那条落进当前态由下面的
	// DISTINCT ON 按观测时间决定,不看写入先后
	reporters := make([]string, 0, len(byReporter))
	for r := range byReporter {
		reporters = append(reporters, r)
	}
	slices.Sort(reporters)

	// ① 当前态:同一张单反复上报就更新价格/数量/last_seen
	liveRows := make([][]any, 0, n)
	// ② 历史流:纯 append
	evtRows := make([][]any, 0, n)
	for _, reporter := range reporters {
		for _, o := range byReporter[reporter] {
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
	}

	// live 表没法直接 CopyFrom(要 upsert),走临时表再 merge。
	// tmp_live 是 ON COMMIT DROP,一个事务里只能建一次——所以各上报人合成一次写,
	// 不能按人各调一遍
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
			last_seen   = EXCLUDED.last_seen,
			reporter    = EXCLUDED.reporter,
			raw_location = EXCLUDED.raw_location
		-- 只接受不比库里旧的观测。乱序晚到的旧观测(断网重传、两个成员先后上传、
		-- 服务重启后 LRU 是空的)状态和当前不同时也会走到这里,不挡的话新状态
		-- 被盖回旧状态、last_seen 却还是新的,读簿会把"旧件数"当成最近一眼。
		-- 同一时刻的两份观测谁对谁错判断不了,按后到的算,不能因此卡死。
		-- 旧观测照样进下面的 event 表:observed_at 是真实时间,历史不受影响
		WHERE market_order_live.last_seen <= EXCLUDED.last_seen`); err != nil {
		return fmt.Errorf("合并 live: %w", err)
	}

	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"market_order_event"},
		[]string{"order_id", "observed_at", "item_id", "location_id", "quality", "enchant",
			"side", "unit_price", "amount", "reporter", "raw_location"},
		pgx.CopyFromRows(evtRows)); err != nil {
		return fmt.Errorf("写入 event: %w", err)
	}
	return nil
}

// touchOrders 只刷新"最后一次看到"。
//
// 状态没变的重复观测走这条路——它不产生历史记录,但让我们知道这张单还活着。
// 挂单从出现到消失的存活时长,是 AODP 给不了的东西。
//
// seen 是每张单各自的观测时间(ingest 已统一到服务端时钟),不是 flush 时刻:
// last_seen 要和 upsert 同一套钟,读簿时"几张单是不是同一眼看到的"
// 才判得准。只前进不后退——乱序到达的旧观测不能把 last_seen 往回拨。
func touchOrders(ctx context.Context, tx pgx.Tx, seen map[int64]time.Time) error {
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	// 按 id 排序再更新:多实例共用一个库时,两边以同样的顺序加行锁,
	// 不容易互相等成死锁(真撞上了 PG 会回滚其中一个,ingest 那边按写失败善后)
	slices.Sort(ids)
	ts := make([]time.Time, len(ids))
	for i, id := range ids {
		ts[i] = seen[id]
	}
	if _, err := tx.Exec(ctx, `
		UPDATE market_order_live m SET last_seen = t.seen
		FROM unnest($1::bigint[], $2::timestamptz[]) AS t(order_id, seen)
		WHERE m.order_id = t.order_id AND m.last_seen < t.seen`, ids, ts); err != nil {
		return fmt.Errorf("刷新 last_seen: %w", err)
	}
	return nil
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

// 批量最优价(WS 推送和 /api/quotes)不在这里:以前这里有一条只按
// last_seen > now−fresh 过滤的 BestQuotes,没有幽灵剔除,已经被买走的最优单会一直
// 挂到过期。现在由 flip.Service.BestQuotes 读 BookOrders、经 book.Build 整理后取第一档,
// 和扫描、查价页同一套残单规则。

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
