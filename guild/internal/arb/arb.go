// Package arb 找跨城套利:在 A 城买,运到 B 城卖。
//
// Demo 把跨城排除在外,理由是"钱锁在路上"。这个理由只对了一半:
// 锁的是**时间**,不是钱的效率。跨城价差常常是同城的几倍,而且
// 秒买秒卖模式只收 4% 税——比同城双挂的 9% 低一半还多。
//
// 真正要正视的是另外两件事,这里都算进去了:
//
//  1. **两端都要有量。** 产地进得去、销地出得来,取两边的较小值。
//     只看一边是这类工具最常见的高估来源
//  2. **运输时间摊薄日化收益。** 一趟来回按 RoundTripHours 折算,
//     跑一趟赚 10 万但要两小时,不如同城一小时赚 6 万
//
// 风险(红区劫道、货砸手里)不建模成数字——那是玩家自己的判断,
// 工具能做的是把量、价、时间摆清楚。
package arb

import (
	"fmt"
	"math"
	"sort"
	"time"

	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
	"albion-guild/internal/histagg"
	"albion-guild/internal/screen"
)

// Market 是某物品在某城的一份快照,两边合起来就能算一条路线。
type Market struct {
	City     string
	Book     econ.Book
	Stats    *histagg.Stats
	AgeHours float64
}

// Route 是一条跨城路线。
type Route struct {
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	Quality  int    `json:"quality"`

	FromCity string `json:"from_city"`
	ToCity   string `json:"to_city"`

	// Mode 是这条路线上最划算的执行方式。跨城的答案经常是「秒买秒卖」,
	// 和同城不一样——因为价差大到足以盖过盘口损耗,而省下的 5% 手续费是实打实的
	Mode      string `json:"mode"`
	ModeLabel string `json:"mode_label"`

	BuyPrice  int64 `json:"buy_price"`
	SellPrice int64 `json:"sell_price"`

	CostPerUnit    float64 `json:"cost_per_unit"`
	RevenuePerUnit float64 `json:"revenue_per_unit"`
	ProfitPerUnit  float64 `json:"profit_per_unit"`
	Margin         float64 `json:"margin"`

	// SourceDaily/DestDaily 是两端各自的日均成交件数,
	// Qty 取两端可吃量和本金三者的最小值
	SourceDaily float64 `json:"source_daily_qty"`
	DestDaily   float64 `json:"dest_daily_qty"`
	Qty         int64   `json:"qty"`
	Bottleneck  string  `json:"bottleneck"` // capital | source | dest

	CapitalUsed  float64 `json:"capital_used"`
	TripProfit   float64 `json:"trip_profit"`
	DailyProfit  float64 `json:"daily_profit"`
	CapitalROI   float64 `json:"capital_roi"`
	TripsPerDay  float64 `json:"trips_per_day"`
	HoursPerTrip float64 `json:"hours_per_trip"`

	// Volatility 是销地的变异系数,ZScore 是销地当前价相对 30 日常态的位置。
	// 两个合起来回答"这个价差是结构性的,还是我赶上了一次抽风"
	Volatility float64 `json:"volatility"`
	ZScore     float64 `json:"z_score"`
	HasZ       bool    `json:"has_z"`

	MaxAgeHours float64           `json:"max_age_hours"`
	Confidence  screen.Confidence `json:"confidence"`
	Warnings    []string          `json:"warnings,omitempty"`

	// Modes 是这条路线上所有可行的执行方式,按单件利润排。
	// 界面全都显示出来:最赚的那个往往要两头等,用户可能宁愿要快的
	Modes []ModeQuote `json:"modes"`
}

// ModeQuote 是一条路线用某种执行方式的报价。
type ModeQuote struct {
	Mode          string  `json:"mode"`
	Label         string  `json:"label"`
	BuyPrice      int64   `json:"buy_price"`
	SellPrice     int64   `json:"sell_price"`
	ProfitPerUnit float64 `json:"profit_per_unit"`
	Margin        float64 `json:"margin"`
	Friction      float64 `json:"friction"`
	// 下面三个是把周转算进去之后的结果。**选哪个模式要看 DailyProfit,
	// 不是 ProfitPerUnit**——单件赚得少但一天能转八趟的,总量可能高得多
	HoursPerTrip float64 `json:"hours_per_trip"`
	TripsPerDay  float64 `json:"trips_per_day"`
	DailyProfit  float64 `json:"daily_profit"`

	unit econ.Unit
	qty  int64
}

