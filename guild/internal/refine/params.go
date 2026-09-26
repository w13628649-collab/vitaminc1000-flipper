package refine

import (
	"fmt"
	"math"
	"time"

	"albion-guild/internal/conf"
)

// Params 是一次计算的参数。字段和 POST /api/refine/plan 的请求体一一对应(设计 §4.4),
// 个人参数(FCE、今日专注、看到的站费、k 的实测值)由客户端每次带上,服务端不存。
//
// 引擎只认填好的参数:API 层先拿 DefaultParams 打底、再把请求体解码进去。
// 零值有意义的字段(站费 0、心不返还)不会被悄悄换成默认值。
type Params struct {
	// Target 是要做的半成品,市场 id,例如 T8_METALBAR_LEVEL4@4
	Target string `json:"target"`
	// City 是生产城市;回收率的专精加成、站费、默认的买料和出售城市都按它
	City string `json:"city"`
	// Qty 是这一批做几件
	Qty int `json:"qty"`

	Sell    SellParams   `json:"sell"`
	Sources SourceParams `json:"sources"`
	Focus   FocusParams  `json:"focus"`
	RRR     RRRParams    `json:"rrr"`
	Fees    FeeParams    `json:"fees"`
	Daily   DailyParams  `json:"daily"`

	// HeartReturns:精炼用的心是否也按回收率返还。A7 ?,默认 true;对 .4 链没有影响
	HeartReturns bool `json:"heart_returns"`
	// Leftover 是批末余料的估值方式(设计 §2.4):carry 结转到下一批、按取得成本计;
	// bid 马上砸买单(×0.96);zero 按 0 算
	Leftover string `json:"leftover"`
	// IncludeTail:把批末余料算进开工本金,bid / zero 时把余料的损失摊进成本
	IncludeTail bool `json:"include_tail"`

	// Top 是候选路线最多列几条(不含两条参照行)
	Top int `json:"top"`
	// Sort 是排序键:daily 日化 | unit 单根利润 | per_focus 每点专注 | pct 利润率
	Sort string `json:"sort"`
	// PureStarts:「从某个起点纯转化」这类候选取前几个起点
	PureStarts int `json:"pure_starts"`

	// Now 是算价格年龄用的「现在」。引擎不看钟,由调用方给
	Now time.Time `json:"-"`
}

// SellParams 是出售方式。
type SellParams struct {
	// Modes 是要算的出售方式:post 挂卖、take 吃买单。第一个是排序和日化用的主方式
	Modes []string `json:"modes"`
	// City 是出售城市,默认等于生产城市
	City string `json:"city"`
	// CliffPct 是吃买单时认「断崖」的落差:某一档比买一低这么多以上就停。
	// 这是工具的判据,不是游戏规则。默认 0.3:实测高 tier 半成品的买单,断崖之前的几档
	// 离买一不到一成,断崖那一档离买一九成以上,0.3 两边都留足了余量
	CliffPct float64 `json:"cliff_pct"`
}

// SourceParams 是买料的来源。
type SourceParams struct {
	// Cities 是可以买料的城市,第一个应当是生产城市。别的城市的料按 TransportPct 加运费
	Cities       []string `json:"cities"`
	TransportPct float64  `json:"transport_pct"`
	// BidOrders:把「挂买单收料」列进候选。默认只当参照行,不进推荐(用户问题 7)
	BidOrders bool `json:"bid_orders"`
	// BidMaxQty 是挂买单收料最多收多少件,0 = 不限。收不收得到都不知道
	BidMaxQty float64 `json:"bid_max_qty"`
	// MaxAgeHours:比这更旧的盘口当作缺价。默认 72(refining.window_hours)
	MaxAgeHours float64 `json:"max_age_hours"`
}

// FocusParams 是专注。
type FocusParams struct {
	// Mode:auto 枚举几种开法并给出预算内最优;off / top / all / custom 只算那一种
	Mode        string `json:"mode"`
	CustomTiers []int  `json:"custom_tiers"`
	// Budget 是今日可用专注(premium 每天恢复 10,000,上限 30,000,D4)
	Budget float64 `json:"budget"`
	// FCE 是这条线各 tier 的专注消耗效率,tier → 点数(D3)。没写的 tier 按 0
	FCE map[int]float64 `json:"fce"`
	// UnlockedTier:命运之轮里精炼解锁到 T 几(A10)。更高的 tier 只能买、不能自炼
	UnlockedTier int `json:"unlocked_tier"`
}

