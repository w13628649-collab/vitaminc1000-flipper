package econ

import "albion-guild/internal/conf"

// BuyMul 是单独一条买入腿的费用系数:成本 = 成交价 × 它。
//
// 导出给精炼(refine)用。精炼的买料和卖货不成对出现——一根钢条要买几种料、
// 有的吃卖单有的挂买单,卖货又是另一回事——套不进 Mode 的「一买一卖」。
// 这里只是把腿拆出来,税费逻辑仍然只有 buyMul/sellMul 这一份:
// 分成两份写过一次,改税率时漏掉一边,「盈亏平衡」和「实际算账」就对不上了。
func BuyMul(l Leg, cfg conf.Economics) float64 { return Mode{Buy: l}.buyMul(cfg) }

// SellMul 是单独一条卖出腿的到手系数:到手 = 成交价 × 它。
func SellMul(l Leg, cfg conf.Economics) float64 { return Mode{Sell: l}.sellMul(cfg) }
