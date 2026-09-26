package scan

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/histagg"
)

// Snapshot 是一次 AODP 全量拉取的结果:价格、按品质聚合好的成交统计、物品集。
//
// 扫描拆成"拉 AODP"和"评估"两半,就是为了两次全量之间能拿同一份快照、
// 配上最新的抓包盘口反复重算,而不再打 AODP——AODP 有配额,抓包没有。
// 发布出去之后只读:补拉新物品走 With 生成新的一份,不原地改,
// 正在用旧快照评估的那一轮不受影响。
type Snapshot struct {
	// FetchedAt 是这份快照的 AODP 全量时刻,也就是 Result.StartedAt
	FetchedAt time.Time
	// ItemIDs 是拉过 AODP 的全部物品:配置清单展开的 ∪ 抓包并进来的 ∪ 之后补拉的
	ItemIDs []string
	// ExtraItemIDs 是其中因为抓包才进来的(含补拉的)
	ExtraItemIDs   []string
	MissingItemIDs []string
	// ExtraDropped 是全量那次因为 capture.max_extra_items 截掉的物品数
	ExtraDropped int
	Prices       []aodp.PriceRecord
	Stats        map[histagg.QualityKey]histagg.Stats
	// RequestCount 是构建这份快照累计打了多少次 AODP(全量加补拉)
	RequestCount int
	// BackfilledAt 是最近一次补拉的时刻,没补过是零值
	BackfilledAt time.Time

	// itemsErr 是全量那次列抓包物品的错误。快照的物品集因此缺了抓包物品,
	// 之后每次用这份快照评估都要报出来
	itemsErr error
}

// FetchSnapshot 是扫描打 AODP 的那一半:展开物品清单、并入抓包物品、拉价和历史、聚合。
func FetchSnapshot(ctx context.Context, client *aodp.Client, cfg conf.Config,
	cat *catalog.Catalog, books BookSource, now time.Time) (*Snapshot, []aodp.HistorySeries, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	// 请求数取本次的增量。client 是全服务共用的(限流状态在它身上),
	// 累计值里混着查价页打出去的请求,报成"这次扫描发了几次"是错的
	before := client.Requests()
	itemIDs, missing := cat.Resolve(cfg.Items.Patterns, cfg.Items.Exclude)
	if len(missing) > 0 {
		head := missing
		if len(head) > 10 {
			head = head[:10]
		}
		slog.Warn("配置里有 ID 在目录中不存在", "count", len(missing), "sample", head)
	}
	if len(itemIDs) == 0 {
		return nil, nil, fmt.Errorf("展开后没有任何有效物品 ID,检查 items.patterns")
	}

	// 配置清单外、成员翻市场翻到的物品也并进来。它们和清单里的物品塞进同一批
	// URL 拉 AODP——troll 过滤要 7 日均价、可吃量要成交量,只有抓包价是判不了的
	var extra []string
	var dropped int
	captured, itemsErr := ListCaptured(ctx, cfg, books, now)
	if itemsErr != nil {
		slog.Warn("列抓包物品失败,这次只扫配置清单", "err", itemsErr)
	} else {
		var unknown int
		extra, dropped, unknown = extraItems(captured, itemIDs, cat, cfg.Capture.MaxExtraItems)
		if unknown > 0 {
			slog.Info("抓到的物品里有目录查不到的 id,跳过", "count", unknown)
		}
		if dropped > 0 {
			slog.Warn("抓到的物品超过 capture.max_extra_items,按件数截断",
				"kept", len(extra), "dropped", dropped, "limit", cfg.Capture.MaxExtraItems)
		}
	}
	all := append(itemIDs[:len(itemIDs):len(itemIDs)], extra...)

	slog.Info("拉取当前挂单价", "items", len(all), "extra", len(extra), "cities", len(cfg.Cities))
	prices, history, stats, err := fetchAODP(ctx, client, cfg, all, now)
	if err != nil {
		return nil, nil, err
	}
	return &Snapshot{
		FetchedAt:      now,
		ItemIDs:        all,
		ExtraItemIDs:   extra,
		MissingItemIDs: missing,
		ExtraDropped:   dropped,
		Prices:         prices,
		Stats:          stats,
		RequestCount:   client.Requests() - before,
		itemsErr:       itemsErr,
	}, history, nil
}

