// Package scan 是扫描编排:拉价 → 拉历史 → 过滤 → 算账 → 排序。
package scan

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/arb"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
	"albion-guild/internal/histagg"
	"albion-guild/internal/model"
	"albion-guild/internal/screen"
)

// CityCoverage 说明某城有多少物品有报价、有多新。
// 亚服的真瓶颈是覆盖率,不是算法。
type CityCoverage struct {
	City      string `json:"city"`
	WithData  int    `json:"with_data"`
	Within2h  int    `json:"within_2h"`
	Within6h  int    `json:"within_6h"`
	Within24h int    `json:"within_24h"`
	// WithinThreshold 是落在当前新鲜度阈值内的报价点数——界面上那根条按这个画。
	WithinThreshold int     `json:"within_threshold"`
	MedianAgeHours  float64 `json:"median_age_hours"`
	HasMedian       bool    `json:"has_median"`

	// 下面四个是抓包口径,和上面的 AODP 口径分开列,不混算:
	// AODP 那几个数回答"众包数据覆盖得怎么样",这几个回答"我们自己翻到了多少"。
	// CaptureSides 是窗口内有抓包挂单的盘口边数,CaptureUsed 是其中融合时
	// 真用上了抓包价的边数,中位数是这些边最优档的数据龄
	CaptureSides          int     `json:"capture_sides"`
	CaptureUsed           int     `json:"capture_used"`
	CaptureMedianAgeHours float64 `json:"capture_median_age_hours"`
	CaptureHasMedian      bool    `json:"capture_has_median"`
}

func coverageByCity(prices []aodp.PriceRecord, cities []string, now time.Time, threshold float64) []CityCoverage {
	ages := map[string][]float64{}
	future := map[string]int{}
	for _, c := range cities {
		ages[c] = nil
	}
	for _, rec := range prices {
		for _, stamp := range []aodp.Stamp{rec.SellPriceMinDate, rec.BuyPriceMaxDate} {
			age, ok := stamp.AgeHours(now)
			if !ok {
				continue
			}
			if age < 0 {
				// 超前的时间戳算"有数据",但不算新鲜,也不进中位数。
				// 以前它会落进每一个 within 桶,钟越离谱覆盖率越好看
				future[rec.City]++
				continue
			}
			ages[rec.City] = append(ages[rec.City], age)
		}
	}

	var out []CityCoverage
	for city, values := range ages {
		c := CityCoverage{City: city, WithData: len(values) + future[city]}
		for _, a := range values {
			if a < 2 {
				c.Within2h++
			}
			if a < 6 {
				c.Within6h++
			}
			if a < 24 {
				c.Within24h++
			}
			if a < threshold {
				c.WithinThreshold++
			}
		}
		c.MedianAgeHours, c.HasMedian = median(values)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].City < out[j].City })
	return out
}

// addCaptureCoverage 把抓包口径的覆盖率填进每城那一行。
func addCaptureCoverage(cov []CityCoverage, books map[model.QuoteKey]CapturedSide,
	sides map[histagg.QualityKey]screen.Sides, now time.Time) {
	if len(books) == 0 {
		return
	}
	ages := map[string][]float64{}
	for k, cs := range books {
		bi := cs.best()
		if bi < 0 {
			continue
		}
		ages[k.LocationID] = append(ages[k.LocationID], math.Max(0, now.Sub(cs.seenAt(bi)).Hours()))
	}
	used := map[string]int{}
	for k, s := range sides {
		for _, side := range []screen.Side{s.Ask, s.Bid} {
			if side.Source == screen.SourceCapture {
				used[k.City]++
			}
		}
	}
	for i := range cov {
		c := &cov[i]
		c.CaptureSides = len(ages[c.City])
		c.CaptureUsed = used[c.City]
		c.CaptureMedianAgeHours, c.CaptureHasMedian = median(ages[c.City])
	}
}

func median(values []float64) (float64, bool) {
	if len(values) == 0 {
		return 0, false
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2], true
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2, true
}

