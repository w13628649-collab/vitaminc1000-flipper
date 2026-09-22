// 销量排行的聚合口径。这些口径决定榜单顺序,错了整张表就是错的。
package rank

import (
	"math"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC)

type namer map[string]string

func (n namer) NameOf(id string) string {
	if v, ok := n[id]; ok {
		return v
	}
	return id
}

var names = namer{"T5_CLOTH": "精布", "T5_ORE": "钛矿石"}

// point 是 (几天前, 件数, 均价)
type point struct {
	ago   int
	qty   int64
	price int64
}

func daily(itemID, city string, points ...point) []Daily {
	today := now.UTC().Truncate(24 * time.Hour)
	var out []Daily
	for _, p := range points {
		out = append(out, Daily{
			ItemID: itemID, City: city, Quality: 1,
			Day:       today.AddDate(0, 0, -p.ago),
			ItemCount: p.qty, AvgPrice: p.price,
		})
	}
	return out
}

// inWindow 模拟 SQL 那一层的日期筛选,让测试只盯聚合口径。
func inWindow(rows []Daily, windowDays int) []Daily {
	first, last := Window(now, windowDays)
	var out []Daily
	for _, r := range rows {
		if !r.Day.Before(first) && !r.Day.After(last) {
			out = append(out, r)
		}
	}
	return out
}

