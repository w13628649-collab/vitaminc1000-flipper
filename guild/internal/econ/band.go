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
//	挂卖价 = min(卖一 − undercut, 参考价 × (1 + band))
//	挂买价 = max(买一 + outbid,  参考价 × (1 − band))
//
// 参考价是那座城的 7 日成交均价。AODP 的历史只统计卖单成交,也就是"有人按挂卖价买走"
// 的价,正好近似挂卖能成交的价;拿它给挂买定下沿是偏保守的(真正砸给买单的成交价
// 比它低),宁可把收货价抬高、少报利润。
//
// 钳制只会让账变差:买价只升、卖价只降。所以它不会让任何本来不在榜上的机会冒出来。

// Band 是钳制要的参数。零值 = 不钳,和 Quote 逐位一致。
type Band struct {
	// Width 是 filters.maker_price_band:挂单价最多偏离参考价多少(0.2 = ±20%)。0 = 关
	Width float64
	// BuyRef/SellRef 是买入城、卖出城的参考价(7 日成交均价)。0 = 没有,那条腿不钳——
	// 没有历史的交给已有的历史闸门管,那一关过不去的根本到不了这里
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
	// RawBuyPrice/RawSellPrice 是不钳时的价:挂单腿是买一 + outbid / 卖一 − undercut,
	// 也就是改之前界面上显示的那个数;吃单腿从来不钳,和 buy_price / sell_price 相同
	RawBuyPrice  int64 `json:"raw_buy_price"`
	RawSellPrice int64 `json:"raw_sell_price"`
	// RawMargin 是按不钳的价算的毛利率,同理是改之前显示的那个数
	RawMargin float64 `json:"raw_margin"`
	// BuyRef/SellRef 是被封那条腿用的参考价(那座城的 7 日成交均价);没封的腿是 0
	BuyRef  float64 `json:"buy_ref,omitempty"`
	SellRef float64 `json:"sell_ref,omitempty"`
}

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
		if floor := int64(math.Round(b.BuyRef * (1 - b.Width))); floor > bid {
			bid, c.BuyClamped, c.BuyRef = floor, true, b.BuyRef
		}
	}
	if m.Sell == Maker && b.SellRef > 0 {
		if ceil := int64(math.Round(b.SellRef * (1 + b.Width))); ceil < ask {
			ask, c.SellClamped, c.SellRef = ceil, true, b.SellRef
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
