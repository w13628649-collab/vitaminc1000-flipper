// Package screen 是 troll 过滤——自己写价差工具时最容易漏、代价最高的一环。
//
// 根本原因:AODP 的 prices 端点**不返回挂单数量**,只有价格。
// 市场上大量恶意挂单——1 件货、价格是正常值几十倍的卖单,或极低的买单,
// 专门污染数据。天真的计算器会把它们显示成天大的机会,买了就套死。
//
// 价格已经由融合层逐边选好(抓包或 AODP),下面每一层都作用在融合后的价上。
// 按顺序判,第一层不过就拒,全部通过才进主榜:
//
//	| 层        | 规则                                                        | 拒绝原因 |
//	|-----------|-------------------------------------------------------------|----------|
//	| 双边存在  | 买价和卖价都要有,单边不进主榜                              | one_sided |
//	| 新鲜度    | 两侧都有时间戳、不超前、距今 ≤ MaxHours                     | no_timestamp / future_timestamp / stale |
//	| 历史可用  | 有成交历史、不太旧、有 7 日均价、样本天数够                 | no_history / stale_history / no_baseline / thin_history |
//	| 偏离度    | 价格 / 7 日均价 落在 [DeviationMin, DeviationMax] 外        | deviation |
//	| 成交量    | 日均成交件数 × 均价 < MinDailyVolumeSilver                  | low_volume |
//	| 价差上限  | 卖一 / 买一 − 1 > MaxSpreadPct(默认关)                     | wide_spread |
//	| 交叉盘    | 买一 ≥ 卖一                                                 | crossed_book |
//	| 深度闸门  | 四种执行方式各过一遍 LegGate,只拦挂单腿、只看抓包那一边;   | no_bid_side / thin_book |
//	|           | 赚钱的模式全被拦下时,报日收益最高那个被拦的原因            |          |
//	| 利润      | 四种执行方式按盘口价没一种税后赚钱                          | unprofitable |
//	| 毛利率    | 按盘口价挑中的那个 Margin > MaxMargin,两边多半同时是 troll  | implausible_margin |
//	| 带宽      | 挂单腿按带宽钳过之后没一种赚钱                              | unprofitable |
//	| 吃单量    | 可吃量不足 1 件                                             | too_thin |
//
// 挂单腿带宽(filters.maker_price_band,econ.QuoteBanded):吃单腿的价是盘口上真挂着的单,
// 挂单腿的价是假设有人按它成交。挂卖价最多到参考价 ×(1 + band),挂买价至少出到
// ×(1 − band);参考价是 7 日成交均价,盘口整体越过均价时跟着对面盘口走。税费、盈亏平衡、
// 利润、日收益全按钳过的价算。钳制只让账变差,不放行任何东西。
//
// 深度闸门、利润、毛利率上限这三层先按**不钳**的账判,也就是 band = 0 时的判定原样复现:
// 它们判的是盘口本身,同一本盘口带宽开着关着、开多宽都得是同一个拒绝原因。
// 过了这三层,再按钳过的账挑执行方式。
//
// 通过之后定置信度:贴着任何一道阈值(含深度闸门的 edge)、或者两条挂单腿都被封
// (两个挂价都是按带宽推出来的)→ low;否则两侧都在 HighConfidenceHours 内 → high;其余 medium。
package screen

import (
	"fmt"
	"math"
	"sort"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/econ"
	"albion-guild/internal/histagg"
)

// 距离阈值多近算"边缘",触发 confidence 降级
const (
	edgeMargin           = 0.15
	volumeEdgeMultiplier = 1.25
)

type Confidence string

const (
	High   Confidence = "high"
	Medium Confidence = "medium"
	Low    Confidence = "low"
)

// Namer 只要能把 item id 翻成显示名就行。
// 这样 screen 不必依赖整个目录包。
type Namer interface {
	NameOf(itemID string) string
}

