// Package rank 是销量排行榜。
//
// 历史数据增量存进 market_history,排行榜从库里聚合,不是每次都打 API。
// 附带好处:**AODP 的 history 只给最近一段,自己存就能越攒越长**——
// 跑上几个月,手里就有一份上游给不了的长历史。
//
// 三个口径,和 histagg 保持一致:
//
//  1. **丢掉当天那个点。** 还没走完的一天量必然偏低。
//  2. **日均的分母是"窗口内有数据的天数"**,不是窗口长度。AODP 是众包数据,
//     某天缺失几乎总是"那天没人上传"而不是"那天没成交"。
//  3. **均价按成交量加权。** 算术平均会让零星成交日和万笔成交日等权。
//
// 一个绕不开的限制:**item_count 只统计卖单成交**,买单那边的成交量拿不到。
// 所以这里的销量是偏低的一侧,真实流动性只会更高。
package rank

import (
	"math"
	"sort"
	"time"
)

// AllCities 是"全服口径"的城市哨兵值。
const AllCities = "__all__"

// AllCitiesLabel 是全服口径那一行显示的城市名。
const AllCitiesLabel = "全服"

// Daily 是库里的一条日线。
type Daily struct {
	ItemID    string
	City      string
	Quality   int
	Day       time.Time
	ItemCount int64
	AvgPrice  int64
}

type Row struct {
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	City     string `json:"city"`
	Quality  int    `json:"quality"`

	// DailyQty 是日均成交件数(只含卖单成交)。
	DailyQty float64 `json:"daily_qty"`
	// DailySilver 是日均流水 = 日均件数 × 加权均价。跟本金可比的量纲。
	DailySilver float64 `json:"daily_silver"`

	TotalQty    int64   `json:"total_qty"`
	AvgPrice    float64 `json:"avg_price"`
	PriceMin    int64   `json:"price_min"`
	PriceMax    int64   `json:"price_max"`
	PriceMedian float64 `json:"price_median"`
	// Volatility 是 (最高 - 最低) / 中位。价格摆得越宽,价差机会越多,风险也越大。
	Volatility float64 `json:"volatility"`

	// TrendPct 是最小二乘拟合出的窗口内涨跌幅(%)。
	// 首尾相减会被单日异常值带偏,回归不会。
	TrendPct float64 `json:"trend_pct"`
	// TrendFit 是拟合优度 R²。低就说明是来回震荡,那条斜线没有意义。
	TrendFit float64 `json:"trend_fit"`
	// Trend 是 up / down / choppy / flat,拿涨跌幅和 R² 一起判的。
	Trend string `json:"trend"`

	// Series 是 [[天序号, 均价], …],给前端画折线。缺数据的日子直接不出现。
	Series [][2]float64 `json:"series"`

	DaysWithData int    `json:"days_with_data"`
	LastDay      string `json:"last_day"`
}

// LinearTrend 最小二乘拟合,返回 (窗口内涨跌幅, R²)。
//
// 直接拿首尾两天相减很容易被单日异常值带偏——某天有人挂了个离谱均价,
// 整条趋势就反了。回归用上全部点,稳得多。R² 则回答"这条斜线值不值得信":
// 低于 0.3 基本就是来回震荡,方向没有意义。
func LinearTrend(points [][2]float64) (changePct, fit float64) {
	n := len(points)
	if n < 3 {
		return 0, 0
	}
	var sumX, sumY float64
	for _, p := range points {
		sumX += p[0]
		sumY += p[1]
	}
	meanX, meanY := sumX/float64(n), sumY/float64(n)
	if meanY <= 0 {
		return 0, 0
	}

	var sxx, sxy float64
	for _, p := range points {
		sxx += (p[0] - meanX) * (p[0] - meanX)
		sxy += (p[0] - meanX) * (p[1] - meanY)
	}
	if sxx == 0 {
		return 0, 0
	}
	slope := sxy / sxx

	minX, maxX := points[0][0], points[0][0]
	for _, p := range points {
		minX = math.Min(minX, p[0])
		maxX = math.Max(maxX, p[0])
	}
	// 基准取拟合直线的**起点**,不是窗口均值。用均值当分母时,
	// 一条从 300 跌到 10 的线会算出 -187%,跌幅超过 100% 说不通。
	fittedStart := meanY + slope*(minX-meanX)
	base := fittedStart
	if base <= 0 {
		base = meanY
	}
	change := slope * (maxX - minX) / base * 100

	var ssTot, ssRes float64
	for _, p := range points {
		ssTot += (p[1] - meanY) * (p[1] - meanY)
		pred := meanY + slope*(p[0]-meanX)
		ssRes += (p[1] - pred) * (p[1] - pred)
	}
	if ssTot == 0 {
		return 0, 1
	}
	return change, math.Max(0, 1-ssRes/ssTot)
}

