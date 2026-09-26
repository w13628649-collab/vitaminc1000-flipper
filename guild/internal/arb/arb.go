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
//  3. **盘口深度(只有抓包那一边有)。** 挂单腿过和同城同一道闸门(screen.LegGate);
//     吃单腿沿阶梯逐档试边际价,吃到的件数当日容量——最优价只是第一件的价格
//
// 风险(红区劫道、货砸手里)不建模成数字——那是玩家自己的判断,
// 工具能做的是把量、价、时间摆清楚。唯一的例外是一端是 Brecilien 的路线:
// 要穿迷雾,路上时间单独配(sizing.brecilien_travel_hours),路线带
// risk_tags=["mists"],置信度最高 medium。
package arb

import (
	"fmt"
	"math"
	"slices"
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
	// Ask/Bid 是这座城卖单簿、买单簿各自用了谁的价、多旧、深度如何,和同城 screen
	// 同一套(scan 经 screen.ResolveSides 填)。数据龄就在 Side.AgeHours 里。
	// 零值就是纯 AODP:挂单腿闸门不触发,吃单腿按最优价算、不限件数,和以前逐位一致
	Ask, Bid screen.Side
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

	// BuyPrice/SellPrice 是我的买价和卖价。吃单腿沿抓包阶梯走过的,就是走到的
	// 最差那一档(限价):整轮都按它计价,只会低估不会高估
	BuyPrice  int64 `json:"buy_price"`
	SellPrice int64 `json:"sell_price"`

	CostPerUnit    float64 `json:"cost_per_unit"`
	RevenuePerUnit float64 `json:"revenue_per_unit"`
	ProfitPerUnit  float64 `json:"profit_per_unit"`
	Margin         float64 `json:"margin"`

	// SourceDaily/DestDaily 是两端各自的日均成交件数,
	// Qty 取两端可吃量、盘口深度和本金的最小值
	SourceDaily float64 `json:"source_daily_qty"`
	DestDaily   float64 `json:"dest_daily_qty"`
	Qty         int64   `json:"qty"`
	Bottleneck  string  `json:"bottleneck"` // capital | source | dest | depth

	// FromAsk/FromBid/ToAsk/ToBid 是两端四个盘口边各自用了谁的价、多旧、深度如何。
	// 没有价的那一边不输出
	FromAsk *screen.Side `json:"from_ask,omitempty"`
	FromBid *screen.Side `json:"from_bid,omitempty"`
	ToAsk   *screen.Side `json:"to_ask,omitempty"`
	ToBid   *screen.Side `json:"to_bid,omitempty"`
	// BuyLeg*/SellLeg* 是选中模式的两条腿各自依托哪一边:秒买吃产地 ask、
	// 挂买排在产地 bid,秒卖吃销地 bid、挂卖排在销地 ask。和同城机会同名同义
	BuyLegSource    string  `json:"buy_leg_source"`
	SellLegSource   string  `json:"sell_leg_source"`
	BuyLegAgeHours  float64 `json:"buy_leg_age_hours"`
	SellLegAgeHours float64 `json:"sell_leg_age_hours"`
	// BuyDepthQty/SellDepthQty 是吃单腿沿抓包阶梯走到限价那一档为止、盘口上一共
	// 挂着多少件,当日容量用(不假设当天会补货)。0 = 这条腿没按阶梯算:
	// 挂单腿,或那一边没有可信的抓包深度,这时容量只受成交量约束
	BuyDepthQty  int64 `json:"buy_depth_qty,omitempty"`
	SellDepthQty int64 `json:"sell_depth_qty,omitempty"`
	// DepthChecked 说明两条腿依托的那一边都有可信的抓包深度
	DepthChecked bool `json:"depth_checked"`

	CapitalUsed  float64 `json:"capital_used"`
	TripProfit   float64 `json:"trip_profit"`
	DailyProfit  float64 `json:"daily_profit"`
	CapitalROI   float64 `json:"capital_roi"`
	TripsPerDay  float64 `json:"trips_per_day"`
	HoursPerTrip float64 `json:"hours_per_trip"`
	// TravelHours 是这条路线单程路上按多少小时算(HoursPerTrip 里含两趟路程)。
	// 皇家城市之间是 sizing.travel_hours,一端是 Brecilien 时是 brecilien_travel_hours
	TravelHours float64 `json:"travel_hours"`

	// RiskTags 是模型里没折成数字的路上风险。目前只有 "mists":一端是 Brecilien,
	// 要穿迷雾,有被劫风险。带这个标记的路线置信度最高 medium
	RiskTags []string `json:"risk_tags,omitempty"`

	// Volatility 是销地的变异系数,ZScore 是销地当前价相对 30 日常态的位置。
	// 两个合起来回答"这个价差是结构性的,还是我赶上了一次抽风"
	Volatility float64 `json:"volatility"`
	ZScore     float64 `json:"z_score"`
	HasZ       bool    `json:"has_z"`

	MaxAgeHours float64           `json:"max_age_hours"`
	Confidence  screen.Confidence `json:"confidence"`
	Warnings    []string          `json:"warnings,omitempty"`

	// Modes 是这条路线上可行的执行方式,按日收益排,第一个就是选用的;
	// 后面跟着被深度闸门拦下的(Blocked 非空),账照算、不参与挑选。
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
	// Breakeven 是真实盈亏平衡价差,和同城一个口径(Friction 是名义和,偏小)
	Breakeven float64 `json:"breakeven"`
	// 下面三个是把周转算进去之后的结果。**选哪个模式要看 DailyProfit,
	// 不是 ProfitPerUnit**——单件赚得少但一天能转八趟的,总量可能高得多
	HoursPerTrip float64 `json:"hours_per_trip"`
	TripsPerDay  float64 `json:"trips_per_day"`
	DailyProfit  float64 `json:"daily_profit"`
	// Blocked 非空说明挂单腿被深度闸门拦下(no_bid_side / thin_book)
	Blocked string `json:"blocked,omitempty"`

	mode econ.Mode
	unit econ.Unit
	qty  int64
	// buyCap/sellCap 是吃单腿按阶梯走到的那一档为止的件数,没按阶梯算是 +Inf
	buyCap, sellCap float64
	// edge/warns 是深度闸门对这个模式的判定:贴着阈值、要降置信
	edge  bool
	warns []string
}

