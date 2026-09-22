// Package histagg 把成交历史聚合成日均成交量和基准均价。
//
// 两个口径决策会直接影响排序结果,写在这里备查:
//
//  1. **丢掉当天那个点。** time-scale=24 的最后一个点是还没走完的今天,
//     量必然偏低,拿它算日均会系统性低估流动性。
//
//  2. **日均的分母是"窗口内有数据的天数",不是窗口长度。**
//     AODP 是众包数据,某天缺失几乎总是"那天没人上传"而不是"那天没成交"。
//     按窗口长度平均会把覆盖率问题误算成流动性问题。代价是样本少时噪声大,
//     所以用 MinDaysWithData 兜底。
package histagg

import (
	"math"
	"sort"
	"time"

	"albion-guild/internal/aodp"
)

type Stats struct {
	ItemID string
	City   string

	AvgPrice7d  float64
	AvgPrice30d float64

	// DailyVolumeQty 是 7 日口径的日均成交件数。
	DailyVolumeQty    float64
	DailyVolumeQty30d float64
	// DailyVolumeSilver 是日均成交件数 × 7 日均价。这才是跟本金可比的量纲。
	DailyVolumeSilver float64

	DaysWithData7d  int
	DaysWithData30d int
	LastPoint       time.Time

	// StdDev30d 是 30 日窗口内**日均价**的标准差(不是逐笔)。
	// 同一个绝对价差,在波动大的品种上赚到的概率低得多
	StdDev30d float64
	// CV 是变异系数 = StdDev30d / AvgPrice30d。跨品种可比:
	// 3000 银的货波动 300 和 300 银的货波动 300,风险完全不是一回事
	CV float64
	// TrendPct30d 是 30 日窗口的回归涨跌幅(%),TrendFit 是 R²。
	// 一路下跌的品种,今天的价差明天可能就没了
	TrendPct30d float64
	TrendFit    float64
}

// ZScore 回答"当前价偏离 30 日常态几个标准差"。
//
// 负得多 = 现在便宜,均值回归的买点;正得多 = 现在贵,别追。
// 这是判断"这个价是不是一时的"最直接的一个数,
// 比单看偏离倍数强——它把这个品种自身的波动幅度考虑进去了。
func (s Stats) ZScore(price float64) (float64, bool) {
	if s.StdDev30d <= 0 || s.AvgPrice30d <= 0 {
		return 0, false
	}
	return (price - s.AvgPrice30d) / s.StdDev30d, true
}

// HistoryAgeDays 返回最后一条成交距今多少天。没有历史时第二个返回值是 false。
func (s Stats) HistoryAgeDays(now time.Time) (float64, bool) {
	if s.LastPoint.IsZero() {
		return 0, false
	}
	return now.Sub(s.LastPoint).Seconds() / 86400, true
}

// Key 是 (物品, 城市)。
type Key struct{ ItemID, City string }

// QualityKey 是 (物品, 城市, 品质)。
type QualityKey struct {
	ItemID, City string
	Quality      int
}

// window 取最近 days 个**完整**日。当天未走完,剔除。
func window(points []aodp.HistoryPoint, now time.Time, days int) []aodp.HistoryPoint {
	today := now.UTC().Truncate(24 * time.Hour)
	cutoff := now.AddDate(0, 0, -days)
	var out []aodp.HistoryPoint
	for _, p := range points {
		t := p.Timestamp.T
		if !t.Before(cutoff) && t.UTC().Truncate(24*time.Hour).Before(today) {
			out = append(out, p)
		}
	}
	return out
}

// weightedAvgPrice 按成交量加权。简单算术平均会让一个零星成交日
// 和一个万笔成交日等权。
func weightedAvgPrice(points []aodp.HistoryPoint) float64 {
	var totalQty, amount float64
	for _, p := range points {
		totalQty += float64(p.ItemCount)
		amount += float64(p.AvgPrice) * float64(p.ItemCount)
	}
	if totalQty <= 0 {
		return 0
	}
	return amount / totalQty
}

// stdDev 是日均价围绕加权均值的标准差。
//
// 按成交量加权:一个只成交 3 件的异常日不该和万笔成交日
// 对波动率有同等话语权。
func stdDev(points []aodp.HistoryPoint, mean float64) float64 {
	var totalQty, acc float64
	for _, p := range points {
		q := float64(p.ItemCount)
		if q <= 0 {
			continue
		}
		d := float64(p.AvgPrice) - mean
		acc += d * d * q
		totalQty += q
	}
	if totalQty <= 0 {
		return 0
	}
	return math.Sqrt(acc / totalQty)
}

// trendOf 对窗口内的日均价做最小二乘,返回 (涨跌幅%, R²)。
// 和销量榜用的是同一套口径,只是这里按 (物品, 城市) 算。
func trendOf(points []aodp.HistoryPoint) (changePct, fit float64) {
	byDay := map[int64]*struct{ qty, amount float64 }{}
	for _, p := range points {
		day := p.Timestamp.T.UTC().Truncate(24 * time.Hour).Unix()
		a, ok := byDay[day]
		if !ok {
			a = &struct{ qty, amount float64 }{}
			byDay[day] = a
		}
		a.qty += float64(p.ItemCount)
		a.amount += float64(p.AvgPrice) * float64(p.ItemCount)
	}
	var days []int64
	for d, a := range byDay {
		if a.qty > 0 {
			days = append(days, d)
		}
	}
	if len(days) < 3 {
		return 0, 0
	}
	sort.Slice(days, func(i, j int) bool { return days[i] < days[j] })

	origin := days[0]
	curve := make([][2]float64, 0, len(days))
	for _, d := range days {
		a := byDay[d]
		curve = append(curve, [2]float64{float64((d - origin) / 86400), a.amount / a.qty})
	}
	return leastSquares(curve)
}