// FlatBand 之内的涨跌幅算"没动"。
const FlatBand = 4.0

func ClassifyTrend(changePct, fit float64) string {
	if math.Abs(changePct) < FlatBand {
		return "flat"
	}
	// 斜率再陡,拟合不好也只是噪声
	if fit < 0.3 {
		return "choppy"
	}
	if changePct > 0 {
		return "up"
	}
	return "down"
}

// Options 是榜单的筛选和排序条件。
type Options struct {
	// City 是 AllCities 时把各城合并成一行。
	City       string
	WindowDays int
	Limit      int
	SortBy     string
	Descending bool
	// Wanted 非 nil 时只保留这些物品(按分类筛选后的结果)。
	Wanted map[string]struct{}
	// Names 把 item id 翻成显示名,可以为 nil。
	Names interface{ NameOf(string) string }
}

// Window 返回榜单该查哪一段日期。当天那根还没走完,剔掉;
// 窗口从昨天往前数。
func Window(now time.Time, windowDays int) (first, last time.Time) {
	today := now.UTC().Truncate(24 * time.Hour)
	last = today.AddDate(0, 0, -1)
	first = last.AddDate(0, 0, -(windowDays - 1))
	return first, last
}

type bucketKey struct{ itemID, label string }

// Aggregate 把日线聚合成榜单。SQL 只负责按日期和城市筛,
// 口径全在这里——这样不用连数据库就能测。
func Aggregate(daily []Daily, opt Options) []Row {
	buckets := map[bucketKey][]Daily{}
	for _, d := range daily {
		if opt.Wanted != nil {
			if _, ok := opt.Wanted[d.ItemID]; !ok {
				continue
			}
		}
		// 全服口径把各城合并成一行;单城口径按 (物品, 城市) 分开
		label := d.City
		if opt.City == AllCities {
			label = AllCitiesLabel
		}
		k := bucketKey{d.ItemID, label}
		buckets[k] = append(buckets[k], d)
	}

	var out []Row
	for k, group := range buckets {
		row, ok := summarize(k, group, opt)
		if ok {
			out = append(out, row)
		}
	}

	sortRows(out, opt.SortBy, opt.Descending)
	if opt.Limit > 0 && len(out) > opt.Limit {
		out = out[:opt.Limit]
	}
	return out
}