// RRRParams 是回收率的附加项。
type RRRParams struct {
	// DailyBonus 是今天落在这条线上的每日生产加成(C6),0 / 10 / 20
	DailyBonus float64 `json:"daily_bonus"`
	// Island:在个人岛上精炼(C7),City 是岛绑定的城市
	Island bool `json:"island"`
}

// FeeParams 是各项费用。
type FeeParams struct {
	// StationPer100 是精炼站使用费,银 / 100 营养(E5 ?,默认 500)
	StationPer100 float64 `json:"station_per100"`
	// TransmuteFactor 是转化实付系数 k(B5 ?,默认 1.154)
	TransmuteFactor float64 `json:"transmute_factor"`
	// TransmuteStationPer100 是转化的主人费率(B6 ◐,默认 500)
	TransmuteStationPer100 float64 `json:"transmute_station_per100"`
	// 市场税费:和 conf.Economics 同一份口径,服务端从配置填
	Tax           float64 `json:"tax"`
	Setup         float64 `json:"setup"`
	BuyOrderSetup bool    `json:"buy_order_setup"`
	Outbid        int64   `json:"outbid"`
	Undercut      int64   `json:"undercut"`
}

// economics 转成 econ 认的配置,税费只走 econ 那一份逻辑(F1–F3)。
func (f FeeParams) economics() conf.Economics {
	return conf.Economics{
		MarketTax: f.Tax, SetupFee: f.Setup, BuyOrderSetupFee: f.BuyOrderSetup,
		OutbidSilver: f.Outbid, UndercutSilver: f.Undercut,
	}
}

// DailyParams 是日化的几个约束(设计 §2.5)。
type DailyParams struct {
	// VolumeShare 是能吃到日成交量的几成,默认 0.3(用户问题 6,没核实)
	VolumeShare float64 `json:"volume_share"`
	// Capital 是本金,0 = 不按本金封顶
	Capital float64 `json:"capital"`
	// TurnoverDays 是一轮(买料、精炼、卖掉)要几天,本金约束用
	TurnoverDays float64 `json:"turnover_days"`
}

// 出售方式、余料估值、排序键、专注模式的取值。
const (
	SellPost = "post"
	SellTake = "take"

	LeftoverCarry = "carry"
	LeftoverBid   = "bid"
	LeftoverZero  = "zero"

	SortDaily    = "daily"
	SortUnit     = "unit"
	SortPerFocus = "per_focus"
	SortPct      = "pct"

	FocusAuto   = "auto"
	FocusOff    = "off"
	FocusTop    = "top"
	FocusAll    = "all"
	FocusCustom = "custom"
)

const (
	// DefaultMaxAgeHours:精炼页读抓包的窗口(设计 §4.3,用户问题 9)。用户是抓一次包、
	// 之后再算,查价页那 6 小时一过就读不到了
	DefaultMaxAgeHours = 72.0
	// AgeYellowHours / AgeRedHours:价格年龄超过这些就标黄 / 标红(设计 §2.5)
	AgeYellowHours = 6.0
	AgeRedHours    = 24.0
	// DefaultFocusBudget:premium 每天恢复 10,000 专注(D4 ✔ characters.xml 568–572)
	DefaultFocusBudget = 10000.0
	// FocusCap:专注上限 30,000,满了就不再恢复(D4)
	FocusCap = 30000.0
	// DefaultVolumeShare:能吃到日成交量的几成(用户问题 6,没核实)
	DefaultVolumeShare = 0.3
	// DefaultCliffPct 见 SellParams.CliffPct
	DefaultCliffPct = 0.3
	// DefaultTop:候选表默认列 12 条(设计 §2.3 ⑤)
	DefaultTop = 12
	// DefaultPureStarts:纯转化候选取前 3 个起点
	DefaultPureStarts = 3
	// MaxQty 挡一下离谱的件数
	MaxQty = 10000
	// MaxAgeHoursLimit:数据最大年龄的上限,和 conf 里 refining.window_hours 的上限(30 天)一致。
	// 服务端还会再按自己的读簿窗口卡一次
	MaxAgeHoursLimit = 24 * 30.0
	// MaxTurnoverDays:一轮(买料、精炼、卖掉)最多按一年算,挡录错
	MaxTurnoverDays = 365.0
	// MaxPriceStep:挂单压价 / 加价(fees.outbid / undercut)的上限,银币。这是防录错的范围,
	// 不是游戏规则:负数会把挂卖价抬到卖一之上、虚高挂卖利润,极大的数会让价格溢出
	MaxPriceStep = 1_000_000
)

