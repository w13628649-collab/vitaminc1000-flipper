// Troll 过滤的回归测试。
//
// 这些用例就是"买了就套死"的那些挂单。任何一条挂掉,主榜上就会出现假机会。
package screen

import (
	"strings"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/conf"
	"albion-guild/internal/histagg"
)

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

type fakeNamer map[string]string

func (f fakeNamer) NameOf(id string) string {
	if n, ok := f[id]; ok {
		return n
	}
	return id
}

var names = fakeNamer{"T5_CLOTH": "精布", "T5_WOOD": "杉木", "T4_CLOTH": "细布"}

func config() conf.Config {
	c := conf.Default()
	c.Cities = []string{"Lymhurst"}
	c.Items.Patterns = []string{"T5_CLOTH"}
	return c
}

type priceOpt struct {
	buy, sell         int64
	buyAgeH, sellAgeH float64
	noBuyStamp        bool
	noSellStamp       bool
}

func makePrice(o priceOpt) aodp.PriceRecord {
	if o.buy == 0 {
		o.buy = 1000
	}
	if o.sell == 0 {
		o.sell = 1200
	}
	if o.buyAgeH == 0 {
		o.buyAgeH = 1
	}
	if o.sellAgeH == 0 {
		o.sellAgeH = 1
	}
	rec := aodp.PriceRecord{
		ItemID: "T5_CLOTH", City: "Lymhurst", Quality: 1,
		SellPriceMin: o.sell, BuyPriceMax: o.buy,
	}
	if !o.noSellStamp {
		rec.SellPriceMinDate = aodp.Stamp{T: now.Add(-time.Duration(o.sellAgeH * float64(time.Hour)))}
	}
	if !o.noBuyStamp {
		rec.BuyPriceMaxDate = aodp.Stamp{T: now.Add(-time.Duration(o.buyAgeH * float64(time.Hour)))}
	}
	return rec
}

type statsOpt struct {
	avg7d          float64
	dailyQty       float64
	days           int
	historyAgeDays float64
}

func makeStats(o statsOpt) *histagg.Stats {
	if o.avg7d == 0 {
		o.avg7d = 1100
	}
	if o.dailyQty == 0 {
		o.dailyQty = 5000
	}
	if o.days == 0 {
		o.days = 7
	}
	if o.historyAgeDays == 0 {
		o.historyAgeDays = 0.5
	}
	return &histagg.Stats{
		ItemID: "T5_CLOTH", City: "Lymhurst",
		AvgPrice7d: o.avg7d, AvgPrice30d: o.avg7d,
		DailyVolumeQty: o.dailyQty, DailyVolumeQty30d: o.dailyQty,
		DailyVolumeSilver: o.dailyQty * o.avg7d,
		DaysWithData7d:    o.days, DaysWithData30d: o.days,
		LastPoint: now.Add(-time.Duration(o.historyAgeDays * 24 * float64(time.Hour))),
	}
}

func reason(t *testing.T, rec aodp.PriceRecord, st *histagg.Stats, cfg conf.Config) string {
	t.Helper()
	opp, rej := Evaluate(rec, st, cfg, names, now)
	if rej == nil {
		t.Fatalf("本该被拒,却通过了: %+v", opp)
	}
	return rej.Reason
}

func TestNormalOpportunityPassesWithHighConfidence(t *testing.T) {
	opp, rej := Evaluate(makePrice(priceOpt{}), makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("本该通过,却被 %s 拒了", rej.Reason)
	}
	if opp.Confidence != High {
		t.Fatalf("置信度 = %s,想要 high", opp.Confidence)
	}
	if opp.DailyProfit <= 0 {
		t.Fatalf("日收益 = %v,应该为正", opp.DailyProfit)
	}
	// 挂单要压过现价才排得到队首
	if opp.MyBid != 1001 || opp.MyAsk != 1199 {
		t.Fatalf("挂单价 = (%d, %d),想要 (1001, 1199)", opp.MyBid, opp.MyAsk)
	}
	if opp.ItemName != "精布" {
		t.Fatalf("物品名 = %s,想要 精布", opp.ItemName)
	}
}

func TestTrollHighSellCaughtByDeviation(t *testing.T) {
	// 1 件货挂 20 倍价——天真计算器会把它报成天大的机会
	if got := reason(t, makePrice(priceOpt{sell: 22_000}), makeStats(statsOpt{}), config()); got != "deviation" {
		t.Fatalf("拒绝原因 = %s,想要 deviation", got)
	}
}