func summarize(k bucketKey, group []Daily, opt Options) (Row, bool) {
	var totalQty int64
	for _, d := range group {
		totalQty += d.ItemCount
	}
	if totalQty <= 0 {
		return Row{}, false
	}

	days := map[int64]struct{}{}
	var prices []int64
	var amount float64
	var lastDay time.Time
	quality := 0
	for _, d := range group {
		days[d.Day.Unix()] = struct{}{}
		if d.AvgPrice > 0 {
			prices = append(prices, d.AvgPrice)
		}
		amount += float64(d.AvgPrice) * float64(d.ItemCount)
		if d.Day.After(lastDay) {
			lastDay = d.Day
		}
		quality = d.Quality
	}
	if len(prices) == 0 || len(days) == 0 {
		return Row{}, false
	}

	weighted := amount / float64(totalQty)
	dailyQty := float64(totalQty) / float64(len(days))
	sort.Slice(prices, func(i, j int) bool { return prices[i] < prices[j] })
	mid := median(prices)

	// 全服口径下同一天有多城数据,先按量加权成一个日均价再谈趋势。
	// 不然两城价格差一截时,折线会在两个水平之间来回跳,看着像震荡。
	type dayAgg struct{ qty, amount float64 }
	perDay := map[int64]*dayAgg{}
	for _, d := range group {
		a, ok := perDay[d.Day.Unix()]
		if !ok {
			a = &dayAgg{}
			perDay[d.Day.Unix()] = a
		}
		a.qty += float64(d.ItemCount)
		a.amount += float64(d.AvgPrice) * float64(d.ItemCount)
	}
	var stamps []int64
	for ts, a := range perDay {
		if a.qty > 0 {
			stamps = append(stamps, ts)
		}
	}
	sort.Slice(stamps, func(i, j int) bool { return stamps[i] < stamps[j] })

	curve := make([][2]float64, 0, len(stamps))
	if len(stamps) > 0 {
		origin := stamps[0]
		for _, ts := range stamps {
			a := perDay[ts]
			curve = append(curve, [2]float64{
				float64((ts - origin) / 86400),
				math.Round(a.amount / a.qty),
			})
		}
	}
	change, fit := LinearTrend(curve)

	volatility := 0.0
	if mid != 0 {
		volatility = float64(prices[len(prices)-1]-prices[0]) / mid
	}

	name := k.itemID
	if opt.Names != nil {
		name = opt.Names.NameOf(k.itemID)
	}

	return Row{
		ItemID:       k.itemID,
		ItemName:     name,
		City:         k.label,
		Quality:      quality,
		DailyQty:     dailyQty,
		DailySilver:  dailyQty * weighted,
		TotalQty:     totalQty,
		AvgPrice:     weighted,
		PriceMin:     prices[0],
		PriceMax:     prices[len(prices)-1],
		PriceMedian:  mid,
		Volatility:   volatility,
		TrendPct:     change,
		TrendFit:     fit,
		Trend:        ClassifyTrend(change, fit),
		Series:       curve,
		DaysWithData: len(days),
		LastDay:      lastDay.Format("2006-01-02"),
	}, true
}

func median(sorted []int64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return float64(sorted[n/2])
	}
	return float64(sorted[n/2-1]+sorted[n/2]) / 2
}

var sortKeys = map[string]func(Row) float64{
	"daily_silver":   func(r Row) float64 { return r.DailySilver },
	"daily_qty":      func(r Row) float64 { return r.DailyQty },
	"volatility":     func(r Row) float64 { return r.Volatility },
	"avg_price":      func(r Row) float64 { return r.AvgPrice },
	"total_qty":      func(r Row) float64 { return float64(r.TotalQty) },
	"price_min":      func(r Row) float64 { return float64(r.PriceMin) },
	"price_max":      func(r Row) float64 { return float64(r.PriceMax) },
	"price_median":   func(r Row) float64 { return r.PriceMedian },
	"trend_pct":      func(r Row) float64 { return r.TrendPct },
	"days_with_data": func(r Row) float64 { return float64(r.DaysWithData) },
}

func sortRows(rows []Row, sortBy string, descending bool) {
	if sortBy == "item_name" {
		sort.SliceStable(rows, func(i, j int) bool {
			if descending {
				return rows[i].ItemName > rows[j].ItemName
			}
			return rows[i].ItemName < rows[j].ItemName
		})
		return
	}
	key, ok := sortKeys[sortBy]
	if !ok {
		key = sortKeys["daily_silver"]
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := key(rows[i]), key(rows[j])
		if a == b {
			return rows[i].ItemID < rows[j].ItemID
		}
		if descending {
			return a > b
		}
		return a < b
	})
}
