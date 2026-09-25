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
	// StartedAt 是这份结果所用 AODP 快照的全量拉取时刻
	StartedAt time.Time `json:"started_at"`
	// EvaluatedAt 是最近一次评估(融合抓包、过滤、算账)的时刻。两次全量之间
	// 会用缓存的 AODP 快照配最新抓包重算,这时它比 StartedAt 新
	EvaluatedAt time.Time `json:"evaluated_at"`
	// ItemIDs 是这次扫描的全部物品:配置清单展开的,加上抓包并进来的
	ItemIDs []string `json:"item_ids"`
	// ExtraItemIDs 是 ItemIDs 里因为抓包窗口里有挂单才并进来的那部分
	ExtraItemIDs   []string             `json:"extra_item_ids"`
	MissingItemIDs []string             `json:"missing_item_ids"`
	Opportunities  []screen.Opportunity `json:"opportunities"`
	Rejected       []screen.Rejected    `json:"rejected"`
	// RequestCount 是构建这份 AODP 快照打了多少次请求(全量加补拉);重算不打 AODP
	RequestCount int            `json:"request_count"`
	PriceRows    int            `json:"price_rows"`
	Coverage     []CityCoverage `json:"coverage"`
	RejectCounts map[string]int `json:"reject_counts"`
	// Routes 是跨城套利。同城和跨城是同一批数据算出来的两种玩法,
	// 一次扫描两个都给
	Routes []arb.Route `json:"routes"`
	// Capture 是抓包参与融合的汇总。capture.enabled 关着时是零值
	Capture CaptureSummary `json:"capture"`
	// Digest 是这份结果除 evaluated_at 和数据龄之外全部内容的摘要(见 Digest),
	// 对外发布时由 flip 填,和 WS scan 通知里的 digest 是同一个值:界面拉完 /api/scan
	// 记下它,之后收到的通知摘要相同就不用重拉。没发布过的结果(测试里直接调 Run)为空
	Digest string `json:"digest,omitempty"`
	// History 是这次顺带拉回来的成交历史,调用方可以存进库攒长历史。
	History []aodp.HistorySeries `json:"-"`
}

// findRoutes 把同一物品在各城的快照凑成市场,两两配对找跨城路线。
//
// 用的是和同城完全一样的那批价格数据——跨城不需要额外请求,
// 只是换个角度看同一份快照。sides 是融合层给的逐边来源和深度,
// 和同城走同一个 screen.ResolveSides,两边对"这一边来自谁"的说法一致;
// 为 nil 时就是纯 AODP。
func findRoutes(prices []aodp.PriceRecord, stats map[histagg.QualityKey]histagg.Stats,
	sides map[histagg.QualityKey]screen.Sides, cat *catalog.Catalog, cfg conf.Config, now time.Time) []arb.Route {

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
		qk := histagg.QualityKey{ItemID: rec.ItemID, City: rec.City, Quality: rec.Quality}
		sd := screen.ResolveSides(rec, sides[qk], now)
		m := arb.Market{
			City:     rec.City,
			Book:     econ.Book{SellMin: rec.SellPriceMin, BuyMax: rec.BuyPriceMax},
			AgeHours: age,
			Ask:      sd.Ask,
			Bid:      sd.Bid,
		}
		if s, ok := stats[qk]; ok {
			m.Stats = &s
		}
		k := group{rec.ItemID, rec.Quality}
		byItem[k] = append(byItem[k], m)
	}

	// 按物品、品质的固定顺序配对。以前直接遍历 map,日收益并列的两条路线
	// 每次重算都可能换位置:界面上的行无故跳动,WS 通知的摘要也跟着乱变
	groups := make([]group, 0, len(byItem))
	for k := range byItem {
		groups = append(groups, k)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].itemID != groups[j].itemID {
			return groups[i].itemID < groups[j].itemID
		}
		return groups[i].quality < groups[j].quality
	})

	opt := arb.DefaultOptions(cfg)
	var out []arb.Route
	for _, k := range groups {
		markets := byItem[k]
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
// 等于 FetchSnapshot + EvaluateSnapshot,两半用同一个 now。
func RunWithCapture(ctx context.Context, client *aodp.Client, cfg conf.Config,
	cat *catalog.Catalog, books BookSource, now time.Time) (*Result, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	snap, history, err := FetchSnapshot(ctx, client, cfg, cat, books, now)
	if err != nil {
		return nil, err
	}
	res := EvaluateSnapshot(ctx, snap, cfg, cat, books, now)
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
		sum.AddError(err)
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

	// 跨城用同一份融合后的价格和逐边深度:挂单腿过闸门,吃单腿沿阶梯逐档算
	routes := findRoutes(prices, stats, sides, cat, cfg, now)

	cov := coverageByCity(raw, cfg.Cities, now, cfg.Freshness.MaxHours)
	addCaptureCoverage(cov, got, sides, now)

	// StartedAt/EvaluatedAt 和快照那几项由 EvaluateSnapshot 填
	return &Result{
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