// DefaultParams 是做 target 的默认参数:生产和出售都在这条线的专精城,
// 待核对的参数取 rules.go 里的默认值,税费取 conf 的默认值。
func DefaultParams(target string) Params {
	city := ""
	if family, _, _, ok := ParseItemID(target); ok {
		if l, ok := LineOf(family); ok {
			city = l.City
		}
	}
	econ := conf.Default().Economics
	return Params{
		Target: target, City: city, Qty: 10,
		Sell:    SellParams{Modes: []string{SellPost, SellTake}, City: city, CliffPct: DefaultCliffPct},
		Sources: SourceParams{Cities: []string{city}, MaxAgeHours: DefaultMaxAgeHours},
		Focus: FocusParams{Mode: FocusAuto, Budget: DefaultFocusBudget, FCE: map[int]float64{},
			UnlockedTier: DefaultUnlockedTier},
		RRR: RRRParams{DailyBonus: DefaultDailyBonus},
		Fees: FeeParams{
			StationPer100: DefaultStationFeePer100, TransmuteFactor: DefaultTransmuteFactor,
			TransmuteStationPer100: DefaultTransmuteStationFeePer100,
			Tax:                    econ.MarketTax, Setup: econ.SetupFee, BuyOrderSetup: econ.BuyOrderSetupFee,
			Outbid: econ.OutbidSilver, Undercut: econ.UndercutSilver,
		},
		Daily:        DailyParams{VolumeShare: DefaultVolumeShare, TurnoverDays: 1},
		HeartReturns: DefaultHeartReturns,
		Leftover:     LeftoverCarry,
		IncludeTail:  true,
		Top:          DefaultTop,
		Sort:         SortDaily,
		PureStarts:   DefaultPureStarts,
	}
}

// normalized 补齐几个空字符串 / 空列表:它们的零值没有别的意义。数值字段不动。
func (p Params) normalized() Params {
	if p.Sell.City == "" {
		p.Sell.City = p.City
	}
	if len(p.Sell.Modes) == 0 {
		p.Sell.Modes = []string{SellPost, SellTake}
	}
	if len(p.Sources.Cities) == 0 {
		p.Sources.Cities = []string{p.City}
	}
	if p.Focus.Mode == "" {
		p.Focus.Mode = FocusAuto
	}
	if p.Leftover == "" {
		p.Leftover = LeftoverCarry
	}
	if p.Sort == "" {
		p.Sort = SortDaily
	}
	if p.Top <= 0 {
		p.Top = DefaultTop
	}
	if p.PureStarts < 0 {
		p.PureStarts = 0
	}
	if p.Focus.FCE == nil {
		p.Focus.FCE = map[int]float64{}
	}
	if p.Focus.CustomTiers == nil {
		p.Focus.CustomTiers = []int{} // JSON 里给 [] 不给 null
	}
	return p
}

