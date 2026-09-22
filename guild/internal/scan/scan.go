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
}

func coverageByCity(prices []aodp.PriceRecord, cities []string, now time.Time, threshold float64) []CityCoverage {
	ages := map[string][]float64{}
	for _, c := range cities {
		ages[c] = nil
	}
	for _, rec := range prices {
		for _, stamp := range []aodp.Stamp{rec.SellPriceMinDate, rec.BuyPriceMaxDate} {
			if age, ok := stamp.AgeHours(now); ok {
				ages[rec.City] = append(ages[rec.City], age)
			}
		}
	}

	var out []CityCoverage
	for city, values := range ages {
		c := CityCoverage{City: city, WithData: len(values)}
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
		if len(values) > 0 {
			sorted := append([]float64(nil), values...)
			sort.Float64s(sorted)
			n := len(sorted)
			if n%2 == 1 {
				c.MedianAgeHours = sorted[n/2]
			} else {
				c.MedianAgeHours = (sorted[n/2-1] + sorted[n/2]) / 2
			}
			c.HasMedian = true
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].City < out[j].City })
	return out
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
// 比没有价格更危险。
func freshness(rec aodp.PriceRecord, now time.Time) (float64, bool) {
	sell, okSell := rec.SellPriceMinDate.AgeHours(now)
	buy, okBuy := rec.BuyPriceMaxDate.AgeHours(now)
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

// Run 跑一次完整扫描。client 由调用方传进来,测试就能指向假服务器。
func Run(ctx context.Context, client *aodp.Client, cfg conf.Config,
	cat *catalog.Catalog, now time.Time) (*Result, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
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

	var opportunities []screen.Opportunity
	var rejected []screen.Rejected
	counts := map[string]int{}
	for _, rec := range prices {
		var st *histagg.Stats
		if s, ok := stats[histagg.QualityKey{ItemID: rec.ItemID, City: rec.City, Quality: rec.Quality}]; ok {
			st = &s
		}
		opp, rej := screen.Evaluate(rec, st, cfg, cat, now)
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

	routes := findRoutes(prices, stats, cat, cfg, now)

	return &Result{
		StartedAt:      now,
		ItemIDs:        itemIDs,
		MissingItemIDs: missing,
		Opportunities:  opportunities,
		Rejected:       rejected,
		RequestCount:   client.Requests(),
		PriceRows:      len(prices),
		Coverage:       coverageByCity(prices, cfg.Cities, now, cfg.Freshness.MaxHours),
		RejectCounts:   counts,
		Routes:         routes,
		History:        history,
	}, nil
}