// Opportunity 是一条同城价差机会。
type Opportunity struct {
	ItemID   string `json:"item_id"`
	ItemName string `json:"item_name"`
	City     string `json:"city"`
	Quality  int    `json:"quality"`

	// BuyPrice/SellPrice 是市场快照里现有的最高买单价 / 最低卖单价。
	BuyPrice  int64 `json:"buy_price"`
	SellPrice int64 `json:"sell_price"`
	// MyBid/MyAsk 是我实际要挂的价:压过现价一档,再按 7 日成交均价做带宽钳制
	// (filters.maker_price_band,见 econ.QuoteBanded)。被钳过时 Clamp 非空,
	// 里面是钳之前的价和参考价
	MyBid int64 `json:"my_bid"`
	MyAsk int64 `json:"my_ask"`
	// Clamp 是选中执行方式的挂单腿有没有被封、封之前是多少。nil = 两条腿都是盘口价
	Clamp *econ.Clamp `json:"clamp,omitempty"`

	CostPerUnit    float64 `json:"cost_per_unit"`
	RevenuePerUnit float64 `json:"revenue_per_unit"`
	ProfitPerUnit  float64 `json:"profit_per_unit"`
	Margin         float64 `json:"margin"`

	DailyVolumeQty    float64 `json:"daily_volume_qty"`
	DailyVolumeSilver float64 `json:"daily_volume_silver"`
	AvgPrice7d        float64 `json:"avg_price_7d"`
	AvgPrice30d       float64 `json:"avg_price_30d"`

	AbsorbableQty float64 `json:"absorbable_qty"`
	Qty           int64   `json:"qty"`
	CapitalUsed   float64 `json:"capital_used"`
	DailyProfit   float64 `json:"daily_profit"`
	DailyROI      float64 `json:"daily_roi"`
	// CapitalROI 是 DailyProfit / 总本金。真正衡量"这条机会对我 1000 万
	// 有多大意义"的指标。
	CapitalROI float64 `json:"capital_roi"`

	// Volatility 是 30 日价格变异系数,ZScore 是当前中价相对 30 日常态的位置。
	// 同样的价差,波动大的品种赚到的概率低得多;z 正得多说明现在偏贵,
	// 这个价差可能只是一时的
	Volatility float64 `json:"volatility"`
	ZScore     float64 `json:"z_score"`
	HasZ       bool    `json:"has_z"`
	// Modes 是四种执行方式各自的报价。Demo 只算「挂买挂卖」一种,
	// 但秒买秒卖只收 4% 税,盘口宽的时候未必更差
	Modes []ModeQuote `json:"modes"`
	// Mode 是选用的那个。同城的答案几乎总是挂买挂卖——
	// 秒买秒卖在同城等于"按卖一买、按买一卖",必亏
	Mode         string  `json:"mode"`
	ModeLabel    string  `json:"mode_label"`
	TurnsPerDay  float64 `json:"turns_per_day"`
	HoursPerTurn float64 `json:"hours_per_turn"`

	BuyAgeHours  float64    `json:"buy_age_hours"`
	SellAgeHours float64    `json:"sell_age_hours"`
	DataAgeHours float64    `json:"data_age_hours"`
	Confidence   Confidence `json:"confidence"`

	// Ask/Bid 是卖单簿、买单簿两边各自用了谁的价。和 buy_age_hours/sell_age_hours
	// 同一个口径:ask 对应 SellPrice,bid 对应 BuyPrice
	Ask Side `json:"ask"`
	Bid Side `json:"bid"`
	// BuyLeg*/SellLeg* 是选中那个执行方式的两条腿各自依托哪一边:
	// 秒买吃 ask、挂买排在 bid 上,秒卖吃 bid、挂卖排在 ask 上。
	// 名字刻意和 ask/bid 分开——"买"指交易腿,不指买单簿
	BuyLegSource    string  `json:"buy_leg_source"`
	SellLegSource   string  `json:"sell_leg_source"`
	BuyLegAgeHours  float64 `json:"buy_leg_age_hours"`
	SellLegAgeHours float64 `json:"sell_leg_age_hours"`
	// DepthChecked 说明选中模式两条腿依托的那一边都有可信的抓包深度,深度闸门
	// 真的判过。false 不等于"深度不够",而是"不知道"——界面要提示回游戏里翻一眼
	DepthChecked bool `json:"depth_checked"`
	// FillPosition 是 7 日均价落在买一和卖一之间的位置(0 贴买价、1 贴卖价),
	// HasFillPosition 为 false 时没法算(比如交叉盘)。**只展示,不参与任何判定**:
	// AODP 的历史只统计卖单成交,均价天生偏向卖价,拿它当闸门会误杀一大片
	FillPosition    float64 `json:"fill_position"`
	HasFillPosition bool    `json:"has_fill_position"`
	// Warnings 是贴近过滤阈值的项。这条之所以还在榜上,是因为差一点点才被拦掉。
	Warnings []string `json:"warnings,omitempty"`
	// Hints 是中性信息,不影响可信度。
	Hints []string `json:"hints,omitempty"`
}