// Options 控制路线怎么算。
type Options struct {
	Capital int64
	// TravelHours 是单程路上的时间,一趟来回按两倍算。
	TravelHours float64
	// BrecilienTravelHours 是一端是 Brecilien 时的单程时间,0 = 和 TravelHours 一样。
	// 经迷雾比皇家城市之间走得久;默认值是估的,见 conf.Sizing.BrecilienTravelHours
	BrecilienTravelHours float64
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

	// Filters 给挂单腿的深度闸门(screen.LegGate)用,零值 = 闸门关
	Filters conf.Filters
	// DeviationMin/Max 是逐腿偏离检查:每条腿实际用到的价 / 该城 7 日均价
	// 要落在这个区间里。零值 = 关。
	//
	// MaxPriceRatio 只比两地同侧价格,遇到一侧为 0 就跳过——产地买一只剩一张
	// 2 银的占位单、销地只有卖单时,它一组都比不了、只能看另一组,挂买腿于是
	// 按 3 银成本算出天价毛利。同城 screen 早就对两边做偏离度,跨城一直没有
	DeviationMin, DeviationMax float64
	// MaxMargin 是同城 filters.max_margin 在跨城的对应,零值 = 关。判的不是原始毛利率,
	// 而是 excessMargin:扣掉两城 7 日均价本身的差距之后还剩的毛利率。
	//
	// 同城第五层补的缺口跨城一样有:两腿各自对本城均价都"合规"(买价 0.45×、卖价 2.4×),
	// 组合起来就是几倍的价差,MaxPriceRatio 比的是同侧价格,也可能两组都过。审查实测
	// T4_RUNE Fort Sterling → Caerleon 挂买挂卖 6 → 15、毛利 128%,两头都是 AODP、
	// 没有深度,排进了组合页第一。
	//
	// 以前跨城刻意不用毛利率判(见 docs/flipper.md「跨城」第 5 条):同一对价格换个
	// 执行方式毛利差一截,拿最赚的那个撞上限会误杀整条路线;而且城市之间本来就可能有
	// 结构性价差,原始毛利率超过 100% 未必是假单。所以这里两点都避开:按执行方式逐个判,
	// 只砍超了的那个模式;判之前先除掉两城均价之比,结构性价差不算"高得不真实"。
	// 两城是同一个均价时 excessMargin 就是毛利率本身,和同城第五层逐字同一个判据
	MaxMargin float64
}

