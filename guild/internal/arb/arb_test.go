package arb

import (
	"testing"

	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
	"albion-guild/internal/histagg"
	"albion-guild/internal/screen"
)

func market(city string, sellMin, buyMax int64, dailyQty float64) Market {
	return Market{
		City: city,
		Book: econ.Book{SellMin: sellMin, BuyMax: buyMax},
		Stats: &histagg.Stats{
			DailyVolumeQty: dailyQty,
			AvgPrice30d:    float64(sellMin+buyMax) / 2,
			StdDev30d:      float64(sellMin+buyMax) / 2 * 0.05,
			CV:             0.05,
		},
		AgeHours: 1,
	}
}

func opts() Options {
	o := DefaultOptions(conf.Default())
	o.Capital = 10_000_000
	return o
}

func find(markets ...Market) []Route {
	return Find("T5_CLOTH", "精布", 1, markets, conf.Default().Economics, opts())
}

func TestRoutesAreDirectional(t *testing.T) {
	routes := find(
		market("Lymhurst", 1000, 950, 5000),  // 产地便宜
		market("Martlock", 1600, 1500, 5000), // 销地贵
	)
	if len(routes) == 0 {
		t.Fatal("没找到任何路线")
	}
	best := routes[0]
	if best.FromCity != "Lymhurst" || best.ToCity != "Martlock" {
		t.Fatalf("最优路线是 %s → %s,应该是从便宜的城往贵的城走", best.FromCity, best.ToCity)
	}
	// 反向那条应该亏钱,直接被过滤掉
	for _, r := range routes {
		if r.FromCity == "Martlock" && r.ToCity == "Lymhurst" {
			t.Fatalf("反向路线不该出现:单件利润 %v", r.ProfitPerUnit)
		}
	}
}

// 两端都要有量。只看一边是这类工具最常见的高估来源。
func TestQuantityIsLimitedByBothEnds(t *testing.T) {
	routes := find(
		market("Lymhurst", 1000, 950, 100_000), // 产地量大
		market("Martlock", 1600, 1500, 500),    // 销地量小
	)
	if len(routes) == 0 {
		t.Fatal("没找到路线")
	}
	r := routes[0]
	// 销地日均 500 件 × 20% = 100 件,产地再多也没用
	if r.Qty > 100 {
		t.Fatalf("可吃量 %d,销地只能吃下 100 件", r.Qty)
	}
	if r.Bottleneck != "dest" {
		t.Fatalf("瓶颈判成了 %s,应该是销地", r.Bottleneck)
	}
}

func TestSourceCanAlsoBeTheBottleneck(t *testing.T) {
	routes := find(
		market("Lymhurst", 1000, 950, 400),      // 产地量小
		market("Martlock", 1600, 1500, 100_000), // 销地量大
	)
	if routes[0].Bottleneck != "source" {
		t.Fatalf("瓶颈判成了 %s,应该是产地", routes[0].Bottleneck)
	}
}

// 跨城秒买秒卖只收 4% 税。价差够大时它常常胜过挂单模式,
// 因为不用在两头各等一次成交。
func TestInstantExecutionIsViableCrossCity(t *testing.T) {
	routes := find(
		market("Lymhurst", 1000, 900, 50_000),
		market("Martlock", 2000, 1900, 50_000),
	)
	r := routes[0]
	t.Logf("最优:%s %s→%s 买%d 卖%d 单件%.0f 毛利%.1f%% 可吃%d件 日收益%.0f",
		r.ModeLabel, r.FromCity, r.ToCity, r.BuyPrice, r.SellPrice,
		r.ProfitPerUnit, r.Margin*100, r.Qty, r.DailyProfit)
	if r.ProfitPerUnit <= 0 {
		t.Fatal("这么大的价差应该是赚的")
	}
}

// 运输和等待时间只在本金撑不满市场容量时才影响日收益。
// 这是跨城和同城最大的模型差别,值得单独钉住。
func TestTripTimeMattersOnlyWhenCapitalBound(t *testing.T) {
	// 场景一:市场很大、本金很小 → 必须多跑几趟,跑得快就赚得多
	big := []Market{
		market("Lymhurst", 1000, 950, 500_000),
		market("Martlock", 1600, 1500, 500_000),
	}
	fast, slow := opts(), opts()
	fast.TravelHours, fast.FillHours = 0.25, 1
	slow.TravelHours, slow.FillHours = 3, 6

	f := Find("T5_CLOTH", "精布", 1, big, conf.Default().Economics, fast)[0]
	s := Find("T5_CLOTH", "精布", 1, big, conf.Default().Economics, slow)[0]
	if !(f.DailyProfit > s.DailyProfit) {
		t.Fatalf("本金受限时,跑得快 %v 应该高于跑得慢 %v", f.DailyProfit, s.DailyProfit)
	}

	// 场景二:市场很小、本金充裕 → 一趟就搬完一天的量,速度不影响
	small := []Market{
		market("Lymhurst", 1000, 950, 300),
		market("Martlock", 1600, 1500, 300),
	}
	f2 := Find("T5_CLOTH", "精布", 1, small, conf.Default().Economics, fast)[0]
	s2 := Find("T5_CLOTH", "精布", 1, small, conf.Default().Economics, slow)[0]
	if f2.DailyProfit != s2.DailyProfit {
		t.Fatalf("市场受限时速度不该有影响:%v vs %v", f2.DailyProfit, s2.DailyProfit)
	}
	// 而且不能超过市场一天能吃下的总量
	if limit := f2.ProfitPerUnit * 300 * 0.2; f2.DailyProfit > limit+1 {
		t.Fatalf("日收益 %v 超过市场容量 %v", f2.DailyProfit, limit)
	}
}

