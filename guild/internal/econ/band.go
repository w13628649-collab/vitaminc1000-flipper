package econ

import (
	"math"

	"albion-guild/internal/conf"
)

// 挂单腿的价格带宽钳制。
//
// **吃单腿的价是真的,挂单腿的价是假设的。** 秒买吃的是盘口上真挂着的卖单,
// 秒卖砸的是真挂着的买单,那边有人等着成交。挂单腿不一样:
//
//   - "挂卖价 = 卖一 − undercut"的前提是有人愿意按这个价买。卖一已经是历史成交价的
//     两倍时这个前提不成立:挂上去没人买,真要出货只能降到成交价附近
//   - "挂买价 = 买一 + outbid"的前提是有人愿意按这个价卖给我。买一远低于历史成交价时
//     同理:挂在买一上收不到货(docs/flipper.md「买方到底存不存在」:高价附魔材料的
//     成交均价贴着卖价,没人砸到买价上)
//
// 以前两条腿都按盘口价算,于是卖一挂成均价两倍的物品排到了机会页最前面
// (T4_WOOD_LEVEL3@3 Bridgewatch → Thetford 挂买挂卖 4,358 → 9,995,毛利 109%,
// 而 Thetford 7 日成交均价只有 4,770)。现在挂单腿按参考价做带宽钳制:
//
//	挂卖价 = min(卖一 − undercut, 卖侧参考价 × (1 + band))
//	挂买价 = max(买一 + outbid,  买侧参考价 × (1 − band))
//
// 参考价是那座城的 7 日成交均价。AODP 的历史只统计卖单成交,也就是"有人按挂卖价买走"
// 的价,正好近似挂卖能成交的价;拿它给挂买定下沿是偏保守的(真正砸给买单的成交价
// 比它低),宁可把收货价抬高、少报利润。
//
// **参考价还要和对面盘口比一次**:买侧取 min(均价, 同本卖一),卖侧取 max(均价, 同本买一)。
// 对面盘口是真有人在等着成交的价,均价是过去 7 天的。整本盘口已经跌到均价下面(卖一 < 均价)
// 时均价是旧的:按它抬挂买价,会把挂买价抬过卖一(审查实测 T4_CLOTH_LEVEL1@1 @ Bridgewatch
// 买一 417、卖一 450、均价 587,下沿 470 高过卖一),进游戏照抄就是一张比秒买还贵、还要付
// 创建费和等成交的错单;单边下跌里盘口紧、有量的正常机会也会被平白压薄。涨过均价时对称。
// 取整之后仍碰到对面价的(价很小或带宽很窄),再夹回对面价内侧 1 银。
//
// 钳制只会让账变差:买价只升、卖价只降。所以它不会让任何本来不在榜上的机会冒出来。

// 参考价取自哪里(Clamp.BuyRefFrom / SellRefFrom)
const (
	RefAvg7d   = "avg_7d"   // 那座城的 7 日成交均价
	RefSellMin = "sell_min" // 同本盘口的卖一:卖一已经低于均价,均价跟不上行情
	RefBuyMax  = "buy_max"  // 同本盘口的买一:买一已经高过均价,均价跟不上行情
)

// Band 是钳制要的参数。零值 = 不钳,和 Quote 逐位一致。
type Band struct {
	// Width 是 filters.maker_price_band:挂单价最多偏离参考价多少(0.2 = ±20%)。0 = 关
	Width float64
	// BuyRef/SellRef 是买入城、卖出城的 7 日成交均价。0 = 没有,那条腿不钳——没有历史的
	// 交给已有的历史闸门管,那一关过不去的根本到不了这里。实际用的参考价还要和对面盘口
	// 比一次,见文件头
	BuyRef, SellRef float64
}

// Clamp 说明哪条挂单腿被封过、封之前是多少,对外 JSON 用。
//
// 只在至少一条腿被封过时出现(Banded.Clamp 非 nil),所以 band = 0 或者谁都没被封时
// 输出和改动之前逐字相同。
type Clamp struct {
	// Band 是这次用的带宽(filters.maker_price_band)
	Band        float64 `json:"band"`
	BuyClamped  bool    `json:"buy_clamped"`
	SellClamped bool    `json:"sell_clamped"`
	// RawBuyPrice/RawSellPrice 是不钳时的价:挂单腿是买一 + outbid / 卖一 − undercut;
	// 吃单腿从来不钳。QuoteBanded 给的是**同一个盘口价**上不钳的价;跨城吃单腿要沿阶梯走,
	// arb 会把它换成"不钳时会挑中的那一档"(钳过之后挑中的深度可能不同),两处都是
	// band = 0 时界面上显示的那个数
	RawBuyPrice  int64 `json:"raw_buy_price"`
	RawSellPrice int64 `json:"raw_sell_price"`
	// RawMargin 是按上面两个价算的毛利率
	RawMargin float64 `json:"raw_margin"`
	// BuyRef/SellRef 是被封那条腿实际用的参考价;没封的腿是 0
	BuyRef  float64 `json:"buy_ref,omitempty"`
	SellRef float64 `json:"sell_ref,omitempty"`
	// BuyRefFrom/SellRefFrom 说明参考价取自哪里:RefAvg7d(7 日成交均价),或者对面盘口
	// (RefSellMin / RefBuyMax,盘口已经整体越过均价、均价是旧的)。没封的腿是空串
	BuyRefFrom  string `json:"buy_ref_from,omitempty"`
	SellRefFrom string `json:"sell_ref_from,omitempty"`
}