// Options 控制路线怎么算。
type Options struct {
	Capital int64
	// TravelHours 是单程路上的时间,一趟来回按两倍算。
	TravelHours float64
	// FillHours 是**一条挂单腿**平均要等多久才成交。
	//
	// 秒买秒卖不用等,所以这个数只对挂单的腿生效。用同一个周转时间
	// 套所有执行方式是错的:挂买挂卖要等两次成交,是最慢的那个,
	// 却会凭空拿到和秒买秒卖一样的周转次数
	FillHours float64
	// MinProfitPerUnit 过滤掉单件赚不到几个银的路线,它们的运输时间不值
	MinProfitPerUnit float64
	MaxAgeHours      float64
	AbsorbRatio      float64
	// MinMargin 是毛利率下限。跨城要承担路上的风险,毛利太薄不值得跑
	MinMargin float64
	// MaxPriceRatio 兜住 troll:两地同一件货的价格比超过这个数,
	// 基本可以断定有一头是假单。**这个判断只看原始价格,和执行方式无关**——
	// 用毛利率来判会漏:同一对价格,不同执行方式算出的毛利差一截,
	// 挑最赚的那个模式反而最容易撞上限,整条路线就被误杀了
	MaxPriceRatio float64

	// 下面这几层同城早就有了,跨城一直没做。两种玩法混在同一张榜、
	// 同一个资金池里排名,只有一边做过滤等于没做——薄流动性品种的
	// 回报率往往最高,会排在最前面把钱拿走
	RejectCrossedBook    bool
	MinDailyVolumeSilver float64
	MinDaysWithData      int
	MaxHistoryGapDays    float64
}

// roundTripHours 是这种执行方式跑完一趟来回要多久。同城没有路程,
// 所以这个模型抽在 econ 里两边共用,免得同城跨城两套口径互相打架。
func (o Options) roundTripHours(m econ.Mode) float64 {
	return m.HoursPerRound(o.TravelHours, o.FillHours)
}

func DefaultOptions(cfg conf.Config) Options {
	return Options{
		Capital:              cfg.Capital,
		TravelHours:          cfg.Sizing.TravelHours,
		FillHours:            cfg.Sizing.FillHours,
		MinProfitPerUnit:     5,
		MaxAgeHours:          cfg.Freshness.MaxHours,
		AbsorbRatio:          cfg.Sizing.AbsorbRatio,
		MinMargin:            0.03,
		MaxPriceRatio:        3.0,
		RejectCrossedBook:    cfg.Filters.RejectCrossedBook,
		MinDailyVolumeSilver: cfg.Filters.MinDailyVolumeSilver,
		MinDaysWithData:      cfg.Filters.MinDaysWithData7d,
		MaxHistoryGapDays:    cfg.Filters.MaxHistoryGapDays,
	}
}

// Find 把一个物品在各城的快照两两配对,算出所有划算的路线。
//
// n 个城市是 n×(n-1) 个有向对。六个皇家城市就是 30 对,
// 乘上物品数也还是小数量级,直接全算,不用剪枝。
func Find(itemID, itemName string, quality int, markets []Market,
	cfg conf.Economics, opt Options, now time.Time) []Route {

	var out []Route
	for _, from := range markets {
		for _, to := range markets {
			if from.City == to.City {
				continue
			}
			if r, ok := evaluate(itemID, itemName, quality, from, to, cfg, opt, now); ok {
				out = append(out, r)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].DailyProfit > out[j].DailyProfit })
	return out
}