// 挂单腿要等成交,所以挂买挂卖一趟最久、秒买秒卖最快。
// 用同一个周转时间套所有模式,会让最慢的那个凭空多出几倍周转。
func TestMakerLegsCostTime(t *testing.T) {
	o := opts()
	o.TravelHours, o.FillHours = 0.5, 4
	want := map[string]float64{
		"taker-taker": 1, "taker-maker": 5, "maker-taker": 5, "maker-maker": 9,
	}
	for _, m := range econ.Modes {
		if got := o.roundTripHours(m); got != want[m.Key()] {
			t.Fatalf("%s 一趟 %v 小时,想要 %v", m.Label(), got, want[m.Key()])
		}
	}
}

// 选哪个执行方式看的是日收益,不是单件利润。
// 单件赚得少但周转快的,总量可能反超。
func TestModeChosenByDailyProfitNotUnitProfit(t *testing.T) {
	// 市场极大(本金才是瓶颈)→ 周转次数直接决定日收益
	markets := []Market{
		market("Lymhurst", 1000, 900, 10_000_000),
		market("Martlock", 1600, 1500, 10_000_000),
	}
	o := opts()
	o.TravelHours, o.FillHours = 0.25, 8 // 挂单等得久,秒单几乎不用等
	r := Find("T5_CLOTH", "精布", 1, markets, conf.Default().Economics, o)[0]

	for _, m := range r.Modes {
		t.Logf("  %s: 单件%.0f 一趟%.1fh %.1f趟/天 日收益%.0f",
			m.Label, m.ProfitPerUnit, m.HoursPerTrip, m.TripsPerDay, m.DailyProfit)
	}
	// Modes 已按日收益降序,选用的必须是第一个
	if r.Mode != r.Modes[0].Mode {
		t.Fatalf("选用了 %s,但日收益最高的是 %s", r.Mode, r.Modes[0].Mode)
	}
	for _, m := range r.Modes {
		if m.DailyProfit > r.DailyProfit+1e-6 {
			t.Fatalf("%s 日收益 %v 高于选用的 %v", m.Label, m.DailyProfit, r.DailyProfit)
		}
	}
	// 这个场景下慢工出细活的挂买挂卖不该赢
	if r.Mode == "maker-maker" {
		t.Errorf("等 8 小时一条腿还选挂买挂卖?日收益 %v", r.DailyProfit)
	}
}

// 销地价格正处在历史高位时要提醒——这个价差可能是一时的。
func TestElevatedDestinationPriceIsFlagged(t *testing.T) {
	dest := market("Martlock", 1600, 1500, 5000)
	dest.Stats.AvgPrice30d = 1000 // 常态 1000,现在 1550
	dest.Stats.StdDev30d = 100
	dest.Stats.CV = 0.1

	routes := find(market("Lymhurst", 1000, 950, 5000), dest)
	r := routes[0]
	if !r.HasZ || r.ZScore < 1.5 {
		t.Fatalf("z-score = %v,应该明显为正", r.ZScore)
	}
	if r.Confidence != screen.Low {
		t.Fatalf("可信度 = %s,销地价在历史高位时应该降级", r.Confidence)
	}
	if len(r.Warnings) == 0 {
		t.Fatal("应该给出提示")
	}
}

// 两地价格比离谱时基本可以断定有一头是假单。
// 注意这个判断只看原始价格——用毛利率判会误杀真实的宽价差路线。
func TestImplausiblePriceRatioIsRejected(t *testing.T) {
	routes := find(
		market("Lymhurst", 100, 90, 5000),    // 假的低价
		market("Martlock", 9000, 8000, 5000), // 假的高价
	)
	if len(routes) != 0 {
		t.Fatalf("80 倍价差应该被当成 troll 拦掉,却出了 %d 条路线", len(routes))
	}
	// 但 2 倍价差是真实存在的,不能一起误杀
	real := find(market("Lymhurst", 1000, 900, 5000), market("Martlock", 2000, 1900, 5000))
	if len(real) == 0 {
		t.Fatal("2 倍价差是正常的跨城行情,不该被拦")
	}
}

func TestStaleSnapshotsAreRejected(t *testing.T) {
	from := market("Lymhurst", 1000, 950, 5000)
	to := market("Martlock", 1600, 1500, 5000)
	to.AgeHours = 12
	if len(find(from, to)) != 0 {
		t.Fatal("12 小时前的快照不该出路线")
	}
}