// finite:不是 NaN、不是 ±Inf。查询参数的 ParseFloat 认 "Inf" / "NaN",
// 光比大小挡不住 +Inf(+Inf >= 0 为真),之后缓存键的 json.Marshal 会失败成 500。
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// validate 检查参数。错误是给用户看的:说清楚哪一项、合法范围是什么。
func (p Params) validate() (Line, int, int, error) {
	family, tier, enchant, ok := ParseItemID(p.Target)
	line, isLine := LineOf(family)
	// 附魔只有 .0~.4(MaxEnchant),而且 id 要是规范写法:T8_METALBAR_LEVEL0@0 这种
	// 游戏里没有的写法、T8_METALBAR_LEVEL5@5 这种不存在的附魔都挡掉(审查 finding 7),
	// 不能让它们读一遍库、打一次 AODP,最后回一个误导人的「没解锁」
	if !ok || !isLine || family != line.Refined || tier < MinEnchantTier || tier > MaxTier ||
		enchant > MaxEnchant || ItemID(family, tier, enchant) != p.Target {
		return Line{}, 0, 0, fmt.Errorf("%q 不是本期能算的半成品:只做矿/皮/纤维/木四条线 T4~T8、附魔 .0~.4 的精炼产物", p.Target)
	}
	if p.City == "" {
		return line, 0, 0, fmt.Errorf("没给生产城市")
	}
	if p.Qty < 1 || p.Qty > MaxQty {
		return line, 0, 0, fmt.Errorf("件数要在 [1, %d] 内,得到 %d", MaxQty, p.Qty)
	}
	if p.Now.IsZero() {
		return line, 0, 0, fmt.Errorf("没给 Now:引擎不看钟,价格年龄要按调用方给的时刻算")
	}
	for _, m := range p.Sell.Modes {
		if m != SellPost && m != SellTake {
			return line, 0, 0, fmt.Errorf("出售方式 %q 不认识,只有 post / take", m)
		}
	}
	if !(p.Sell.CliffPct > 0 && p.Sell.CliffPct < 1) {
		return line, 0, 0, fmt.Errorf("断崖落差要在 (0, 1) 内,得到 %g", p.Sell.CliffPct)
	}
	if !(p.Sources.TransportPct >= 0 && p.Sources.TransportPct < 10) {
		return line, 0, 0, fmt.Errorf("运费比例要在 [0, 10) 内,得到 %g", p.Sources.TransportPct)
	}
	if !(p.Sources.BidMaxQty >= 0) || !finite(p.Sources.BidMaxQty) {
		return line, 0, 0, fmt.Errorf("挂买单上限要是非负的有限数(0 = 不限),得到 %g", p.Sources.BidMaxQty)
	}
	if !(p.Sources.MaxAgeHours > 0 && p.Sources.MaxAgeHours <= MaxAgeHoursLimit) {
		return line, 0, 0, fmt.Errorf("数据最大年龄要在 (0, %g] 小时内,得到 %g", MaxAgeHoursLimit, p.Sources.MaxAgeHours)
	}
	switch p.Focus.Mode {
	case FocusAuto, FocusOff, FocusTop, FocusAll:
	case FocusCustom:
		for _, t := range p.Focus.CustomTiers {
			if t < MinEnchantTier || t > tier {
				return line, 0, 0, fmt.Errorf("自定义专注的 T%d 不在这条链上(T%d~T%d)", t, MinEnchantTier, tier)
			}
		}
	default:
		return line, 0, 0, fmt.Errorf("专注模式 %q 不认识", p.Focus.Mode)
	}
	if !(p.Focus.Budget >= 0) || math.IsInf(p.Focus.Budget, 0) {
		return line, 0, 0, fmt.Errorf("今日专注要是非负的有限数,得到 %g", p.Focus.Budget)
	}
	for t, fce := range p.Focus.FCE {
		// 键是 tier:只有 T4~T8 有精炼专精;{"99":1000} 这种以前被悄悄忽略
		if t < MinEnchantTier || t > MaxTier {
			return line, 0, 0, fmt.Errorf("专精 FCE 的键是 tier,要在 %d~%d 内,得到 %d", MinEnchantTier, MaxTier, t)
		}
		if err := CheckFCE(fce); err != nil {
			return line, 0, 0, fmt.Errorf("T%d: %w", t, err)
		}
	}
	if p.Focus.UnlockedTier < MinTier || p.Focus.UnlockedTier > MaxTier {
		return line, 0, 0, fmt.Errorf("已解锁 tier 要在 [%d, %d] 内,得到 %d", MinTier, MaxTier, p.Focus.UnlockedTier)
	}
	if !(p.RRR.DailyBonus >= 0 && p.RRR.DailyBonus <= 100) {
		return line, 0, 0, fmt.Errorf("每日生产加成要在 [0, 100] 内,得到 %g", p.RRR.DailyBonus)
	}
	if err := CheckStationFee(p.Fees.StationPer100); err != nil {
		return line, 0, 0, err
	}
	if err := CheckStationFee(p.Fees.TransmuteStationPer100); err != nil {
		return line, 0, 0, fmt.Errorf("转化主人费率: %w", err)
	}
	if err := CheckTransmuteFactor(p.Fees.TransmuteFactor); err != nil {
		return line, 0, 0, err
	}
	if !(p.Fees.Tax >= 0 && p.Fees.Setup >= 0 && p.Fees.Tax+p.Fees.Setup < 1) {
		return line, 0, 0, fmt.Errorf("税 %g、创建费 %g 不合理", p.Fees.Tax, p.Fees.Setup)
	}
	if p.Fees.Outbid < 0 || p.Fees.Outbid > MaxPriceStep || p.Fees.Undercut < 0 || p.Fees.Undercut > MaxPriceStep {
		return line, 0, 0, fmt.Errorf("挂单加价 outbid %d、压价 undercut %d 要在 [0, %d] 银内",
			p.Fees.Outbid, p.Fees.Undercut, MaxPriceStep)
	}
	switch p.Leftover {
	case LeftoverCarry, LeftoverBid, LeftoverZero:
	default:
		return line, 0, 0, fmt.Errorf("余料估值 %q 不认识,只有 carry / bid / zero", p.Leftover)
	}
	switch p.Sort {
	case SortDaily, SortUnit, SortPerFocus, SortPct:
	default:
		return line, 0, 0, fmt.Errorf("排序键 %q 不认识", p.Sort)
	}
	if !(p.Daily.VolumeShare > 0 && p.Daily.VolumeShare <= 1) {
		return line, 0, 0, fmt.Errorf("成交份额要在 (0, 1] 内,得到 %g", p.Daily.VolumeShare)
	}
	if !(p.Daily.Capital >= 0) || !finite(p.Daily.Capital) {
		return line, 0, 0, fmt.Errorf("本金要是非负的有限数(0 = 不按本金封顶),得到 %g", p.Daily.Capital)
	}
	if !(p.Daily.TurnoverDays > 0 && p.Daily.TurnoverDays <= MaxTurnoverDays) {
		return line, 0, 0, fmt.Errorf("周转天数要在 (0, %g] 内,得到 %g", MaxTurnoverDays, p.Daily.TurnoverDays)
	}
	return line, tier, enchant, nil
}