// Both 说明两条挂单腿都被封过。这时挂买价、挂卖价都不是盘口给的,毛利只由带宽和两个
// 参考价决定(同城 band 0.2 时恒约 37%),跟盘口本身没关系——调用方要把可信度压到 low
func (c *Clamp) Both() bool { return c != nil && c.BuyClamped && c.SellClamped }

// Banded 是 QuoteBanded 的结果。
type Banded struct {
	// Unit 是钳过的账:进游戏照它挂,税费、盈亏平衡、利润、日收益全按它算
	Unit
	// Raw 是不钳的账,只给 troll 判据用(同城 max_margin、跨城 excessMargin、查价页的
	// suspect)。那几道判的是"盘口本身离不离谱":拿钳过的毛利去判,带宽会把挂买挂卖的毛利
	// 封在 (1+band)/(1−band) × 0.935/1.025 − 1 以内(band 0.2 时约 37%),max_margin
	// 对挂单模式就永远不响了——以前被当成 troll 拦掉的盘口,会顶着一个编出来的价回到榜上
	Raw Unit
	// Clamp 非 nil 说明至少一条挂单腿被封过
	Clamp *Clamp
}

// QuoteBanded 是带挂单腿带宽钳制的 Quote。同城 screen、跨城 arb、查价页共用这一个,
// 同一个盘口在三处的挂价因此是同一个数。b.Width <= 0 时和 Quote 逐位一致,Clamp 为 nil。
//
// 挂买腿的对面是 buy.SellMin(挂买排在买入城的买单簿上,对面是同一座城的卖单),
// 挂卖腿的对面是 sell.BuyMax。两者都只取决于挂单腿所在那本盘口,和吃单腿走到第几档无关
func QuoteBanded(buy, sell Book, m Mode, cfg conf.Economics, b Band) Banded {
	raw := Quote(buy, sell, m, cfg)
	out := Banded{Unit: raw, Raw: raw}
	if b.Width <= 0 {
		return out
	}
	bid, ask := raw.MyBid, raw.MyAsk
	var c Clamp
	// 四舍五入到整银:游戏只能挂整数价。用 Round 不用 Floor/Ceil,
	// 是因为 100 × (1 + 0.15) 在浮点里是 114.999…,Floor 会平白少 1 银
	if m.Buy == Maker && b.BuyRef > 0 {
		ref, from := b.BuyRef, RefAvg7d
		if buy.SellMin > 0 && float64(buy.SellMin) < ref {
			ref, from = float64(buy.SellMin), RefSellMin
		}
		floor := int64(math.Round(ref * (1 - b.Width)))
		if buy.SellMin > 0 && floor >= buy.SellMin {
			floor = buy.SellMin - 1
		}
		if floor > bid {
			bid, c.BuyClamped, c.BuyRef, c.BuyRefFrom = floor, true, ref, from
		}
	}
	if m.Sell == Maker && b.SellRef > 0 {
		ref, from := b.SellRef, RefAvg7d
		if sell.BuyMax > 0 && float64(sell.BuyMax) > ref {
			ref, from = float64(sell.BuyMax), RefBuyMax
		}
		ceil := int64(math.Round(ref * (1 + b.Width)))
		if sell.BuyMax > 0 && ceil <= sell.BuyMax {
			ceil = sell.BuyMax + 1
		}
		if ceil < ask {
			ask, c.SellClamped, c.SellRef, c.SellRefFrom = ceil, true, ref, from
		}
	}
	if !c.BuyClamped && !c.SellClamped {
		return out
	}
	c.Band = b.Width
	c.RawBuyPrice, c.RawSellPrice, c.RawMargin = raw.MyBid, raw.MyAsk, raw.Margin
	out.Unit = unitAt(bid, ask, m, cfg)
	out.Clamp = &c
	return out
}
