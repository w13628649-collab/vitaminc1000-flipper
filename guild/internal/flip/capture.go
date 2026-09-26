package flip

import (
	"context"
	"sort"
	"time"

	"albion-guild/internal/book"
	"albion-guild/internal/model"
	"albion-guild/internal/scan"
	"albion-guild/internal/store"
)

// ConflictSource 报出最近卷进过多开串城的盘口。线上就是 *ingest.Ingestor;
// 用接口是为了 flip 不依赖 ingest,测试也能直接喂。
type ConflictSource interface {
	ConflictedSince(keys []model.QuoteKey, since time.Time) map[model.QuoteKey]time.Time
}

// captureReader 是读抓包那两步,线上是 *store.Store。
type captureReader interface {
	BookOrders(ctx context.Context, keys []model.QuoteKey, since time.Time) ([]store.LiveOrder, error)
	CapturedItems(ctx context.Context, cities []string, qualities []int,
		since time.Time) ([]store.CapturedItem, error)
}

// rounds 是整理盘口边(book.Build)用的 slack 口径。slack 和 window 一律来自
// capture.snapshot_slack_seconds 和扫描的抓包窗口(conf.Config.CaptureWindow):
// 扫描、WS 报价(storeBooks)和查价页(buildGrid / buildBook)对"哪几张单算同一眼"
// 的说法因此一致。
//
// 最近卷进过多开串城的盘口边 slack 放到 window + slack,等于暂停幽灵剔除。
// 幽灵规则把"最近一眼"当权威,而错归的单恰恰带着最新的时间戳,会成为被串入那座城的
// 最近一眼,把那座城上一眼的真实挂单当幽灵剔掉。暂停剔除的代价是可能留几张已成交的
// 旧单,比整段真实盘口凭空消失要轻。
//
// 窗口再加一个 slack:newest 可能因为钟快略超前于调用方的 now,只放到窗口大小的话,
// 窗口最早那几张单仍可能被判成幽灵。window 一律是扫描的抓包窗口:报价的窗口
// (-fresh,默认 30m)比它小,放到扫描窗口只会更宽,同样等于不剔
type rounds struct {
	slack      time.Duration
	window     time.Duration
	conflicted map[model.QuoteKey]time.Time
}

// slackOf 是一个盘口边该用的 slack,第二个返回值说明它是不是因为串城放宽的。
func (r rounds) slackOf(k model.QuoteKey) (time.Duration, bool) {
	if _, bad := r.conflicted[k]; bad {
		return r.window + r.slack, true
	}
	return r.slack, false
}

// conflictsOf 问 src 这批盘口边里哪些 since 之后串过城。src 为 nil(没接 ingest)时不做串城处理。
func conflictsOf(src ConflictSource, keys []model.QuoteKey, since time.Time) map[model.QuoteKey]time.Time {
	if src == nil || len(keys) == 0 {
		return nil
	}
	return src.ConflictedSince(keys, since)
}

// storeBooks 把 store 的两个抓包查询包成 scan.BookSource。
type storeBooks struct {
	st        captureReader
	conflicts ConflictSource // 可以为 nil:没接 ingest 时就不做串城处理
	slack     time.Duration  // "同一眼"的宽容度
	window    time.Duration  // 抓包窗口;串城的 key 把 slack 放到这么大
	levels    int
}

// CaptureBooks 读一批盘口边,转成扫描用的形状。
func (b *storeBooks) CaptureBooks(ctx context.Context, keys []model.QuoteKey,
	since time.Time) (map[model.QuoteKey]scan.CapturedSide, error) {

	got, flagged, err := b.readSides(ctx, keys, since)
	if err != nil {
		return nil, err
	}
	out := make(map[model.QuoteKey]scan.CapturedSide, len(got))
	for k, side := range got {
		cs := scan.CapturedFrom(side, b.levels)
		cs.Conflicted = flagged[k]
		out[k] = cs
	}
	return out, nil
}

