package histagg

import (
	"math"
	"testing"
	"time"

	"albion-guild/internal/aodp"
)

var now = time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC)

type point struct {
	day   string
	qty   int64
	price int64
}

func series(points ...point) aodp.HistorySeries {
	s := aodp.HistorySeries{Location: "Lymhurst", ItemID: "T5_CLOTH", Quality: 1}
	for _, p := range points {
		s.Data = append(s.Data, aodp.HistoryPoint{
			ItemCount: p.qty, AvgPrice: p.price,
			Timestamp: aodp.Stamp{T: aodp.ParseTime(p.day)},
		})
	}
	return s
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func only(t *testing.T, stats map[Key]Stats) Stats {
	t.Helper()
	s, ok := stats[Key{"T5_CLOTH", "Lymhurst"}]
	if !ok {
		t.Fatal("没聚合出 T5_CLOTH/Lymhurst")
	}
	return s
}

// 当天的点量必然偏低、价格可能是单笔异常值,拿它算日均会系统性失真。
func TestDropsTodaysIncompleteDay(t *testing.T) {
	s := only(t, Aggregate([]aodp.HistorySeries{series(
		point{"2026-09-19T00:00:00", 1000, 300},
		point{"2026-09-20T00:00:00", 1000, 300},
		point{"2026-09-21T00:00:00", 3, 9000}, // 今天,只有一笔离谱成交
	)}, now, 7, 30))

	if !near(s.AvgPrice7d, 300) {
		t.Fatalf("7 日均价 = %v,想要 300", s.AvgPrice7d)
	}
	if !near(s.DailyVolumeQty, 1000) {
		t.Fatalf("日均件数 = %v,想要 1000", s.DailyVolumeQty)
	}
}

func TestAveragePriceIsVolumeWeighted(t *testing.T) {
	s := only(t, Aggregate([]aodp.HistorySeries{series(
		point{"2026-09-19T00:00:00", 9000, 100},
		point{"2026-09-20T00:00:00", 1000, 1100},
	)}, now, 7, 30))

	// 算术平均会给出 600,把一个零星成交日和一个万笔成交日等权
	if !near(s.AvgPrice7d, 200) {
		t.Fatalf("7 日均价 = %v,想要 200", s.AvgPrice7d)
	}
}

// AODP 缺失日几乎总是"那天没人上传",不是"那天没成交"。
func TestDailyAverageDividesByDaysWithData(t *testing.T) {
	s := only(t, Aggregate([]aodp.HistorySeries{series(
		point{"2026-09-19T00:00:00", 1000, 300},
		point{"2026-09-20T00:00:00", 2000, 300},
	)}, now, 7, 30))

	if s.DaysWithData7d != 2 {
		t.Fatalf("有数据天数 = %d,想要 2", s.DaysWithData7d)
	}
	if !near(s.DailyVolumeQty, 1500) { // 不是 3000/7
		t.Fatalf("日均件数 = %v,想要 1500", s.DailyVolumeQty)
	}
}

func TestSeriesForSameItemAndCityAreMerged(t *testing.T) {
	a := series(point{"2026-09-19T00:00:00", 1000, 300})
	b := series(point{"2026-09-20T00:00:00", 1000, 300})
	s := only(t, Aggregate([]aodp.HistorySeries{a, b}, now, 7, 30))
	if s.DaysWithData7d != 2 {
		t.Fatalf("有数据天数 = %d,两条 series 应该合并成 2 天", s.DaysWithData7d)
	}
}

func TestSevenAndThirtyDayWindowsAreSeparate(t *testing.T) {
	s := only(t, Aggregate([]aodp.HistorySeries{series(
		point{"2026-09-01T00:00:00", 1000, 100}, // 只在 30 日窗口里
		point{"2026-09-19T00:00:00", 1000, 300},
		point{"2026-09-20T00:00:00", 1000, 300},
	)}, now, 7, 30))

	if !near(s.AvgPrice7d, 300) {
		t.Fatalf("7 日均价 = %v,想要 300", s.AvgPrice7d)
	}
	if !near(s.AvgPrice30d, 700.0/3) {
		t.Fatalf("30 日均价 = %v,想要 %v", s.AvgPrice30d, 700.0/3)
	}
}

// 同一个绝对价差,放在波动大的品种上赚到的概率低得多。
// CV 让不同价位的品种可比。
func TestVolatilityIsComparableAcrossPriceLevels(t *testing.T) {
	steady := series()
	swingy := series()
	for i := 1; i <= 10; i++ {
		day := now.AddDate(0, 0, -i).Format("2006-01-02T00:00:00")
		steady.Data = append(steady.Data, aodp.HistoryPoint{
			ItemCount: 1000, AvgPrice: int64(3000 + (i%2)*20), // 3000 上下摆 20
			Timestamp: aodp.Stamp{T: aodp.ParseTime(day)},
		})
		swingy.Data = append(swingy.Data, aodp.HistoryPoint{
			ItemCount: 1000, AvgPrice: int64(300 + (i%2)*100), // 300 上下摆 100
			Timestamp: aodp.Stamp{T: aodp.ParseTime(day)},
		})
	}
	a := only(t, Aggregate([]aodp.HistorySeries{steady}, now, 7, 30))
	swingy.ItemID = "T5_WOOD"
	b := Aggregate([]aodp.HistorySeries{swingy}, now, 7, 30)[Key{"T5_WOOD", "Lymhurst"}]

	if !(a.StdDev30d > b.StdDev30d*0 && a.CV < b.CV) {
		t.Fatalf("变异系数:稳的 %v 应该小于飘的 %v(标准差 %v vs %v)",
			a.CV, b.CV, a.StdDev30d, b.StdDev30d)
	}
}

// z-score 回答"现在这个价是不是一时的"。
func TestZScoreFlagsCheapAndExpensive(t *testing.T) {
	s := series()
	for i := 1; i <= 10; i++ {
		day := now.AddDate(0, 0, -i).Format("2006-01-02T00:00:00")
		s.Data = append(s.Data, aodp.HistoryPoint{
			ItemCount: 1000, AvgPrice: int64(1000 + (i%2)*100), // 均值 1050,σ 50
			Timestamp: aodp.Stamp{T: aodp.ParseTime(day)},
		})
	}
	st := only(t, Aggregate([]aodp.HistorySeries{s}, now, 7, 30))

	cheap, ok := st.ZScore(900)
	if !ok || cheap >= -1 {
		t.Fatalf("900 相对均值 %v(σ %v)的 z = %v,应该明显为负", st.AvgPrice30d, st.StdDev30d, cheap)
	}
	rich, _ := st.ZScore(1200)
	if rich <= 1 {
		t.Fatalf("1200 的 z = %v,应该明显为正", rich)
	}
	// 没有波动就没有 z-score,不能拿 0 当"正常"
	flat := Stats{AvgPrice30d: 1000}
	if _, ok := flat.ZScore(1500); ok {
		t.Fatal("σ 为 0 时不该给出 z-score")
	}
}

// 两个聚合口径必须给出完全一致的指标,只是分组粒度不同。
//
// 以前按品质聚合那份只算 7 日窗口,没有标准差、变异系数和趋势。
// 扫描路径换过去用的时候,风险指标会被悄悄丢掉——z-score 和波动率
// 全变成 0,而界面上照样显示。
func TestBothAggregationsProduceTheSameMetrics(t *testing.T) {
	s := series()
	for i := 1; i <= 20; i++ {
		day := now.AddDate(0, 0, -i).Format("2006-01-02T00:00:00")
		s.Data = append(s.Data, aodp.HistoryPoint{
			ItemCount: int64(1000 + i*10), AvgPrice: int64(1000 + (i%3)*120),
			Timestamp: aodp.Stamp{T: aodp.ParseTime(day)},
		})
	}
	byCity := Aggregate([]aodp.HistorySeries{s}, now, 7, 30)[Key{"T5_CLOTH", "Lymhurst"}]
	byQual := AggregateByQuality([]aodp.HistorySeries{s}, now, 7, 30)[QualityKey{"T5_CLOTH", "Lymhurst", 1}]

	if byCity != byQual {
		t.Fatalf("两条路径结果不一致:\n  按城市 %+v\n  按品质 %+v", byCity, byQual)
	}
	// 而且风险指标真的算出来了,不是恰好都为 0
	if byQual.StdDev30d <= 0 || byQual.CV <= 0 {
		t.Fatalf("风险指标没算出来:σ=%v CV=%v", byQual.StdDev30d, byQual.CV)
	}
}

// 品质必须分开。合并的话同一份成交量会被每档品质各领一次,
// 资金分配那边就会按品质数量成倍超配。
func TestQualitiesDoNotShareVolume(t *testing.T) {
	var all []aodp.HistorySeries
	for q := 1; q <= 3; q++ {
		s := series()
		s.Quality = q
		for i := 1; i <= 5; i++ {
			day := now.AddDate(0, 0, -i).Format("2006-01-02T00:00:00")
			s.Data = append(s.Data, aodp.HistoryPoint{
				ItemCount: 1000, AvgPrice: 1000,
				Timestamp: aodp.Stamp{T: aodp.ParseTime(day)},
			})
		}
		all = append(all, s)
	}
	byQual := AggregateByQuality(all, now, 7, 30)
	if len(byQual) != 3 {
		t.Fatalf("应该分成 3 档,得到 %d", len(byQual))
	}
	for q := 1; q <= 3; q++ {
		st := byQual[QualityKey{"T5_CLOTH", "Lymhurst", q}]
		if !near(st.DailyVolumeQty, 1000) {
			t.Fatalf("品质 %d 的日均件数 = %v,应该是它自己那 1000 件", q, st.DailyVolumeQty)
		}
	}
	// 合并口径会把三档加起来,正是要避免的
	merged := Aggregate(all, now, 7, 30)[Key{"T5_CLOTH", "Lymhurst"}]
	if !near(merged.DailyVolumeQty, 3000) {
		t.Fatalf("合并口径应该是 3000(这正是不能用它的原因),得到 %v", merged.DailyVolumeQty)
	}
}