// leastSquares 和 rank.LinearTrend 是同一套算法。
// 没有互相 import,因为 rank 依赖 store 的行类型,histagg 不该被拖进去。
func leastSquares(points [][2]float64) (changePct, fit float64) {
	n := float64(len(points))
	var sumX, sumY float64
	for _, p := range points {
		sumX += p[0]
		sumY += p[1]
	}
	meanX, meanY := sumX/n, sumY/n
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
	minX, maxX := points[0][0], points[len(points)-1][0]
	// 基准取拟合直线起点,不是窗口均值——否则跌幅能算出超过 100%
	base := meanY + slope*(minX-meanX)
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
		return change, 1
	}
	return change, math.Max(0, 1-ssRes/ssTot)
}

func distinctDays(points []aodp.HistoryPoint) int {
	seen := map[int64]struct{}{}
	for _, p := range points {
		seen[p.Timestamp.T.UTC().Truncate(24*time.Hour).Unix()] = struct{}{}
	}
	return len(seen)
}

func sumQty(points []aodp.HistoryPoint) float64 {
	var total float64
	for _, p := range points {
		total += float64(p.ItemCount)
	}
	return total
}

func sortByTime(points []aodp.HistoryPoint) {
	sort.Slice(points, func(i, j int) bool {
		return points[i].Timestamp.T.Before(points[j].Timestamp.T)
	})
}

func withTimestamp(points []aodp.HistoryPoint) []aodp.HistoryPoint {
	out := points[:0:0]
	for _, p := range points {
		if p.Timestamp.Valid() {
			out = append(out, p)
		}
	}
	return out
}

// Aggregate 按 (物品, 城市) 聚合。
// AODP 同一组合可能返回多条 series(按品质拆),这里合并。
func Aggregate(seriesList []aodp.HistorySeries, now time.Time, baselineDays, historyDays int) map[Key]Stats {
	merged := map[Key][]aodp.HistoryPoint{}
	for _, s := range seriesList {
		k := Key{s.ItemID, s.Location}
		merged[k] = append(merged[k], s.Data...)
	}

	out := make(map[Key]Stats, len(merged))
	for k, points := range merged {
		points = withTimestamp(points)
		if len(points) == 0 {
			continue
		}
		sortByTime(points)

		short := window(points, now, baselineDays)
		long := window(points, now, historyDays)

		daysShort := distinctDays(short)
		daysLong := distinctDays(long)

		qtyShort, qtyLong := 0.0, 0.0
		if daysShort > 0 {
			qtyShort = sumQty(short) / float64(daysShort)
		}
		if daysLong > 0 {
			qtyLong = sumQty(long) / float64(daysLong)
		}
		avgShort := weightedAvgPrice(short)

		avgLong := weightedAvgPrice(long)
		sd := stdDev(long, avgLong)
		cv := 0.0
		if avgLong > 0 {
			cv = sd / avgLong
		}
		change, fit := trendOf(long)

		out[k] = Stats{
			ItemID:            k.ItemID,
			City:              k.City,
			AvgPrice7d:        avgShort,
			AvgPrice30d:       avgLong,
			StdDev30d:         sd,
			CV:                cv,
			TrendPct30d:       change,
			TrendFit:          fit,
			DailyVolumeQty:    qtyShort,
			DailyVolumeQty30d: qtyLong,
			DailyVolumeSilver: qtyShort * avgShort,
			DaysWithData7d:    daysShort,
			DaysWithData30d:   daysLong,
			LastPoint:         points[len(points)-1].Timestamp.T,
		}
	}
	return out
}

// AggregateByQuality 按 (物品, 城市, 品质) 聚合。
//
// 查价界面要按品质分开看——卓越品质的成交量和普通品质差一个数量级,
// 混在一起的均价对哪一档都不准。
func AggregateByQuality(seriesList []aodp.HistorySeries, now time.Time, baselineDays int) map[QualityKey]Stats {
	merged := map[QualityKey][]aodp.HistoryPoint{}
	for _, s := range seriesList {
		k := QualityKey{s.ItemID, s.Location, s.Quality}
		merged[k] = append(merged[k], s.Data...)
	}

	out := make(map[QualityKey]Stats, len(merged))
	for k, points := range merged {
		points = withTimestamp(points)
		if len(points) == 0 {
			continue
		}
		sortByTime(points)

		win := window(points, now, baselineDays)
		days := distinctDays(win)
		qty := 0.0
		if days > 0 {
			qty = sumQty(win) / float64(days)
		}
		avg := weightedAvgPrice(win)

		out[k] = Stats{
			ItemID:            k.ItemID,
			City:              k.City,
			AvgPrice7d:        avg,
			AvgPrice30d:       avg,
			DailyVolumeQty:    qty,
			DailyVolumeQty30d: qty,
			DailyVolumeSilver: qty * avg,
			DaysWithData7d:    days,
			DaysWithData30d:   days,
			LastPoint:         points[len(points)-1].Timestamp.T,
		}
	}
	return out
}