type Result struct {
	StartedAt      time.Time            `json:"started_at"`
	ItemIDs        []string             `json:"item_ids"`
	MissingItemIDs []string             `json:"missing_item_ids"`
	Opportunities  []screen.Opportunity `json:"opportunities"`
	Rejected       []screen.Rejected    `json:"rejected"`
	RequestCount   int                  `json:"request_count"`
	PriceRows      int                  `json:"price_rows"`
	Coverage       []CityCoverage       `json:"coverage"`
	RejectCounts   map[string]int       `json:"reject_counts"`
	// Routes 是跨城套利。同城和跨城是同一批数据算出来的两种玩法,
	// 一次扫描两个都给
	Routes []arb.Route `json:"routes"`
	// Capture 是抓包参与融合的汇总。capture.enabled 关着时是零值
	Capture CaptureSummary `json:"capture"`
	// History 是这次顺带拉回来的成交历史,调用方可以存进库攒长历史。
	History []aodp.HistorySeries `json:"-"`
}

// findRoutes 把同一物品在各城的快照凑成市场,两两配对找跨城路线。
//
// 用的是和同城完全一样的那批价格数据——跨城不需要额外请求,
// 只是换个角度看同一份快照。
func findRoutes(prices []aodp.PriceRecord, stats map[histagg.QualityKey]histagg.Stats,
	cat *catalog.Catalog, cfg conf.Config, now time.Time) []arb.Route {

	type group struct {
		itemID  string
		quality int
	}
	byItem := map[group][]arb.Market{}
	for _, rec := range prices {
		if rec.SellPriceMin <= 0 && rec.BuyPriceMax <= 0 {
			continue
		}
		// 时间戳缺失不能当成"刚刚更新"。同城口径是直接 reject("no_timestamp"),
		// 这里把它记成超龄,后面的新鲜度检查会把它挡掉
		age, ok := freshness(rec, now)
		if !ok {
			continue
		}
		m := arb.Market{
			City:     rec.City,
			Book:     econ.Book{SellMin: rec.SellPriceMin, BuyMax: rec.BuyPriceMax},
			AgeHours: age,
		}
		if s, ok := stats[histagg.QualityKey{
			ItemID: rec.ItemID, City: rec.City, Quality: rec.Quality,
		}]; ok {
			m.Stats = &s
		}
		k := group{rec.ItemID, rec.Quality}
		byItem[k] = append(byItem[k], m)
	}

	opt := arb.DefaultOptions(cfg)
	var out []arb.Route
	for k, markets := range byItem {
		if len(markets) < 2 {
			continue // 只有一个城市有数据,没得比
		}
		out = append(out, arb.Find(k.itemID, cat.NameOf(k.itemID), k.quality,
			markets, cfg.Economics, opt, now)...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DailyProfit > out[j].DailyProfit })
	return out
}

// freshness 返回这条报价里更旧的那一侧有多旧。
// 任何一侧缺时间戳就判不可用——价格有值但不知道什么时候的,
// 比没有价格更危险。任何一侧的数据龄为负(时间戳超前)也判不可用:
// 同城那边是 future_timestamp 拒绝,跨城以前会把它当成"刚刚更新"
func freshness(rec aodp.PriceRecord, now time.Time) (float64, bool) {
	sell, okSell := rec.SellPriceMinDate.AgeHours(now)
	buy, okBuy := rec.BuyPriceMaxDate.AgeHours(now)
	if (okSell && sell < 0) || (okBuy && buy < 0) {
		return 0, false
	}
	switch {
	case okSell && okBuy:
		return math.Max(sell, buy), true
	case okSell && rec.BuyPriceMax == 0:
		return sell, true // 这一侧本来就没挂单,不算缺时间戳
	case okBuy && rec.SellPriceMin == 0:
		return buy, true
	default:
		return 0, false
	}
}

func (r Result) ElapsedNote() string {
	return fmt.Sprintf("%d 次请求,%d 条报价", r.RequestCount, r.PriceRows)
}

// Run 跑一次纯 AODP 的完整扫描。client 由调用方传进来,测试就能指向假服务器。
func Run(ctx context.Context, client *aodp.Client, cfg conf.Config,
	cat *catalog.Catalog, now time.Time) (*Result, error) {
	return RunWithCapture(ctx, client, cfg, cat, nil, now)
}