// roundTripHours 是这种执行方式跑完一趟来回要多久。同城没有路程,
// 所以这个模型抽在 econ 里两边共用,免得同城跨城两套口径互相打架。
func (o Options) roundTripHours(m econ.Mode) float64 {
	return m.HoursPerRound(o.TravelHours, o.FillHours)
}

// travelHours 是 from → to 单程路上的时间。口径和 conf.Sizing.TravelHoursBetween 一致。
func (o Options) travelHours(from, to string) float64 {
	return conf.Sizing{TravelHours: o.TravelHours, BrecilienTravelHours: o.BrecilienTravelHours}.
		TravelHoursBetween(from, to)
}

// routeHours 是这条路线用这种执行方式跑完一趟来回要多久。
func (o Options) routeHours(m econ.Mode, from, to string) float64 {
	return m.HoursPerRound(o.travelHours(from, to), o.FillHours)
}

func DefaultOptions(cfg conf.Config) Options {
	return Options{
		Capital:              cfg.Capital,
		TravelHours:          cfg.Sizing.TravelHours,
		BrecilienTravelHours: cfg.Sizing.BrecilienTravelHours,
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
		Filters:              cfg.Filters,
		DeviationMin:         cfg.Filters.DeviationMin,
		DeviationMax:         cfg.Filters.DeviationMax,
		MaxMargin:            cfg.Filters.MaxMargin,
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
	// 三个关键点:
	//  1. 先挑最赚的再检查约束,会因为最赚那个不合规就把整条路线扔掉
	//  2. 挑的标准是**日收益**不是单件利润。挂买挂卖单件赚得最多,
	//     但要等两次成交,一天转不了几趟;秒买秒卖单件少一半,
	//     周转快起来总量可能反超
	//  3. 深度闸门只砍掉被拦的那个模式,不连带整条路线:产地买方太薄只说明
	//     挂买收不到货,秒买照样能做
	var viable, blocked []ModeQuote
	for _, m := range econ.Modes {
		if !playable(m, from.Book, to.Book) {
			continue
		}
		q, ok := bestFill(m, from, to, byMarket, cfg, opt)
		if !ok {
			continue
		}
		// 扣掉两城均价之差还高得不真实:这个模式依托的那两边多半有一头是假单或旧单。
		// 只砍这个模式,别的模式用的是另外两边,照样能做
		if opt.MaxMargin > 0 && excessMargin(q.Margin, from.Stats, to.Stats) > opt.MaxMargin {
			continue
		}
		// 挂买排在产地的买单簿上,挂卖排在销地的卖单簿上
		q.Blocked, _, q.edge, q.warns = screen.LegGate(m, from.Bid, to.Ask, opt.Filters)
		if q.Blocked != "" {
			blocked = append(blocked, q)
		} else {
			viable = append(viable, q)
		}
	}
	if len(viable) == 0 {
		return Route{}, false
	}
	byDaily := func(s []ModeQuote) {
		sort.SliceStable(s, func(i, j int) bool { return s[i].DailyProfit > s[j].DailyProfit })
	}
	byDaily(viable)
	byDaily(blocked)
	best := viable[0]
	unit := best.unit
	qty := best.qty

	byCapital := 0.0
	if unit.CostPerUnit > 0 {
		byCapital = float64(opt.Capital) / unit.CostPerUnit
	}

	// 盘口深度比成交量和本金都紧,才算深度是瓶颈。没按阶梯算的腿是 +Inf,不参与
	depthCap := math.Min(best.buyCap, best.sellCap)
	bottleneck := "capital"
	switch {
	case depthCap < byMarket && depthCap < byCapital:
		bottleneck = "depth"
	case byCapital > byMarket && sourceCap <= destCap:
		bottleneck = "source"
	case byCapital > byMarket:
		bottleneck = "dest"
	}

	tripProfit := unit.ProfitPerUnit * float64(qty)

	fromSides := screen.Sides{Ask: from.Ask, Bid: from.Bid}
	toSides := screen.Sides{Ask: to.Ask, Bid: to.Bid}
	buyLeg, sellLeg := screen.LegSides(best.mode, fromSides, toSides)

	r := Route{
		ItemID: itemID, ItemName: itemName, Quality: quality,
		FromCity: from.City, ToCity: to.City,
		Mode: best.Mode, ModeLabel: best.Label,
		BuyPrice: unit.MyBid, SellPrice: unit.MyAsk,
		CostPerUnit: unit.CostPerUnit, RevenuePerUnit: unit.RevenuePerUnit,
		ProfitPerUnit: unit.ProfitPerUnit, Margin: unit.Margin,
		SourceDaily: sourceDaily, DestDaily: destDaily,
		Qty: qty, Bottleneck: bottleneck,
		FromAsk: sideRef(from.Ask), FromBid: sideRef(from.Bid),
		ToAsk: sideRef(to.Ask), ToBid: sideRef(to.Bid),
		BuyLegSource: buyLeg.Source, SellLegSource: sellLeg.Source,
		BuyLegAgeHours: buyLeg.AgeHours, SellLegAgeHours: sellLeg.AgeHours,
		BuyDepthQty: capQty(best.buyCap), SellDepthQty: capQty(best.sellCap),
		// 挂单腿看的是闸门那一边,吃单腿看的是阶梯那一边——都是 LegSides 映射到的那一边
		DepthChecked: buyLeg.Captured() && sellLeg.Captured(),
		CapitalUsed:  float64(qty) * unit.CostPerUnit,
		TripProfit:   tripProfit,
		DailyProfit:  best.DailyProfit,
		TripsPerDay:  best.TripsPerDay,
		HoursPerTrip: best.HoursPerTrip,
		TravelHours:  opt.travelHours(from.City, to.City),
		MaxAgeHours:  maxAge,
		Modes:        append(viable, blocked...),
	}
	if conf.ViaMists(from.City, to.City) {
		r.RiskTags = []string{RiskMists}
	}
	if opt.Capital > 0 {
		r.CapitalROI = best.DailyProfit / float64(opt.Capital)
	}
	if to.Stats != nil {
		r.Volatility = to.Stats.CV
		// 销地两侧都有价,中价才有意义。只有卖单时 (卖一 + 0) / 2 是个假中价,
		// 会算出一个离谱的负 z,看着像"现在便宜、是买点"。
		// 交叉盘在前面已经排除(RejectCrossedBook 开着时)
		if to.Book.SellMin > 0 && to.Book.BuyMax > 0 {
			if z, ok := to.Stats.ZScore(float64(to.Book.SellMin+to.Book.BuyMax) / 2); ok {
				r.ZScore, r.HasZ = z, true
			}
		}
	}
	r.Confidence, r.Warnings = judge(r, maxAge)
	// 选中模式的挂单腿贴着深度阈值:和同城一样降为 low
	if best.edge {
		r.Confidence = screen.Low
	}
	r.Warnings = append(r.Warnings, best.warns...)
	return r, true
}

// rung 是吃单腿阶梯的一个前缀:一路吃到 price 这一档为止,一共 qty 件。
type rung struct {
	price int64
	qty   float64
}

// ladder 列出一条腿的候选前缀。只有吃单腿、而且那一边有可信的抓包深度时才逐档走;
// 否则只有一个候选:最优价、不限件数——没有阶梯时整个循环退化成单次,和以前逐位一致。
func ladder(s screen.Side, walk bool, top int64) []rung {
	if walk && s.Captured() {
		out := make([]rung, 0, len(s.Levels))
		cum := 0.0
		for _, l := range s.Levels {
			if l.Qty <= 0 || l.Price <= 0 {
				continue
			}
			cum += float64(l.Qty)
			out = append(out, rung{price: l.Price, qty: cum})
		}
		if len(out) > 0 {
			return out
		}
	}
	return []rung{{price: top, qty: math.Inf(1)}}
}

// bestFill 算一个执行方式在这两端能做成什么样:吃单腿沿阶梯逐档试边际价,
// 挑日收益最高的那个前缀。不可行(价不合理、不赚钱、一件都做不成)返回 false。
//
// 口径:
//   - 整轮按吃到的最差那一档计价(不是 VWAP),只会低估
//   - 吃到的件数当**日容量**,和成交量、本金一起取最小,不假设当天会补货
//   - 日收益并列时取浅的:同样的钱,少吃几档少冒一分险
//
// 剪枝:
//   - 某个前缀不赚钱或毛利不够,更深的只会更差(买价只升、卖价只降)。卖侧第一档
//     就不合格时,更深的买价也救不回来,整个模式到此为止。这一条是精确的
//   - 偏离度那一关不完全单调:最优档离谱地便宜、更深几档反而正常时,这里也收手。
//     是故意的——最优档本身离谱,多半是 troll 或幽灵单,这一整边都不该信
//   - 容量已经顶到别的约束,再往深吃容量不涨、价只更差。这一条也是精确的
//     (有测试拿暴力枚举对过)
func bestFill(m econ.Mode, from, to Market, byMarket float64, cfg conf.Economics, opt Options) (ModeQuote, bool) {
	buys := ladder(from.Ask, m.Buy == econ.Taker, from.Book.SellMin)
	sells := ladder(to.Bid, m.Sell == econ.Taker, to.Book.BuyMax)
	hours := opt.routeHours(m, from.City, to.City)

	var best ModeQuote
	found := false
	for _, b := range buys {
		bk := from.Book
		if m.Buy == econ.Taker {
			bk.SellMin = b.price
		}
		for j, s := range sells {
			sk := to.Book
			if m.Sell == econ.Taker {
				sk.BuyMax = s.price
			}
			u := econ.Quote(bk, sk, m, cfg)
			if !legsSane(m, bk, sk, from.Stats, to.Stats, opt) ||
				u.ProfitPerUnit < opt.MinProfitPerUnit || u.Margin < opt.MinMargin {
				if j == 0 {
					return best, found
				}
				break
			}
			absorbable := math.Min(byMarket, math.Min(b.qty, s.qty))
			q, trips, daily := econ.Turnover(u, absorbable, opt.Capital, hours)
			if q >= 1 && (!found || daily > best.DailyProfit) {
				found = true
				best = ModeQuote{
					Mode: m.Key(), Label: m.Label(),
					BuyPrice: u.MyBid, SellPrice: u.MyAsk,
					ProfitPerUnit: u.ProfitPerUnit, Margin: u.Margin,
					Friction:     m.Friction(cfg),
					Breakeven:    m.Breakeven(cfg),
					HoursPerTrip: hours,
					TripsPerDay:  trips,
					DailyProfit:  daily,
					mode:         m, unit: u, qty: q,
					buyCap: b.qty, sellCap: s.qty,
				}
			}
			if s.qty >= math.Min(byMarket, b.qty) {
				break
			}
		}
		// 卖侧最深的前缀也就这么多件:买侧已经不比它少,再往深买容量不涨
		if b.qty >= math.Min(byMarket, sells[len(sells)-1].qty) {
			break
		}
	}
	return best, found
}

// legsSane 是逐腿偏离检查:每条腿实际依托的价 / 该城 7 日均价要落在
// [DeviationMin, DeviationMax] 内。秒买依托产地卖一、挂买依托产地买一,
// 秒卖依托销地买一、挂卖依托销地卖一。7 日均价没有时跳过这一腿——
// 历史闸门(usable)已经管了"有没有历史",这里只管"价离不离谱"。
func legsSane(m econ.Mode, buy, sell econ.Book, fromStats, toStats *histagg.Stats, opt Options) bool {
	if opt.DeviationMin <= 0 && opt.DeviationMax <= 0 {
		return true
	}
	bp := buy.BuyMax
	if m.Buy == econ.Taker {
		bp = buy.SellMin
	}
	sp := sell.SellMin
	if m.Sell == econ.Taker {
		sp = sell.BuyMax
	}
	return legSane(bp, fromStats, opt) && legSane(sp, toStats, opt)
}

func legSane(price int64, s *histagg.Stats, opt Options) bool {
	if s == nil || s.AvgPrice7d <= 0 || price <= 0 {
		return true
	}
	r := float64(price) / s.AvgPrice7d
	if opt.DeviationMin > 0 && r < opt.DeviationMin {
		return false
	}
	if opt.DeviationMax > 0 && r > opt.DeviationMax {
		return false
	}
	return true
}

// excessMargin 是扣掉两城 7 日均价本身的差距之后还剩的毛利率:成本、收入各自
// 除以本城 7 日均价再算,等于 (1+毛利率) × 产地均价 / 销地均价 − 1。
//
// 销地均价本来就是产地的 2.5 倍时,128% 的毛利只是"按历史价搬过去",扣完是负的;
// 两城均价差不多时,128% 就是实打实的 128%,和同城一样高得不真实。
// 任一端没有 7 日均价时退回原始毛利率:判不了结构性价差,宁可严。
func excessMargin(margin float64, fromStats, toStats *histagg.Stats) float64 {
	if fromStats == nil || toStats == nil || fromStats.AvgPrice7d <= 0 || toStats.AvgPrice7d <= 0 {
		return margin
	}
	return (1+margin)*fromStats.AvgPrice7d/toStats.AvgPrice7d - 1
}

// sideRef 给 JSON 用:没有价的那一边不输出。
func sideRef(s screen.Side) *screen.Side {
	if s.Source == "" {
		return nil
	}
	return &s
}

// capQty 把阶梯容量转成件数,没按阶梯算(+Inf)的是 0。
func capQty(c float64) int64 {
	if math.IsInf(c, 1) {
		return 0
	}
	return int64(c)
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
	switch r.Bottleneck {
	case "capital":
	case "depth":
		// 阶梯是某一眼的快照,吃掉之后多久补上完全不知道——按日容量算已经是保守口径,
		// 但真去做的人得知道"一天就这么多"
		warns = append(warns, fmt.Sprintf("盘口深度是瓶颈:按当前挂单只够 %s 件,补货速度未知", screen.Thousands(depthQty(r))))
	default:
		side := "产地"
		if r.Bottleneck == "dest" {
			side = "销地"
		}
		warns = append(warns, side+"成交量是瓶颈,本金没用满")
	}
	if maxAge > 4 {
		level = screen.Low
	}
	// 迷雾里能被劫。被劫概率没法折成数字(看时段、看人、看带多少货),
	// 所以不改收益,只提醒、把置信度压到 medium:榜上看着再好也要自己掂量
	if slices.Contains(r.RiskTags, RiskMists) {
		warns = append(warns, fmt.Sprintf("经迷雾,有被劫风险;路上按单程 %.1f 小时估,没实测", r.TravelHours))
		if level == screen.High {
			level = screen.Medium
		}
	}
	return level, warns
}

// RiskMists 是"路线要穿迷雾"的风险标记:一端是 Brecilien。
const RiskMists = "mists"

// depthQty 是两条腿里被阶梯限住的那个件数(较小的非零值)。
func depthQty(r Route) int64 {
	switch {
	case r.BuyDepthQty > 0 && r.SellDepthQty > 0:
		return min(r.BuyDepthQty, r.SellDepthQty)
	case r.BuyDepthQty > 0:
		return r.BuyDepthQty
	default:
		return r.SellDepthQty
	}
}