// UnverifiedUsed 是结果里 params_used.unverified 的一项:没核实的参数、这次用的值、
// 是不是默认值。界面据此给数字打「未核实」徽章。
type UnverifiedUsed struct {
	Unverified
	Value     any  `json:"value"`
	IsDefault bool `json:"is_default"`
}

// unverifiedUsed 按 UnverifiedParams 的顺序列出这次用的值。
func (p Params) unverifiedUsed(chainTiers []int) []UnverifiedUsed {
	fce := map[int]float64{}
	fceDefault := true
	for _, t := range chainTiers {
		fce[t] = p.Focus.FCE[t]
		if fce[t] != DefaultFCE {
			fceDefault = false
		}
	}
	value := map[string]struct {
		v   any
		def bool
	}{
		"fees.transmute_factor":         {p.Fees.TransmuteFactor, p.Fees.TransmuteFactor == DefaultTransmuteFactor},
		"focus.fce":                     {fce, fceDefault},
		"focus.unlocked_tier":           {p.Focus.UnlockedTier, p.Focus.UnlockedTier == DefaultUnlockedTier},
		"fees.station_per100":           {p.Fees.StationPer100, p.Fees.StationPer100 == DefaultStationFeePer100},
		"fees.transmute_station_per100": {p.Fees.TransmuteStationPer100, p.Fees.TransmuteStationPer100 == DefaultTransmuteStationFeePer100},
		"rrr.daily_bonus":               {p.RRR.DailyBonus, p.RRR.DailyBonus == DefaultDailyBonus},
		"heart_returns":                 {p.HeartReturns, p.HeartReturns == DefaultHeartReturns},
	}
	out := make([]UnverifiedUsed, 0, len(UnverifiedParams))
	for _, u := range UnverifiedParams {
		v := value[u.Key]
		out = append(out, UnverifiedUsed{Unverified: u, Value: v.v, IsDefault: v.def})
	}
	return out
}

// PremiumSilverPerFocus 是「为专注养一个 premium 小号」的门槛:每点专注值多少银。
//
// D6 ◐:premium 30 天 3750 金(gamedata 1588),每天恢复 10,000 专注(D4),
// 所以每点专注的成本 = 3750 × 金价 ÷ (30 × 10,000)。金价 13,554(AODP gold,2026-09-26)
// 时约 169 银/点。每点专注赚不到这个数,就不值得为了专注开小号。
func PremiumSilverPerFocus(goldPrice float64) float64 {
	const (
		premiumGold30d = 3750.0
		days           = 30.0
	)
	return premiumGold30d * goldPrice / (days * DefaultFocusBudget)
}