// ModeQuote 是某种执行方式下的报价。
type ModeQuote struct {
	Mode          string  `json:"mode"`
	Label         string  `json:"label"`
	BuyPrice      int64   `json:"buy_price"`
	SellPrice     int64   `json:"sell_price"`
	ProfitPerUnit float64 `json:"profit_per_unit"`
	Margin        float64 `json:"margin"`
	// Friction 是名义摩擦(几个比例相加),Breakeven 是真实要跨过的价差。
	// 后者总是更高,而且能分开名义都是 6.5% 的秒买挂卖(6.95%)和挂买秒卖(6.77%)
	Friction     float64 `json:"friction"`
	Breakeven    float64 `json:"breakeven"`
	HoursPerTurn float64 `json:"hours_per_turn"`
	TurnsPerDay  float64 `json:"turns_per_day"`
	DailyProfit  float64 `json:"daily_profit"`
	// Blocked 非空说明这个模式被深度闸门拦下(no_bid_side / thin_book):
	// 账照样算出来给人看,但不参与挑选
	Blocked string `json:"blocked,omitempty"`
	// Clamp 非空说明这个模式的挂单腿按 7 日成交均价封过:BuyPrice/SellPrice 和
	// 上面的账都是封过的,封之前的价在里面
	Clamp *econ.Clamp `json:"clamp,omitempty"`
}

// Notes 是警告加提示,报告里一起显示。
func (o Opportunity) Notes() []string { return append(append([]string{}, o.Warnings...), o.Hints...) }

// Rejected 是被过滤掉的候选。留着是为了能回答"为什么我没看到某物品"。
type Rejected struct {
	ItemID string `json:"item_id"`
	City   string `json:"city"`
	// Quality 以前没有:qualities 配成 [1,2,3] 时,同一物品同一城会有三条
	// 看起来一模一样的拒绝
	Quality int    `json:"quality"`
	Reason  string `json:"reason"`
	Detail  string `json:"detail"`
	// AskSource/BidSource 说明判它时两边各用了谁的价。抓包价被拒和 AODP 价被拒,
	// 处置不一样:前者多半该回游戏里再翻一眼
	AskSource string `json:"ask_source,omitempty"`
	BidSource string `json:"bid_source,omitempty"`
	// AskPrice/BidPrice 是判它时用的卖一、买一(融合之后的,和 AskSource/BidSource 对应),
	// 0 表示这一边没有价。以前明细里只有原因:只有卖单的高品质装备整批判成 one_sided,
	// 抓包抓到的卖价在机会页上一眼都看不到,只能去查价页一件件翻
	AskPrice int64 `json:"ask_price,omitempty"`
	BidPrice int64 `json:"bid_price,omitempty"`
}

// Evaluate 把一条 (物品, 城市) 的市场快照判成机会或拒绝原因。
// 两个返回值永远只有一个非 nil。等于两边都用 AODP 的 EvaluateSides。
func Evaluate(rec aodp.PriceRecord, stats *histagg.Stats, cfg conf.Config,
	names Namer, now time.Time) (*Opportunity, *Rejected) {
	return EvaluateSides(rec, Sides{}, stats, cfg, names, now)
}