func evaluate(itemID, itemName string, quality int, from, to Market,
	cfg conf.Economics, opt Options, now time.Time) (Route, bool) {

	// 产地要有人卖给我,销地要有人接我的货
	if from.Book.SellMin <= 0 && from.Book.BuyMax <= 0 {
		return Route{}, false
	}
	if to.Book.SellMin <= 0 && to.Book.BuyMax <= 0 {
		return Route{}, false
	}
	maxAge := math.Max(from.AgeHours, to.AgeHours)
	if opt.MaxAgeHours > 0 && maxAge > opt.MaxAgeHours {
		return Route{}, false
	}

	// 交叉盘(买一 >= 卖一)在真实市场不可能持续存在,出现说明
	// 那一侧的两个价来自不同时间点。**这一层必须在算价格比之前**:
	// 一条陈旧的低价卖单会把中价拉下来,比值看着正常,却能造出
	// 10000% 毛利的路线还标成高可信
	if opt.RejectCrossedBook {
		if crossed(from.Book) || crossed(to.Book) {
			return Route{}, false
		}
	}

	// Troll 兜底看原始价格比,不看毛利率。
	// 用**同侧口径**比(产地卖一 vs 销地卖一),不用中价——
	// 中价会把交叉盘平均成一个看着正常的数
	if opt.MaxPriceRatio > 0 {
		if !ratioSane(from.Book, to.Book, opt.MaxPriceRatio) {
			return Route{}, false
		}
	}

	// 两端的历史都要能用。同城这几层早就有,跨城之前一层都没有
	if !usable(from.Stats, opt, now) || !usable(to.Stats, opt, now) {
		return Route{}, false
	}

	// 两端都要有量:产地进得去,销地也得出得来
	sourceDaily, destDaily := dailyQty(from.Stats), dailyQty(to.Stats)
	sourceCap := sourceDaily * opt.AbsorbRatio
	destCap := destDaily * opt.AbsorbRatio
	byMarket := math.Min(sourceCap, destCap)

	// 四种执行方式各算一遍完整的账,只在**通过约束的**里面挑最优。
	//
	// 两个关键点:
	//  1. 先挑最赚的再检查约束,会因为最赚那个不合规就把整条路线扔掉
	//  2. 挑的标准是**日收益**不是单件利润。挂买挂卖单件赚得最多,
	//     但要等两次成交,一天转不了几趟;秒买秒卖单件少一半,
	//     周转快起来总量可能反超
	var viable []ModeQuote
	for _, m := range econ.Modes {
		if !playable(m, from.Book, to.Book) {
			continue
		}
		u := econ.Quote(from.Book, to.Book, m, cfg)
		if u.ProfitPerUnit < opt.MinProfitPerUnit || u.Margin < opt.MinMargin {
			continue
		}
		q, trips, daily := econ.Turnover(u, byMarket, opt.Capital, opt.roundTripHours(m))
		if q < 1 {
			continue
		}
		viable = append(viable, ModeQuote{
			Mode: m.Key(), Label: m.Label(),
			BuyPrice: u.MyBid, SellPrice: u.MyAsk,
			ProfitPerUnit: u.ProfitPerUnit, Margin: u.Margin,
			Friction:     m.Friction(cfg),
			HoursPerTrip: opt.roundTripHours(m),
			TripsPerDay:  trips,
			DailyProfit:  daily,
			unit:         u, qty: q,
		})
	}
	if len(viable) == 0 {
		return Route{}, false
	}
	sort.SliceStable(viable, func(i, j int) bool {
		return viable[i].DailyProfit > viable[j].DailyProfit
	})
	best := viable[0]
	unit := best.unit
	qty := best.qty

	byCapital := 0.0
	if unit.CostPerUnit > 0 {
		byCapital = float64(opt.Capital) / unit.CostPerUnit
	}

	bottleneck := "capital"
	switch {
	case byCapital > byMarket && sourceCap <= destCap:
		bottleneck = "source"
	case byCapital > byMarket:
		bottleneck = "dest"
	}

	tripProfit := unit.ProfitPerUnit * float64(qty)

	r := Route{
		ItemID: itemID, ItemName: itemName, Quality: quality,
		FromCity: from.City, ToCity: to.City,
		Mode: best.Mode, ModeLabel: best.Label,
		BuyPrice: unit.MyBid, SellPrice: unit.MyAsk,
		CostPerUnit: unit.CostPerUnit, RevenuePerUnit: unit.RevenuePerUnit,
		ProfitPerUnit: unit.ProfitPerUnit, Margin: unit.Margin,
		SourceDaily: sourceDaily, DestDaily: destDaily,
		Qty: qty, Bottleneck: bottleneck,
		CapitalUsed:  float64(qty) * unit.CostPerUnit,
		TripProfit:   tripProfit,
		DailyProfit:  best.DailyProfit,
		TripsPerDay:  best.TripsPerDay,
		HoursPerTrip: best.HoursPerTrip,
		MaxAgeHours:  maxAge,
		Modes:        viable,
	}
	if opt.Capital > 0 {
		r.CapitalROI = best.DailyProfit / float64(opt.Capital)
	}
	if to.Stats != nil {
		r.Volatility = to.Stats.CV
		// 到这里已经排除过交叉盘,中价是可信的
		if z, ok := to.Stats.ZScore(float64(to.Book.SellMin+to.Book.BuyMax) / 2); ok {
			r.ZScore, r.HasZ = z, true
		}
	}
	r.Confidence, r.Warnings = judge(r, maxAge)
	return r, true
}