// RunWithCapture 跑一次完整扫描,capture.enabled 打开且 books 非 nil 时
// 把自建抓包的盘口逐边融合进来。books 为 nil 或开关关着时和 Run 逐字相同。
func RunWithCapture(ctx context.Context, client *aodp.Client, cfg conf.Config,
	cat *catalog.Catalog, books BookSource, now time.Time) (*Result, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	// 请求数取本次扫描的增量。client 是全服务共用的(限流状态在它身上),
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
		return nil, fmt.Errorf("展开后没有任何有效物品 ID,检查 items.patterns")
	}

	slog.Info("拉取当前挂单价", "items", len(itemIDs), "cities", len(cfg.Cities))
	prices, err := client.FetchPrices(ctx, itemIDs, cfg.Cities, cfg.Qualities)
	if err != nil {
		return nil, fmt.Errorf("拉价格: %w", err)
	}

	slog.Info("拉取成交历史", "days", cfg.Sizing.HistoryDays)
	history, err := client.FetchHistory(ctx, itemIDs, cfg.Cities, cfg.Qualities,
		cfg.Sizing.HistoryDays, 24)
	if err != nil {
		return nil, fmt.Errorf("拉历史: %w", err)
	}

	// 必须按品质分开。合并的话,qualities 填成 [1,2,3] 时同一份成交量
	// 会被三档各领一次,资金池里三行各分一份钱,一天买走三倍于实际
	// 容量的货;偏离度基准也被混合品质的均价带偏
	stats := histagg.AggregateByQuality(history, now, cfg.Sizing.BaselineDays, cfg.Sizing.HistoryDays)

	res := evaluate(ctx, prices, stats, itemIDs, cfg, cat, books, now)
	res.MissingItemIDs = missing
	res.RequestCount = client.Requests() - before
	res.History = history
	return res, nil
}

// evaluate 是扫描里不打 AODP 的那一半:读抓包盘口、逐边融合、过滤、算账、排序。
//
// 读簿失败不让整次扫描白跑:报进 Capture.Error,退回纯 AODP 继续。
// 融合之后同城(screen)和跨城(findRoutes)用的是同一份价格;
// 覆盖率仍按 AODP 原始价格算,抓包口径另列。
func evaluate(ctx context.Context, raw []aodp.PriceRecord, stats map[histagg.QualityKey]histagg.Stats,
	itemIDs []string, cfg conf.Config, cat *catalog.Catalog, books BookSource, now time.Time) *Result {

	prices := raw
	var sides map[histagg.QualityKey]screen.Sides
	var got map[model.QuoteKey]CapturedSide
	var sum CaptureSummary
	if cfg.Capture.Enabled && books != nil {
		keys := captureKeys(itemIDs, cfg.Cities, cfg.Qualities)
		var err error
		got, err = books.CaptureBooks(ctx, keys, now.Add(-cfg.CaptureWindow()))
		if err != nil {
			slog.Warn("读抓包盘口失败,这次扫描退回纯 AODP", "err", err)
			got = nil
		}
		prices, sides, sum = overlay(raw, got, cfg, now)
		sum.Enabled, sum.RequestedKeys = true, len(keys)
		if err != nil {
			sum.Error = err.Error()
		}
	}

	var opportunities []screen.Opportunity
	var rejected []screen.Rejected
	counts := map[string]int{}
	for _, rec := range prices {
		qk := histagg.QualityKey{ItemID: rec.ItemID, City: rec.City, Quality: rec.Quality}
		var st *histagg.Stats
		if s, ok := stats[qk]; ok {
			st = &s
		}
		opp, rej := screen.EvaluateSides(rec, sides[qk], st, cfg, cat, now)
		if opp != nil {
			opportunities = append(opportunities, *opp)
		} else {
			rejected = append(rejected, *rej)
			counts[rej.Reason]++
		}
	}

	// 排序主键是日化**绝对**收益,不是利润率。
	// 利润率 30% 但一天只能做 3 件,不如利润率 5% 但一天能做 2000 件。
	sort.SliceStable(opportunities, func(i, j int) bool {
		return opportunities[i].DailyProfit > opportunities[j].DailyProfit
	})

	// 跨城这一步只吃到融合后的价格,两端深度要到 arb 按腿接上之后才用得上
	routes := findRoutes(prices, stats, cat, cfg, now)

	cov := coverageByCity(raw, cfg.Cities, now, cfg.Freshness.MaxHours)
	addCaptureCoverage(cov, got, sides, now)

	return &Result{
		StartedAt:     now,
		ItemIDs:       itemIDs,
		Opportunities: opportunities,
		Rejected:      rejected,
		PriceRows:     len(raw),
		Coverage:      cov,
		RejectCounts:  counts,
		Routes:        routes,
		Capture:       sum,
	}
}