func fetchAODP(ctx context.Context, client *aodp.Client, cfg conf.Config, itemIDs []string,
	now time.Time) ([]aodp.PriceRecord, []aodp.HistorySeries, map[histagg.QualityKey]histagg.Stats, error) {
	prices, err := client.FetchPrices(ctx, itemIDs, cfg.Cities, cfg.Qualities)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("拉价格: %w", err)
	}
	slog.Info("拉取成交历史", "items", len(itemIDs), "days", cfg.Sizing.HistoryDays)
	history, err := client.FetchHistory(ctx, itemIDs, cfg.Cities, cfg.Qualities,
		cfg.Sizing.HistoryDays, 24)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("拉历史: %w", err)
	}
	// 必须按品质分开。合并的话,qualities 填成 [1,2,3] 时同一份成交量
	// 会被三档各领一次,资金池里三行各分一份钱,一天买走三倍于实际
	// 容量的货;偏离度基准也被混合品质的均价带偏
	stats := histagg.AggregateByQuality(history, now, cfg.Sizing.BaselineDays, cfg.Sizing.HistoryDays)
	return prices, history, stats, nil
}

// ListCaptured 按配置列抓包窗口里有挂单的物品。开关关着、没有来源、
// capture.max_extra_items 为 0 时返回 nil,也不查库。
func ListCaptured(ctx context.Context, cfg conf.Config, books BookSource, now time.Time) ([]CapturedItem, error) {
	if !cfg.Capture.Enabled || books == nil || cfg.Capture.MaxExtraItems <= 0 {
		return nil, nil
	}
	items, err := books.CapturedItems(ctx, cfg.Cities, cfg.Qualities, now.Add(-cfg.CaptureWindow()))
	if err != nil {
		return nil, fmt.Errorf("列抓包物品: %w", err)
	}
	return items, nil
}

// PendingExtras 是抓到了、该并进快照、却还没拉过 AODP 的物品,已按剩余名额截断:
// 快照里抓包物品的总数(含补拉的)不超过 capture.max_extra_items。
func (s *Snapshot) PendingExtras(captured []CapturedItem, cfg conf.Config, cat *catalog.Catalog) []string {
	if s == nil || !cfg.Capture.Enabled {
		return nil
	}
	room := cfg.Capture.MaxExtraItems - len(s.ExtraItemIDs)
	if room <= 0 {
		return nil
	}
	pending, _, _ := extraItems(captured, s.ItemIDs, cat, room)
	return pending
}

// Extension 是给快照补拉的一批物品的 AODP 数据。
type Extension struct {
	FetchedAt    time.Time
	ItemIDs      []string
	Prices       []aodp.PriceRecord
	Stats        map[histagg.QualityKey]histagg.Stats
	RequestCount int
}

// FetchExtension 给一批物品补拉 AODP 价格和历史。和全量走同一个 client,
// 批量和限流都照旧。
func FetchExtension(ctx context.Context, client *aodp.Client, cfg conf.Config, itemIDs []string,
	now time.Time) (*Extension, []aodp.HistorySeries, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	before := client.Requests()
	prices, history, stats, err := fetchAODP(ctx, client, cfg, itemIDs, now)
	if err != nil {
		return nil, nil, err
	}
	return &Extension{
		FetchedAt:    now,
		ItemIDs:      append([]string(nil), itemIDs...),
		Prices:       prices,
		Stats:        stats,
		RequestCount: client.Requests() - before,
	}, history, nil
}

// With 返回并进了补拉数据的新快照,s 本身不动。
//
// 快照里已经有的物品一律跳过:补拉期间可能刚好跑完一次全量,新快照已经带上
// 了这些物品,而且是更新的数据,不能被补拉的旧一份盖掉或者重复一行。
func (s *Snapshot) With(ext *Extension) *Snapshot {
	if ext == nil {
		return s
	}
	have := make(map[string]bool, len(s.ItemIDs))
	for _, id := range s.ItemIDs {
		have[id] = true
	}
	var newIDs []string
	added := map[string]bool{}
	for _, id := range ext.ItemIDs {
		if !have[id] && !added[id] {
			added[id] = true
			newIDs = append(newIDs, id)
		}
	}
	if len(newIDs) == 0 {
		return s
	}

	n := *s
	n.ItemIDs = append(append([]string(nil), s.ItemIDs...), newIDs...)
	n.ExtraItemIDs = append(append([]string(nil), s.ExtraItemIDs...), newIDs...)
	n.Prices = append([]aodp.PriceRecord(nil), s.Prices...)
	for _, p := range ext.Prices {
		if added[p.ItemID] {
			n.Prices = append(n.Prices, p)
		}
	}
	n.Stats = make(map[histagg.QualityKey]histagg.Stats, len(s.Stats)+len(ext.Stats))
	for k, v := range s.Stats {
		n.Stats[k] = v
	}
	for k, v := range ext.Stats {
		if added[k.ItemID] {
			n.Stats[k] = v
		}
	}
	n.RequestCount = s.RequestCount + ext.RequestCount
	n.BackfilledAt = ext.FetchedAt
	return &n
}