// readSides 一次查询读出一批盘口边的原始挂单,用 book.Build 整理。扫描(CaptureBooks)
// 和报价(BestQuotes)共用这一处,查价页(buildGrid / buildBook)用的也是同一个
// book.Build 和同一份 rounds 口径,几处对"盘口现在长什么样"的说法才一致。
//
// 请求了但窗口内没有挂单的 key 不在结果里。串城的 key 放宽 slack(见 rounds),
// 第二个返回值标出它们。
func (b *storeBooks) readSides(ctx context.Context, keys []model.QuoteKey,
	since time.Time) (map[model.QuoteKey]book.Side, map[model.QuoteKey]bool, error) {

	out := make(map[model.QuoteKey]book.Side)
	flagged := make(map[model.QuoteKey]bool)
	if len(keys) == 0 {
		return out, flagged, nil
	}
	r := rounds{slack: b.slack, window: b.window, conflicted: conflictsOf(b.conflicts, keys, since)}
	orders, err := b.st.BookOrders(ctx, keys, since)
	if err != nil {
		return nil, nil, err
	}
	want := make(map[model.QuoteKey]struct{}, len(keys))
	for _, k := range keys {
		want[k] = struct{}{}
	}
	for g, group := range book.Group(orders) {
		k := model.QuoteKey{ItemID: g.ItemID, LocationID: g.City, Quality: int16(g.Quality), Side: g.Side}
		if _, ok := want[k]; !ok {
			continue // 按品质集合过滤时顺带回来的别的 key(见 store.bookOrdersSQL)
		}
		slack, conflict := r.slackOf(k)
		side := book.Build(group, k.Side, slack)
		if len(side.Levels) == 0 {
			continue
		}
		out[k] = side
		if conflict {
			flagged[k] = true
		}
	}
	return out, flagged, nil
}

// bestQuotes 是每个盘口边整理之后的第一档。
//
// At 取这一边的"最近一眼"(newest),不取最优档自己的 last_seen:最优单被买走、
// 退到次优档时,次优档的 last_seen 往往比旧最优档早,hub.Fanout 的乱序闸门
// (q.At 早于上一条就丢)会把这条合法更新当成旧消息吞掉。newest 只进不退。
//
// 续页的情形(book.Side.PrevPage > 0)第一档来自更早那一页,价和查价页、扫描一致。
func (b *storeBooks) bestQuotes(ctx context.Context, keys []model.QuoteKey, since time.Time) ([]model.Quote, error) {
	got, _, err := b.readSides(ctx, keys, since)
	if err != nil {
		return nil, err
	}
	out := make([]model.Quote, 0, len(got))
	for k, side := range got {
		l := side.Levels[0]
		out = append(out, model.Quote{Key: k.String(), Price: l.Price, Depth: l.Qty,
			Orders: int32(l.Orders), At: side.Newest})
	}
	// 按 key 排:同一批报价每次顺序一样,测试和排查都好对
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// CapturedItems 列出抓包窗口里有挂单的物品,给扫描扩物品集。
func (b *storeBooks) CapturedItems(ctx context.Context, cities []string, qualities []int,
	since time.Time) ([]scan.CapturedItem, error) {
	rows, err := b.st.CapturedItems(ctx, cities, qualities, since)
	if err != nil {
		return nil, err
	}
	out := make([]scan.CapturedItem, len(rows))
	for i, r := range rows {
		out[i] = scan.CapturedItem{ItemID: r.ItemID, Qty: r.Qty}
	}
	return out, nil
}

// books 是扫描用的读簿来源。没有库(测试里)时是 nil,扫描退回纯 AODP;
// 开关关着时照样返回——要不要融合由 scan 按 capture.enabled 决定,口径只在一处
func (s *Service) books() scan.BookSource {
	if s.bookSrc != nil {
		return s.bookSrc
	}
	if b := s.storeBooks(); b != nil {
		return b
	}
	return nil // 不能直接返回 nil 的 *storeBooks:那是装着 nil 指针的非 nil 接口
}

// storeBooks 是库上的读簿器,扫描和报价共用。没有库时是 nil。
func (s *Service) storeBooks() *storeBooks {
	var st captureReader
	switch {
	case s.ladder != nil:
		st = s.ladder
	case s.Store != nil:
		st = s.Store
	default:
		return nil
	}
	return &storeBooks{
		st:        st,
		conflicts: s.Conflicts,
		slack:     s.Cfg.SnapshotSlack(),
		window:    s.Cfg.CaptureWindow(),
		levels:    s.Cfg.Capture.BookLevels,
	}
}

// BestQuotes 是 WS 报价推送(hub.Conflator)和 /api/quotes 的最优价来源,实现 hub.QuoteSource。
//
// 和扫描读簿同一套口径:底下就是 readSides 整理之后取第一档,残单剔除、续页、
// 串城盘口暂停剔除都一样。以前走的是一条只按 last_seen > now−fresh 过滤的 SQL:
// 已经被买走的最优单在行情推送和 /api/quotes 里一直挂到 30 分钟过期,同一个盘口在
// 实时页上是一个价、在机会页上是另一个价。
//
// 窗口仍是 fresh(服务端 -fresh,默认 30m),不是扫描的 6h:实时页只要"现在还看得到"的。
// 没有库时返回空。
func (s *Service) BestQuotes(ctx context.Context, keys []model.QuoteKey, fresh time.Duration) ([]model.Quote, error) {
	b := s.storeBooks()
	if b == nil || len(keys) == 0 {
		return nil, nil
	}
	return b.bestQuotes(ctx, keys, time.Now().Add(-fresh))
}