// EvaluateSides 和 Evaluate 一样,只是多带了两边的来源和深度。
//
// 价格本身已经由融合层写进 rec(SellPriceMin/BuyPriceMax 及其时间戳),
// 这里的全部 troll 过滤照原样作用在融合后的价格上;sides 带来源、数据龄、
// 落选报价,以及抓包那一边的深度——深度闸门(LegGate)只看它。
// 零值 Sides 就是纯 AODP,深度闸门一律不触发。
func EvaluateSides(rec aodp.PriceRecord, sides Sides, stats *histagg.Stats, cfg conf.Config,
	names Namer, now time.Time) (*Opportunity, *Rejected) {

	sides = ResolveSides(rec, sides, now)

	reject := func(reason, detail string) (*Opportunity, *Rejected) {
		return nil, &Rejected{
			ItemID: rec.ItemID, City: rec.City, Quality: rec.Quality,
			Reason: reason, Detail: detail,
			AskSource: sides.Ask.Source, BidSource: sides.Bid.Source,
			AskPrice: max(rec.SellPriceMin, 0), BidPrice: max(rec.BuyPriceMax, 0),
		}
	}

	f := cfg.Filters
	var warnings, hints []string
	edge := false

	// ---- 第 1 层:双边存在 ------------------------------------------------
	// 价格 0 是 AODP 的"无数据"哨兵,不是"白送"。
	hasSell := rec.SellPriceMin > 0
	hasBuy := rec.BuyPriceMax > 0
	if !hasSell || !hasBuy {
		side := "两侧都无数据"
		switch {
		case hasSell:
			side = "只有卖单"
		case hasBuy:
			side = "只有买单"
		}
		return reject("one_sided", side)
	}

	// ---- 第 2 层:新鲜度 --------------------------------------------------
	sellAge, okSell := rec.SellPriceMinDate.AgeHours(now)
	buyAge, okBuy := rec.BuyPriceMaxDate.AgeHours(now)
	if !okSell || !okBuy {
		return reject("no_timestamp", "价格有值但时间戳缺失")
	}
	// 本机时钟慢、或上游时间戳超前,都会算出负的数据龄。不拦的话它不但
	// 通过下面这道新鲜度闸门,还会因为 maxAge <= HighConfidenceHours 直接
	// 拿到 high —— 越离谱越可信,恰好反了。
	if sellAge < 0 || buyAge < 0 {
		return reject("future_timestamp",
			fmt.Sprintf("数据龄为负(%.1fh),对一下本机和服务端时钟", math.Min(sellAge, buyAge)))
	}
	maxAge := math.Max(sellAge, buyAge)
	if maxAge > cfg.Freshness.MaxHours {
		return reject("stale", fmt.Sprintf("%.1fh > %gh", maxAge, cfg.Freshness.MaxHours))
	}

	// ---- 第 3 层:历史可用 ------------------------------------------------
	if stats == nil {
		return reject("no_history", "该城市没有成交历史")
	}
	historyAge, okHistory := stats.HistoryAgeDays(now)
	if !okHistory {
		return reject("stale_history", "无")
	}
	if historyAge > f.MaxHistoryGapDays {
		return reject("stale_history", fmt.Sprintf("最后成交距今 %.1fd", historyAge))
	}
	if stats.AvgPrice7d <= 0 {
		return reject("no_baseline", "7 日均价为 0,无法做偏离度判断")
	}
	if stats.DaysWithData7d < f.MinDaysWithData7d {
		return reject("thin_history", fmt.Sprintf("7 日内仅 %d 天有数据", stats.DaysWithData7d))
	}

	// ---- 第 4 层:偏离度 --------------------------------------------------
	span := f.DeviationMax - f.DeviationMin
	for _, side := range []struct {
		label string
		dev   float64
	}{
		{"卖价", float64(rec.SellPriceMin) / stats.AvgPrice7d},
		{"买价", float64(rec.BuyPriceMax) / stats.AvgPrice7d},
	} {
		if side.dev < f.DeviationMin || side.dev > f.DeviationMax {
			return reject("deviation", fmt.Sprintf("%s偏离 7 日均价 %.2fx", side.label, side.dev))
		}
		if side.dev-f.DeviationMin < span*edgeMargin || f.DeviationMax-side.dev < span*edgeMargin {
			edge = true
			warnings = append(warnings,
				fmt.Sprintf("%s偏离 %.2fx 接近阈值边缘", side.label, side.dev))
		}
	}

	// ---- 第 5 层:成交量 --------------------------------------------------
	if stats.DailyVolumeSilver < f.MinDailyVolumeSilver {
		return reject("low_volume", fmt.Sprintf("日流水 %s 银", thousandsF(stats.DailyVolumeSilver)))
	}
	if stats.DailyVolumeSilver < f.MinDailyVolumeSilver*volumeEdgeMultiplier {
		edge = true
		warnings = append(warnings, "日流水接近下限")
	}

	// ---- 价差上限 ---------------------------------------------------------
	// 不分数据来源:价差宽到这个程度,多半是有一边没人在真做。默认关,
	// 纯价格过滤的 max_margin 已经兜住了最离谱的那段
	if f.MaxSpreadPct > 0 {
		if spread := float64(rec.SellPriceMin)/float64(rec.BuyPriceMax) - 1; spread > f.MaxSpreadPct {
			return reject("wide_spread", fmt.Sprintf("卖一 %s / 买一 %s,价差 %.1f%% 超过上限 %.1f%%",
				Thousands(rec.SellPriceMin), Thousands(rec.BuyPriceMax), spread*100, f.MaxSpreadPct*100))
		}
	}

	// ---- 交叉盘:买价 >= 卖价 ---------------------------------------------
	// 真实市场不会持续存在这种状态(会立刻自己成交),出现说明两侧快照
	// 来自不同时间点,是陈旧数据的强信号。
	if rec.BuyPriceMax >= rec.SellPriceMin {
		detail := fmt.Sprintf("买 %s >= 卖 %s", Thousands(rec.BuyPriceMax), Thousands(rec.SellPriceMin))
		if f.RejectCrossedBook {
			return reject("crossed_book", detail)
		}
		edge = true
		warnings = append(warnings, fmt.Sprintf("交叉盘(%s),两侧快照时间不一致", detail))
	}

	// ---- 利润与周转 -------------------------------------------------------
	// 四种执行方式各算一遍完整的账,按**日收益**挑,不是按单件利润。
	// 同城没有路程,但挂单腿一样要等成交——用 econ 里那套和跨城
	// 共用的周转模型,否则组合页把同城跨城混排时口径不一致
	book := econ.Book{SellMin: rec.SellPriceMin, BuyMax: rec.BuyPriceMax}
	absorbable := stats.DailyVolumeQty * cfg.Sizing.AbsorbRatio
	// 挂单腿按本城 7 日成交均价做带宽钳制。偏离度(上面第 4 层)只拦离谱到 troll 的价,
	// 0.4~2.5 倍整段放行;挂单价在这一段里照盘口压一档,就是假设有人按均价两倍来买
	band := MakerBand(f.MakerPriceBand, stats, stats)

	var modes []ModeQuote
	type sized struct {
		q    ModeQuote
		unit econ.Unit
		// raw/rawQ 是不钳的账和按它算的日收益、单件利润:第一步拿它们复现 band = 0 时的判定
		raw  econ.Unit
		rawQ ModeQuote
		qty  int64
		mode econ.Mode
		// 深度闸门对这个模式的判定
		edge   bool
		warns  []string
		detail string
	}
	// rawOK 是不钳也赚钱、没被拦的;rawBlocked 是不钳赚钱、但被深度闸门拦下的。
	// 不钳都不赚的模式,钳了只会更不赚,两步里都用不上
	var rawOK, rawBlocked []sized
	for _, m := range econ.Modes {
		bq := econ.QuoteBanded(book, book, m, cfg.Economics, band)
		u := bq.Unit
		hours := m.HoursPerRound(0, cfg.Sizing.FillHours) // 同城 travel = 0
		qty, turns, daily := econ.Turnover(u, absorbable, cfg.Capital, hours)
		// 同城两条腿在同一个城:挂买看这里的 bid,挂卖看这里的 ask
		reason, detail, gEdge, gWarns := LegGate(m, sides.Bid, sides.Ask, f)
		q := ModeQuote{
			Mode: m.Key(), Label: m.Label(),
			BuyPrice: u.MyBid, SellPrice: u.MyAsk,
			ProfitPerUnit: u.ProfitPerUnit, Margin: u.Margin,
			Friction:     m.Friction(cfg.Economics),
			Breakeven:    m.Breakeven(cfg.Economics),
			HoursPerTurn: hours, TurnsPerDay: turns, DailyProfit: daily,
			Blocked: reason,
			Clamp:   bq.Clamp,
		}
		modes = append(modes, q)
		if bq.Raw.ProfitPerUnit <= 0 {
			continue
		}
		rawQ := q
		if bq.Clamp != nil {
			_, _, rawDaily := econ.Turnover(bq.Raw, absorbable, cfg.Capital, hours)
			rawQ.ProfitPerUnit, rawQ.DailyProfit = bq.Raw.ProfitPerUnit, rawDaily
		}
		s := sized{q: q, unit: u, raw: bq.Raw, rawQ: rawQ, qty: qty, mode: m, edge: gEdge, warns: gWarns, detail: detail}
		if reason != "" {
			rawBlocked = append(rawBlocked, s)
		} else {
			rawOK = append(rawOK, s)
		}
	}
	sort.SliceStable(modes, func(i, j int) bool { return modes[i].DailyProfit > modes[j].DailyProfit })
	// 按日收益挑。量不足时日收益全是 0,退回按单件利润挑——
	// 这时候要报的是"吃不下"而不是"不赚钱",两者的处置完全不同。
	// key 选按哪份账排:第一步按不钳的(rawQ),第二步按钳过的(q)
	byDaily := func(s []sized, key func(sized) ModeQuote) {
		sort.SliceStable(s, func(i, j int) bool {
			a, b := key(s[i]), key(s[j])
			if a.DailyProfit != b.DailyProfit {
				return a.DailyProfit > b.DailyProfit
			}
			return a.ProfitPerUnit > b.ProfitPerUnit
		})
	}
	rawKey := func(s sized) ModeQuote { return s.rawQ }
	bandKey := func(s sized) ModeQuote { return s.q }

	// ---- 第一步:按不钳的账复现 band = 0 时的判定 ------------------------
	// 深度闸门、不赚钱、毛利率上限这三个拒绝原因判的都是盘口本身,和带宽无关:同一本盘口,
	// 带宽开着关着、开多宽,都得是同一个结论。以前先按钳过的账判"赚不赚钱",钳完不赚钱的
	// troll 盘(买 1,100 / 卖 2,450 / 均价 1,000,盘口毛利 103%)被改报成 unprofitable,
	// 深度闸门拦下的也一样,拒绝明细里看不出它是假单、还是没人在收。
	// band = 0 时这一步就是以前的整个判定,逐位不变
	if len(rawOK) == 0 && len(rawBlocked) > 0 {
		// 纸面上赚钱,只是盘口撑不住。报被拦的原因而不是"不赚钱":
		// 前者是没人在收、或卖一是张孤单,后者是价差本身不够,处置完全不同
		byDaily(rawBlocked, rawKey)
		return reject(rawBlocked[0].q.Blocked, rawBlocked[0].detail)
	}
	spread := float64(rec.SellPriceMin)/float64(rec.BuyPriceMax) - 1
	// 亏损写成正数:以前是"税后亏 -9.5",负号和"亏"叠成双重否定,读着像赚了
	lossText := func(u econ.Unit) string {
		if u.ProfitPerUnit < 0 {
			return fmt.Sprintf("税后每件亏 %s 银", thousands1(-u.ProfitPerUnit))
		}
		return "税后每件刚好打平"
	}
	if len(rawOK) == 0 {
		// 四种方式没一种赚钱(不钳都不赚)。报最保守那种的亏损,让人看得懂为什么被拒。
		//
		// 门槛报**真实盈亏平衡**而不是名义摩擦:名义是几个比例直接相加,
		// 而买侧的费乘在 my_bid 上、卖侧的费乘在 my_ask 上,基数不同。
		// 挂买挂卖名义 9.0%、真实要 9.63% —— 中间那 0.63 个百分点的价差
		// 会被"摩擦 9%"这句话说成够用,实际每件都在亏。
		mm := econ.Mode{Buy: econ.Maker, Sell: econ.Maker}
		return reject("unprofitable", fmt.Sprintf(
			"%s:买卖价差 %.2f%%,%s 要 %.2f%% 才打平",
			lossText(econ.Quote(book, book, mm, cfg.Economics)), spread*100, mm.Label(), mm.Breakeven(cfg.Economics)*100))
	}
	byDaily(rawOK, rawKey)
	// 毛利率上限是 troll 判据,判的是盘口本身离不离谱,所以按**不钳**的价、判**不钳时会挑中**
	// 的那个模式(见 econ.Banded.Raw)。按钳过的判,挂买挂卖的毛利被带宽封在 ~37% 以内,
	// 这一层对挂单模式就永远不响了
	if top := rawOK[0]; top.raw.Margin > f.MaxMargin {
		detail := fmt.Sprintf("毛利率 %.0f%% 高得不真实", top.raw.Margin*100)
		switch {
		case top.q.Clamp != nil && top.unit.ProfitPerUnit <= 0:
			// 封过之后已经不赚钱。原因照样报 troll:它是盘口本身的毛病,带宽只是顺带把账算低了
			detail = fmt.Sprintf("按盘口价算毛利率 %.0f%% 高得不真实(挂单价封回参考价附近之后已经不赚钱,两边盘口多半有 troll 单)",
				top.raw.Margin*100)
		case top.q.Clamp != nil:
			detail = fmt.Sprintf("按盘口价算毛利率 %.0f%% 高得不真实(挂单价封回参考价附近之后是 %.0f%%,但两边盘口多半有 troll 单)",
				top.raw.Margin*100, top.unit.Margin*100)
		}
		return reject("implausible_margin", detail)
	}

	// ---- 第二步:按钳过的账挑 --------------------------------------------
	// 钳前毛利超上限的模式也不挑:盘口作为整体放行了(不钳时挑中的那个没超),但这个模式
	// 自己依托的那两个价离谱。band = 0 时它本来就挑不上(挑中的是上面那个),逐位不变
	var profitable []sized
	for _, s := range rawOK {
		if s.unit.ProfitPerUnit > 0 && s.raw.Margin <= f.MaxMargin {
			profitable = append(profitable, s)
		}
	}
	if len(profitable) == 0 {
		// 盘口价差够,是挂单价封回参考价附近之后不够了。报不钳时会挑中的那个模式封过的账,
		// 并把盘口价差一起写上:只报盘口价差的话,"价差 80% 还说不赚钱"读不懂。
		// 走到这里说明那个模式一定被封过:没封的话钳过的账和不钳的一样,是赚的
		top := rawOK[0]
		u := top.unit
		return reject("unprofitable", fmt.Sprintf(
			"%s:挂单价按参考价 ±%s 封过,%s → %s 价差 %.2f%%,%s 要 %.2f%% 才打平(盘口价差 %.2f%%)",
			lossText(u), bandPct(f.MakerPriceBand), Thousands(u.MyBid), Thousands(u.MyAsk),
			(float64(u.MyAsk)/float64(u.MyBid)-1)*100, top.mode.Label(), top.mode.Breakeven(cfg.Economics)*100, spread*100))
	}
	byDaily(profitable, bandKey)
	best, unit, bestQty, bestMode := profitable[0].q, profitable[0].unit, profitable[0].qty, profitable[0].mode
	// 选中模式贴着深度阈值:并进 edge 和警告,置信度规则本身不变
	if profitable[0].edge {
		edge = true
	}
	warnings = append(warnings, profitable[0].warns...)

	// 选中模式的挂单腿被封过:说清楚封在哪、为什么。只封一边不降置信——封过的价本来就更保守;
	// 两边都封时两个挂价都是按带宽推出来的,毛利跟着带宽走,压到 low
	warnings = append(warnings, ClampWarnings(best.Clamp, unit.MyBid, unit.MyAsk,
		rec.BuyPriceMax, rec.SellPriceMin, "", "")...)
	if best.Clamp.Both() {
		edge = true
	}

	// ---- 吃单量 -----------------------------------------------------------
	pos := econ.SizePosition(unit, stats.DailyVolumeQty, cfg.Capital, cfg.Sizing)
	if bestQty < 1 {
		return reject("too_thin", fmt.Sprintf("可吃量不足 1 件(%.2f)", pos.AbsorbableQty))
	}
	if pos.CapitalBound {
		hints = append(hints, "本金是瓶颈,市场还能吃更多")
	}

	// ---- 置信度 -----------------------------------------------------------
	confidence := Medium
	switch {
	case edge:
		confidence = Low
	case maxAge <= cfg.Freshness.HighConfidenceHours:
		confidence = High
	}

	name := rec.ItemID
	if names != nil {
		name = names.NameOf(rec.ItemID)
	}

	zscore, hasZ := stats.ZScore(float64(rec.SellPriceMin+rec.BuyPriceMax) / 2)
	if hasZ && zscore > 1.5 {
		warnings = append(warnings,
			fmt.Sprintf("现价高于 30 日常态 %.1fσ,这个价差可能是一时的", zscore))
	}
	if stats.CV > 0.25 {
		warnings = append(warnings, fmt.Sprintf("价格波动大(变异系数 %.2f)", stats.CV))
	}

	for _, s := range []Side{sides.Ask, sides.Bid} {
		if s.Note != "" {
			hints = append(hints, s.Note)
		}
	}
	if sides.Ask.Source == SourceCapture && sides.Bid.Source != SourceCapture {
		// 最常见的缺口:市场列表页只发卖单请求。买单簿要点进物品详情页才抓得到,
		// 挂买腿有没有人砸货这件事现在完全不知道
		hints = append(hints, "买方深度未知:列表页只抓卖单,点进物品详情页才抓得到买单")
	}
	buyLeg, sellLeg := legSides(bestMode, sides)
	// 两条腿依托的那一边都有可信深度才算核过。挂单腿就是闸门看的那一边;
	// 同城只有秒买秒卖没有挂单腿,这时按它吃的两边算,免得"没有挂单腿"被说成核过
	depthChecked := buyLeg.Captured() && sellLeg.Captured()
	fillPos, hasFillPos := depth.FillPosition(rec.BuyPriceMax, rec.SellPriceMin, stats.AvgPrice7d)

	return &Opportunity{
		Ask:               sides.Ask,
		Bid:               sides.Bid,
		BuyLegSource:      buyLeg.Source,
		SellLegSource:     sellLeg.Source,
		BuyLegAgeHours:    buyLeg.AgeHours,
		SellLegAgeHours:   sellLeg.AgeHours,
		DepthChecked:      depthChecked,
		FillPosition:      fillPos,
		HasFillPosition:   hasFillPos,
		ItemID:            rec.ItemID,
		ItemName:          name,
		City:              rec.City,
		Quality:           rec.Quality,
		BuyPrice:          rec.BuyPriceMax,
		SellPrice:         rec.SellPriceMin,
		MyBid:             unit.MyBid,
		MyAsk:             unit.MyAsk,
		Clamp:             best.Clamp,
		CostPerUnit:       unit.CostPerUnit,
		RevenuePerUnit:    unit.RevenuePerUnit,
		ProfitPerUnit:     unit.ProfitPerUnit,
		Margin:            unit.Margin,
		DailyVolumeQty:    stats.DailyVolumeQty,
		DailyVolumeSilver: stats.DailyVolumeSilver,
		AvgPrice7d:        stats.AvgPrice7d,
		AvgPrice30d:       stats.AvgPrice30d,
		AbsorbableQty:     pos.AbsorbableQty,
		Qty:               bestQty,
		CapitalUsed:       float64(bestQty) * unit.CostPerUnit,
		DailyProfit:       best.DailyProfit,
		DailyROI:          unit.Margin,
		CapitalROI:        best.DailyProfit / float64(cfg.Capital),
		Mode:              best.Mode,
		ModeLabel:         best.Label,
		TurnsPerDay:       best.TurnsPerDay,
		HoursPerTurn:      best.HoursPerTurn,
		BuyAgeHours:       buyAge,
		SellAgeHours:      sellAge,
		DataAgeHours:      maxAge,
		Volatility:        stats.CV,
		ZScore:            zscore,
		HasZ:              hasZ,
		Modes:             modes,
		Confidence:        confidence,
		Warnings:          warnings,
		Hints:             hints,
	}, nil
}