func TestTrollLowBuyCaughtByDeviation(t *testing.T) {
	rec := makePrice(priceOpt{buy: 10, sell: 1200})
	if got := reason(t, rec, makeStats(statsOpt{}), config()); got != "deviation" {
		t.Fatalf("拒绝原因 = %s,想要 deviation", got)
	}
}

// 买价 0.45x、卖价 2.4x,各自都在 [0.4, 2.5] 内"合规",
// 组合起来却是 5 倍价差。这是 SPEC 四层过滤盖不住的缺口。
func TestBothSidesTrollCaughtByMarginCap(t *testing.T) {
	rec := makePrice(priceOpt{buy: 495, sell: 2640})
	if got := reason(t, rec, makeStats(statsOpt{}), config()); got != "implausible_margin" {
		t.Fatalf("拒绝原因 = %s,想要 implausible_margin", got)
	}
}

func TestStaleDataRejected(t *testing.T) {
	rec := makePrice(priceOpt{sellAgeH: 7})
	if got := reason(t, rec, makeStats(statsOpt{}), config()); got != "stale" {
		t.Fatalf("拒绝原因 = %s,想要 stale", got)
	}
}

// 价格 0 是 AODP 的"无数据"哨兵,不是"白送"
func TestOneSidedDataStaysOffTheBoard(t *testing.T) {
	rec := makePrice(priceOpt{})
	rec.BuyPriceMax = 0 // AODP 用 0 表示这一侧没数据
	if got := reason(t, rec, makeStats(statsOpt{}), config()); got != "one_sided" {
		t.Fatalf("拒绝原因 = %s,想要 one_sided", got)
	}
}

func TestLowVolumeRejected(t *testing.T) {
	stats := makeStats(statsOpt{dailyQty: 100}) // 100 × 1100 = 11 万 < 50 万
	if got := reason(t, makePrice(priceOpt{}), stats, config()); got != "low_volume" {
		t.Fatalf("拒绝原因 = %s,想要 low_volume", got)
	}
}

// 买价 >= 卖价 在真实市场不可能持续存在,说明两侧快照来自不同时间
func TestCrossedBookRejected(t *testing.T) {
	rec := makePrice(priceOpt{buy: 1300, sell: 1200})
	if got := reason(t, rec, makeStats(statsOpt{}), config()); got != "crossed_book" {
		t.Fatalf("拒绝原因 = %s,想要 crossed_book", got)
	}
}

func TestSpreadBelowFrictionIsUnprofitable(t *testing.T) {
	rec := makePrice(priceOpt{buy: 1000, sell: 1050}) // 5% 价差 < 9% 摩擦
	if got := reason(t, rec, makeStats(statsOpt{}), config()); got != "unprofitable" {
		t.Fatalf("拒绝原因 = %s,想要 unprofitable", got)
	}
}

func TestThinHistoryRejected(t *testing.T) {
	stats := makeStats(statsOpt{days: 2})
	if got := reason(t, makePrice(priceOpt{}), stats, config()); got != "thin_history" {
		t.Fatalf("拒绝原因 = %s,想要 thin_history", got)
	}
}

func TestStaleHistoryRejected(t *testing.T) {
	stats := makeStats(statsOpt{historyAgeDays: 5})
	if got := reason(t, makePrice(priceOpt{}), stats, config()); got != "stale_history" {
		t.Fatalf("拒绝原因 = %s,想要 stale_history", got)
	}
}

// 流水够高但单价极高 → 20% 的日成交量不足 1 件
func TestLessThanOneUnitAbsorbableRejected(t *testing.T) {
	stats := makeStats(statsOpt{avg7d: 2_000_000, dailyQty: 1})
	rec := makePrice(priceOpt{buy: 1_800_000, sell: 2_200_000})
	if got := reason(t, rec, stats, config()); got != "too_thin" {
		t.Fatalf("拒绝原因 = %s,想要 too_thin", got)
	}
}

func TestOlderDataDropsToMedium(t *testing.T) {
	opp, rej := Evaluate(makePrice(priceOpt{sellAgeH: 4}), makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("本该通过,却被 %s 拒了", rej.Reason)
	}
	if opp.Confidence != Medium {
		t.Fatalf("置信度 = %s,想要 medium", opp.Confidence)
	}
}

