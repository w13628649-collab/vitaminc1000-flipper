package econ

import "albion-guild/internal/conf"

// Leg 是一条腿的执行方式。摩擦成本完全由它决定,差别很大:
//
//	秒买  —— 直接吃掉别人的卖单。**买方不付任何费用**,但要付卖一的价
//	挂买  —— 挂买单等人卖给你。2.5% 创建费(不退),但能拿到买一附近的价
//	秒卖  —— 直接卖给别人的买单。付 4% 市场税,拿到的是买一的价
//	挂卖  —— 挂卖单等人来买。2.5% 创建费 + 成交时 4% 市场税,能卖到卖一附近
//
// Demo 只建模了「挂买 + 挂卖」这一种,固定 9% 摩擦。实际上
// **秒买 + 秒卖只有 4%**——代价是买贵卖便宜。哪种划算取决于盘口宽度,
// 不该由代码替用户决定。
type Leg int8

const (
	Taker Leg = iota // 吃单(市价)
	Maker            // 挂单(限价)
)

func (l Leg) String() string {
	if l == Taker {
		return "taker"
	}
	return "maker"
}

// Mode 是一次往返的两条腿。
type Mode struct {
	Buy  Leg
	Sell Leg
}

var Modes = []Mode{
	{Taker, Taker}, // 秒买秒卖:最快,摩擦最低,但吃盘口
	{Taker, Maker}, // 秒买挂卖:货立刻到手,卖价好,等成交
	{Maker, Taker}, // 挂买秒卖:买价好,拿到货立刻脱手
	{Maker, Maker}, // 挂买挂卖:价格最好,摩擦最高,两头都要等
}

func (m Mode) Key() string { return m.Buy.String() + "-" + m.Sell.String() }

func (m Mode) Label() string {
	switch m {
	case Mode{Taker, Taker}:
		return "秒买秒卖"
	case Mode{Taker, Maker}:
		return "秒买挂卖"
	case Mode{Maker, Taker}:
		return "挂买秒卖"
	default:
		return "挂买挂卖"
	}
}

// Friction 是这个模式的总摩擦比例,用来排序和显示。
// 真正算钱走 Quote,这里只是个概览数字。
func (m Mode) Friction(cfg conf.Economics) float64 {
	f := 0.0
	if m.Buy == Maker && cfg.BuyOrderSetupFee {
		f += cfg.SetupFee
	}
	if m.Sell == Maker {
		f += cfg.SetupFee
	}
	f += cfg.MarketTax // 卖出永远要交税,不管是挂的还是秒的
	return f
}

// Book 是一侧市场的两个价:别人的最低卖价和最高买价。
type Book struct {
	SellMin int64 // 卖一:我秒买要付这个价
	BuyMax  int64 // 买一:我秒卖能拿到这个价
}

// Quote 按指定执行方式算一件货的账。
//
// 关键在于买卖两腿用的是**不同的价格基准**:
//   - 秒买付卖一,挂买出价在买一之上一档
//   - 秒卖收买一,挂卖报价在卖一之下一档
//
// 用同一个价去算两种模式,是这类计算器最常见的错。
func Quote(buy, sell Book, m Mode, cfg conf.Economics) Unit {
	var myBid, myAsk int64
	cost, revenue := 0.0, 0.0

	if m.Buy == Taker {
		myBid = buy.SellMin
		cost = float64(myBid) // 吃单不收买方任何费用
	} else {
		myBid = buy.BuyMax + cfg.OutbidSilver
		cost = float64(myBid)
		if cfg.BuyOrderSetupFee {
			cost *= 1 + cfg.SetupFee
		}
	}

	if m.Sell == Taker {
		myAsk = sell.BuyMax
		revenue = float64(myAsk) * (1 - cfg.MarketTax)
	} else {
		myAsk = sell.SellMin - cfg.UndercutSilver
		revenue = float64(myAsk) * (1 - cfg.MarketTax - cfg.SetupFee)
	}

	profit := revenue - cost
	margin := 0.0
	if cost > 0 {
		margin = profit / cost
	}
	return Unit{
		MyBid: myBid, MyAsk: myAsk,
		CostPerUnit: cost, RevenuePerUnit: revenue,
		ProfitPerUnit: profit, Margin: margin,
	}
}

// Best 在所有执行方式里挑单件利润最高的那个。
//
// 返回第二个值是模式本身——界面要显示"该怎么挂",
// 不能只给个数字让人猜。
func Best(buy, sell Book, cfg conf.Economics) (Unit, Mode) {
	best := Quote(buy, sell, Modes[0], cfg)
	bestMode := Modes[0]
	for _, m := range Modes[1:] {
		if u := Quote(buy, sell, m, cfg); u.ProfitPerUnit > best.ProfitPerUnit {
			best, bestMode = u, m
		}
	}
	return best, bestMode
}