// playable 判断这个执行方式需要的价格是不是都有。
// 缺哪一侧,对应的腿就没法执行。
func playable(m econ.Mode, from, to econ.Book) bool {
	if m.Buy == econ.Taker && from.SellMin <= 0 {
		return false
	}
	if m.Buy == econ.Maker && from.BuyMax <= 0 {
		return false
	}
	if m.Sell == econ.Taker && to.BuyMax <= 0 {
		return false
	}
	if m.Sell == econ.Maker && to.SellMin <= 0 {
		return false
	}
	return true
}

// crossed 判断这一侧是不是交叉盘:有人出价比卖家要价还高。
// 真实市场会立刻自己成交掉,所以这是两侧快照时间不一致的强信号。
func crossed(b econ.Book) bool {
	return b.SellMin > 0 && b.BuyMax > 0 && b.BuyMax >= b.SellMin
}

// ratioSane 用同侧口径比两地价格。
//
// 卖一对卖一、买一对买一,两组都要落在合理区间。用中价比的话,
// 一条离谱的低价卖单会被同侧的正常买单平均掉,兜底就失效了。
func ratioSane(from, to econ.Book, maxRatio float64) bool {
	ok := false
	for _, pair := range [][2]int64{
		{from.SellMin, to.SellMin},
		{from.BuyMax, to.BuyMax},
	} {
		a, b := pair[0], pair[1]
		if a <= 0 || b <= 0 {
			continue
		}
		r := float64(b) / float64(a)
		if r > maxRatio || r < 1/maxRatio {
			return false
		}
		ok = true
	}
	// 一组可比的都没有(两边各缺一侧),没法判,保守起见不出这条路线
	return ok
}

// usable 判断这一端的成交历史够不够支撑一条路线。
// 和同城 screen 那几层对齐:流水下限、样本天数、历史新鲜度。
func usable(s *histagg.Stats, opt Options, now time.Time) bool {
	if s == nil {
		return false
	}
	if opt.MinDailyVolumeSilver > 0 && s.DailyVolumeSilver < opt.MinDailyVolumeSilver {
		return false
	}
	if opt.MinDaysWithData > 0 && s.DaysWithData7d < opt.MinDaysWithData {
		return false
	}
	if opt.MaxHistoryGapDays > 0 {
		age, ok := s.HistoryAgeDays(now)
		if !ok || age > opt.MaxHistoryGapDays {
			return false
		}
	}
	return true
}

func dailyQty(s *histagg.Stats) float64 {
	if s == nil {
		return 0
	}
	return s.DailyVolumeQty
}

// judge 给路线定可信度。跨城比同城多两个风险点:
// 销地价格可能只是一时的,以及两端快照可能不同时。
func judge(r Route, maxAge float64) (screen.Confidence, []string) {
	var warns []string
	level := screen.High

	if maxAge > 2 {
		level = screen.Medium
	}
	if r.HasZ && r.ZScore > 1.5 {
		warns = append(warns, fmt.Sprintf("销地现价高于 30 日常态 %.1fσ,可能是一时的", r.ZScore))
		level = screen.Low
	}
	if r.Volatility > 0.25 {
		warns = append(warns, fmt.Sprintf("销地价格波动大(变异系数 %.2f)", r.Volatility))
		if level == screen.High {
			level = screen.Medium
		}
	}
	if r.Bottleneck != "capital" {
		side := "产地"
		if r.Bottleneck == "dest" {
			side = "销地"
		}
		warns = append(warns, side+"成交量是瓶颈,本金没用满")
	}
	if maxAge > 4 {
		level = screen.Low
	}
	return level, warns
}