func rankOf(rows []Daily, opt Options) []Row {
	if opt.WindowDays == 0 {
		opt.WindowDays = 7
	}
	if opt.SortBy == "" {
		opt.SortBy = "daily_silver"
		opt.Descending = true
	}
	opt.Names = names
	return Aggregate(inWindow(rows, opt.WindowDays), opt)
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestDropsTodaysIncompleteDay(t *testing.T) {
	rows := rankOf(daily("T5_CLOTH", "Lymhurst",
		point{1, 1000, 300}, point{2, 1000, 300},
		point{0, 3, 9000}, // ago=0 就是今天
	), Options{})
	if !near(rows[0].DailyQty, 1000) {
		t.Fatalf("日均件数 = %v,想要 1000", rows[0].DailyQty)
	}
	if rows[0].PriceMax != 300 { // 今天那个 9000 的离谱均价不参与
		t.Fatalf("最高价 = %d,想要 300", rows[0].PriceMax)
	}
}

// AODP 缺失某天几乎总是"那天没人上传",不是"那天没成交"。
func TestDailyAverageDividesByDaysWithData(t *testing.T) {
	rows := rankOf(daily("T5_CLOTH", "Lymhurst", point{1, 1000, 300}, point{2, 2000, 300}), Options{})
	if rows[0].DaysWithData != 2 {
		t.Fatalf("有数据天数 = %d", rows[0].DaysWithData)
	}
	if !near(rows[0].DailyQty, 1500) { // 不是 3000/7
		t.Fatalf("日均件数 = %v,想要 1500", rows[0].DailyQty)
	}
}

func TestAveragePriceIsVolumeWeighted(t *testing.T) {
	rows := rankOf(daily("T5_CLOTH", "Lymhurst", point{1, 9000, 100}, point{2, 1000, 1100}), Options{})
	// 算术平均会给出 600,把零星成交日和万笔成交日等权
	if !near(rows[0].AvgPrice, 200) {
		t.Fatalf("加权均价 = %v,想要 200", rows[0].AvgPrice)
	}
}

func TestServerWideMergesCities(t *testing.T) {
	data := append(daily("T5_CLOTH", "Lymhurst", point{1, 1000, 300}),
		daily("T5_CLOTH", "Martlock", point{1, 3000, 300})...)

	rows := rankOf(data, Options{City: AllCities})
	if len(rows) != 1 {
		t.Fatalf("全服口径出了 %d 行,应该合并成 1 行", len(rows))
	}
	if rows[0].City != AllCitiesLabel || rows[0].TotalQty != 4000 {
		t.Fatalf("全服行 = %+v", rows[0])
	}

	// 单城口径只看那一城,城市标签是真名
	var lym []Daily
	for _, d := range data {
		if d.City == "Lymhurst" {
			lym = append(lym, d)
		}
	}
	perCity := rankOf(lym, Options{City: "Lymhurst"})
	if len(perCity) != 1 || perCity[0].City != "Lymhurst" || perCity[0].TotalQty != 1000 {
		t.Fatalf("单城行 = %+v", perCity)
	}
}

// 一天 5 千件、单价 3 万的 T8 材料,和一天 15 万件、单价 23 银的,不是一个生意。
func TestDefaultSortIsDailySilverNotQuantity(t *testing.T) {
	data := append(
		daily("T5_CLOTH", "Lymhurst", point{1, 150_000, 23}),    // 件数高,流水 345 万
		daily("T5_ORE", "Lymhurst", point{1, 5_000, 30_000})..., // 件数低,流水 1.5 亿
	)
	if got := itemIDs(rankOf(data, Options{})); got[0] != "T5_ORE" || got[1] != "T5_CLOTH" {
		t.Fatalf("按流水排 = %v", got)
	}
	byQty := rankOf(data, Options{SortBy: "daily_qty", Descending: true})
	if got := itemIDs(byQty); got[0] != "T5_CLOTH" || got[1] != "T5_ORE" {
		t.Fatalf("按件数排 = %v", got)
	}
}

func TestVolatilityAndPriceRange(t *testing.T) {
	rows := rankOf(daily("T5_CLOTH", "Lymhurst",
		point{1, 100, 100}, point{2, 100, 200}, point{3, 100, 300}), Options{})
	r := rows[0]
	if r.PriceMin != 100 || r.PriceMedian != 200 || r.PriceMax != 300 {
		t.Fatalf("价格区间 = (%d, %v, %d)", r.PriceMin, r.PriceMedian, r.PriceMax)
	}
	if !near(r.Volatility, 1.0) { // (300-100)/200
		t.Fatalf("波动率 = %v,想要 1.0", r.Volatility)
	}
}

func TestOutOfWindowDaysAreExcluded(t *testing.T) {
	rows := rankOf(daily("T5_CLOTH", "Lymhurst", point{1, 1000, 300}, point{20, 9999, 300}), Options{})
	if rows[0].TotalQty != 1000 {
		t.Fatalf("总件数 = %d,窗口外那天不该算进来", rows[0].TotalQty)
	}
}

func TestCategoryFilter(t *testing.T) {
	data := append(daily("T5_CLOTH", "Lymhurst", point{1, 1000, 300}),
		daily("T5_ORE", "Lymhurst", point{1, 1000, 300})...)
	wanted := map[string]struct{}{"T5_CLOTH": {}}
	if got := itemIDs(rankOf(data, Options{Wanted: wanted})); len(got) != 1 || got[0] != "T5_CLOTH" {
		t.Fatalf("按分类筛 = %v", got)
	}
}

// 某天有人挂了个离谱均价,首尾相减整条趋势就反了,回归不会。
func TestTrendUsesRegressionNotEndpoints(t *testing.T) {
	var points [][2]float64
	for i := range 9 {
		points = append(points, [2]float64{float64(i), 100 + float64(i)*10})
	}
	points = append(points, [2]float64{9, 60}) // 整体一路上涨,但最后一天砸了个低价

	change, fit := LinearTrend(points)
	if change <= 0 { // 首尾相减会得出 -40%
		t.Fatalf("涨跌幅 = %v,回归应该仍然判涨", change)
	}
	if fit >= 0.9 { // 那个离群点把拟合拉差了
		t.Fatalf("R² = %v,离群点应该把拟合拉差", fit)
	}
}

// 基准取拟合直线的起点。用窗口均值当分母,300 跌到 10 会算出 -187%。
func TestDropNeverExceedsOneHundredPercent(t *testing.T) {
	var points [][2]float64
	for i := range 11 {
		points = append(points, [2]float64{float64(i), 300 - float64(i)*29})
	}
	change, _ := LinearTrend(points)
	if change <= -100 || change >= -90 {
		t.Fatalf("涨跌幅 = %v,应该落在 (-100, -90)", change)
	}
}

// 斜率再陡,拟合不好也只是噪声。
func TestChoppyIsNotGivenADirection(t *testing.T) {
	var points [][2]float64
	for i := range 10 {
		delta := -40.0
		if i%2 == 1 {
			delta = 40
		}
		points = append(points, [2]float64{float64(i), 150 + delta})
	}
	change, fit := LinearTrend(points)
	if fit >= 0.3 {
		t.Fatalf("R² = %v,来回震荡不该拟合得好", fit)
	}
	if got := ClassifyTrend(change, fit); got != "choppy" {
		t.Fatalf("趋势 = %s,想要 choppy", got)
	}
	if got := ClassifyTrend(30, 0.9); got != "up" {
		t.Fatalf("趋势 = %s,想要 up", got)
	}
	if got := ClassifyTrend(-30, 0.9); got != "down" {
		t.Fatalf("趋势 = %s,想要 down", got)
	}
	if got := ClassifyTrend(1, 0.99); got != "flat" { // 涨跌太小,方向没意义
		t.Fatalf("趋势 = %s,想要 flat", got)
	}
}

func TestSortCanBeAscendingOrDescending(t *testing.T) {
	data := append(daily("T5_CLOTH", "Lymhurst", point{1, 1000, 100}),
		daily("T5_ORE", "Lymhurst", point{1, 1000, 500})...)

	highFirst := itemIDs(rankOf(data, Options{SortBy: "avg_price", Descending: true}))
	lowFirst := itemIDs(rankOf(data, Options{SortBy: "avg_price", Descending: false}))
	if highFirst[0] != "T5_ORE" || highFirst[1] != "T5_CLOTH" {
		t.Fatalf("降序 = %v", highFirst)
	}
	if lowFirst[0] != "T5_CLOTH" || lowFirst[1] != "T5_ORE" {
		t.Fatalf("升序 = %v", lowFirst)
	}
}

// 不然两城价格差一截时,折线会在两个水平之间来回跳,看着像震荡。
func TestServerWideCurveIsVolumeWeightedPerDay(t *testing.T) {
	data := append(
		daily("T5_CLOTH", "Lymhurst", point{3, 1000, 100}, point{2, 1000, 100}, point{1, 1000, 100}),
		daily("T5_CLOTH", "Martlock", point{3, 1000, 300}, point{2, 1000, 300}, point{1, 1000, 300})...,
	)
	row := rankOf(data, Options{City: AllCities})[0]
	for _, p := range row.Series {
		if p[1] != 200 { // 每天都是加权后的 200
			t.Fatalf("折线 = %v,每天都该是 200", row.Series)
		}
	}
	if row.Trend != "flat" {
		t.Fatalf("趋势 = %s,想要 flat", row.Trend)
	}
}

func itemIDs(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ItemID
	}
	return out
}