// NewerPrice 逐字段合并同一格(物品 × 城市 × 品质)的两条 AODP 当前价:四个价
// (卖单最低 / 最高、买单最低 / 最高)各看各的时间戳,add 那条更新就用 add 的。
// 第二个返回值说明有没有哪个字段换了。
//
// 只收有价、有时间戳的字段:AODP 对没数据的组合回 0 价、零时间,那不是"现在没有挂单",
// 不能拿它盖掉旧的真实报价。逐字段而不是整条比,是因为同一条记录里四个价各自更新,
// 一条记录的卖价新、买价可能反而旧。
func NewerPrice(base, add aodp.PriceRecord) (aodp.PriceRecord, bool) {
	changed := false
	take := func(px *int64, at *aodp.Stamp, npx int64, nat aodp.Stamp) {
		if npx <= 0 || !nat.Valid() {
			return
		}
		if at.Valid() && !nat.T.After(at.T) {
			return
		}
		*px, *at = npx, nat
		changed = true
	}
	take(&base.SellPriceMin, &base.SellPriceMinDate, add.SellPriceMin, add.SellPriceMinDate)
	take(&base.SellPriceMax, &base.SellPriceMaxDate, add.SellPriceMax, add.SellPriceMaxDate)
	take(&base.BuyPriceMin, &base.BuyPriceMinDate, add.BuyPriceMin, add.BuyPriceMinDate)
	take(&base.BuyPriceMax, &base.BuyPriceMaxDate, add.BuyPriceMax, add.BuyPriceMaxDate)
	return base, changed
}

// WithPrices 返回一份用 recs 逐字段更新过 AODP 当前价的快照(NewerPrice),s 本身不动。
// 只更新快照里已经有的格子,不添格子:快照的物品、城市、品质集合由全量和补拉决定,
// 这里只是让已有的格子用上别处(查价页)现取回来的更新的价。没有可换的就原样返回 s。
func (s *Snapshot) WithPrices(recs []aodp.PriceRecord) *Snapshot {
	if s == nil || len(recs) == 0 {
		return s
	}
	type key struct {
		item, city string
		quality    int
	}
	newer := make(map[key]aodp.PriceRecord, len(recs))
	for _, r := range recs {
		k := key{r.ItemID, r.City, r.Quality}
		if cur, ok := newer[k]; ok {
			r, _ = NewerPrice(cur, r)
		}
		newer[k] = r
	}
	var prices []aodp.PriceRecord
	for i, p := range s.Prices {
		add, ok := newer[key{p.ItemID, p.City, p.Quality}]
		if !ok {
			continue
		}
		merged, changed := NewerPrice(p, add)
		if !changed {
			continue
		}
		if prices == nil {
			prices = append([]aodp.PriceRecord(nil), s.Prices...)
		}
		prices[i] = merged
	}
	if prices == nil {
		return s
	}
	n := *s
	n.Prices = prices
	return &n
}

// EvaluateSnapshot 是扫描不打 AODP 的那一半:用快照里的 AODP 数据,配上 now 时刻的
// 抓包盘口,重跑融合、过滤、算账和跨城配对。
//
// StartedAt 报快照的 AODP 全量时刻,EvaluatedAt 报这次评估的时刻——两次全量之间
// 重算过的结果,AODP 那部分并没有变新,界面不能把它说成"刚扫过"。
func EvaluateSnapshot(ctx context.Context, snap *Snapshot, cfg conf.Config, cat *catalog.Catalog,
	books BookSource, now time.Time) *Result {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	res := evaluate(ctx, snap.Prices, snap.Stats, snap.ItemIDs, cfg, cat, books, now)
	res.StartedAt = snap.FetchedAt
	res.EvaluatedAt = now
	res.MissingItemIDs = snap.MissingItemIDs
	res.ExtraItemIDs = snap.ExtraItemIDs
	res.RequestCount = snap.RequestCount
	res.Capture.ExtraItems = len(snap.ExtraItemIDs)
	res.Capture.ExtraDropped = snap.ExtraDropped
	res.Capture.BackfilledAt = snap.BackfilledAt
	res.Capture.AddError(snap.itemsErr)
	return res
}
