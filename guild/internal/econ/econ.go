// Package econ 算交易经济学:一单赚多少,一天能做多少。
//
// 核心立场:排序主键是**日化绝对收益**,不是单笔利润率。
// 利润率 30% 但一天只能做 3 件,不如利润率 5% 但一天能做 2000 件。
package econ

import (
	"math"

	"albion-guild/internal/conf"
)

type Unit struct {
	MyBid          int64
	MyAsk          int64
	CostPerUnit    float64
	RevenuePerUnit float64
	ProfitPerUnit  float64
	Margin         float64
}

// UnitEconomics 是模式 A:挂买单收货 → 挂卖单出货。
//
// 挂在现价上只能跟人并排排队,得等前面的人先成交。要真的排到队首,
// 买单得高 OutbidSilver,卖单得低 UndercutSilver——这一步的成本很小,
// 但不算进去就会系统性高估所有机会。
func UnitEconomics(buyPrice, sellPrice int64, cfg conf.Economics) Unit {
	myBid := buyPrice + cfg.OutbidSilver
	myAsk := sellPrice - cfg.UndercutSilver

	buyFeeMultiplier := 1.0
	if cfg.BuyOrderSetupFee {
		buyFeeMultiplier = 1 + cfg.SetupFee
	}
	cost := float64(myBid) * buyFeeMultiplier
	revenue := float64(myAsk) * (1 - cfg.MarketTax - cfg.SetupFee)
	profit := revenue - cost

	margin := 0.0
	if cost > 0 {
		margin = profit / cost
	}
	return Unit{
		MyBid:          myBid,
		MyAsk:          myAsk,
		CostPerUnit:    cost,
		RevenuePerUnit: revenue,
		ProfitPerUnit:  profit,
		Margin:         margin,
	}
}

type Position struct {
	AbsorbableQty float64
	Qty           int64
	CapitalUsed   float64
	DailyProfit   float64
	DailyROI      float64
	CapitalROI    float64
	// CapitalBound 为 true 表示本金是瓶颈(市场还能吃更多);
	// false 表示市场深度是瓶颈。
	CapitalBound bool
}

// SizePosition 算"我能吃下多少而不砸价",再用本金截断。
//
// AbsorbRatio 默认 0.20 是拍脑袋的保守值。第二阶段用实盘成交率记录
// 反过来校准它——那才是这个项目相对现成工具的长期价值所在。
func SizePosition(u Unit, dailyVolumeQty float64, capital int64, cfg conf.Sizing) Position {
	absorbable := dailyVolumeQty * cfg.AbsorbRatio
	maxByCapital := 0.0
	if u.CostPerUnit > 0 {
		maxByCapital = float64(capital) / u.CostPerUnit
	}
	qty := int64(math.Floor(math.Min(absorbable, maxByCapital)))

	capitalUsed := float64(qty) * u.CostPerUnit
	dailyProfit := u.ProfitPerUnit * float64(qty)

	capitalROI := 0.0
	if capital > 0 {
		capitalROI = dailyProfit / float64(capital)
	}
	return Position{
		AbsorbableQty: absorbable,
		Qty:           qty,
		CapitalUsed:   capitalUsed,
		DailyProfit:   dailyProfit,
		// 同城假设一天一轮,所以 DailyROI 数值上就等于单笔 Margin。
		DailyROI: u.Margin,
		// 这条才真正回答"这个机会对我这 1000 万有多大意义":
		// margin 再高,只吃得下 3 件也就是三瓜两枣。
		CapitalROI:   capitalROI,
		CapitalBound: maxByCapital < absorbable,
	}
}