// 卖价 2.35x,距上限 2.5 只有 0.15,在 15% 边缘带内。
// 这个价差同时会撞上毛利率上限,这里只想测边缘降级,所以把那层放开。
func TestNearDeviationEdgeDropsToLow(t *testing.T) {
	cfg := config()
	cfg.Filters.MaxMargin = 5.0
	opp, rej := Evaluate(makePrice(priceOpt{buy: 1000, sell: 2585}), makeStats(statsOpt{}), cfg, names, now)
	if rej != nil {
		t.Fatalf("本该通过,却被 %s 拒了", rej.Reason)
	}
	if opp.Confidence != Low {
		t.Fatalf("置信度 = %s,想要 low", opp.Confidence)
	}
	found := false
	for _, n := range opp.Notes() {
		if strings.Contains(n, "边缘") {
			found = true
		}
	}
	if !found {
		t.Fatalf("没有说明是边缘降级: %v", opp.Notes())
	}
}

func TestNearVolumeFloorDropsToLow(t *testing.T) {
	// 500 × 1100 = 55 万,在 50 万~62.5 万边缘带
	opp, rej := Evaluate(makePrice(priceOpt{}), makeStats(statsOpt{dailyQty: 500}), config(), names, now)
	if rej != nil {
		t.Fatalf("本该通过,却被 %s 拒了", rej.Reason)
	}
	if opp.Confidence != Low {
		t.Fatalf("置信度 = %s,想要 low", opp.Confidence)
	}
}

func TestCrossedBookDowngradesWhenRejectionDisabled(t *testing.T) {
	cfg := config()
	cfg.Filters.RejectCrossedBook = false
	// 价差吃不过摩擦,所以仍然会被判不赚钱——但不是被 crossed_book 丢的
	if got := reason(t, makePrice(priceOpt{buy: 1190, sell: 1200}), makeStats(statsOpt{}), cfg); got != "unprofitable" {
		t.Fatalf("拒绝原因 = %s,想要 unprofitable", got)
	}
}

func TestMissingTimestampOnEitherSideStaysOffTheBoard(t *testing.T) {
	for _, tc := range []struct {
		name string
		opt  priceOpt
	}{
		{"买价没时间戳", priceOpt{noBuyStamp: true}},
		{"卖价没时间戳", priceOpt{noSellStamp: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reason(t, makePrice(tc.opt), makeStats(statsOpt{}), config()); got != "no_timestamp" {
				t.Fatalf("拒绝原因 = %s,想要 no_timestamp", got)
			}
		})
	}
}

// 同城也按周转挑执行方式,口径必须和跨城一致——
// 组合页把两者混在一起排名,一边"一天一轮"一边"按模式算",排出来的顺序没意义。
func TestSameCityUsesTheSharedTurnoverModel(t *testing.T) {
	opp, rej := Evaluate(makePrice(priceOpt{}), makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("本该通过,却被 %s 拒了", rej.Reason)
	}
	if opp.Mode == "" || opp.TurnsPerDay <= 0 || opp.HoursPerTurn <= 0 {
		t.Fatalf("没给出执行方式和周转:%+v", opp)
	}
	for _, m := range opp.Modes {
		t.Logf("  %s: 单件%.0f 一轮%.1fh %.1f轮/天 日收益%.0f",
			m.Label, m.ProfitPerUnit, m.HoursPerTurn, m.TurnsPerDay, m.DailyProfit)
	}
	// 同城秒买秒卖 = 按卖一买、按买一卖,必亏,不可能被选中
	if opp.Mode == "taker-taker" {
		t.Fatalf("同城选了秒买秒卖?那是按卖一买按买一卖,必亏")
	}
	// 选用的那个必须是日收益最高的
	if opp.DailyProfit+1e-6 < opp.Modes[0].DailyProfit {
		t.Fatalf("选用日收益 %v,但榜首是 %v", opp.DailyProfit, opp.Modes[0].DailyProfit)
	}
	// 日收益不能超过市场一天吃得下的量
	limit := opp.ProfitPerUnit * opp.AbsorbableQty
	if opp.DailyProfit > limit+1 {
		t.Fatalf("日收益 %v 超过市场容量 %v", opp.DailyProfit, limit)
	}
}
