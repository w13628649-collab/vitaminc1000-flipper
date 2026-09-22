// Package depth 回答"我要这么多货,实际成交均价是多少"。
//
// 最优价只是**第一件**的价格。要买 5000 件就得往下吃好几档,
// 真实成交均价一定比卖一差。只看最优价会系统性高估每一条机会——
// 而且越是想做大单,高估得越厉害。
//
// 这是抓包数据独有的能力:AODP 的 prices 端点只给价格不给数量,
// 拿它根本算不出这件事。
package depth

// Level 是盘口的一档。
type Level struct {
	Price int64 `json:"price"`
	Qty   int64 `json:"qty"`
}

// Fill 是吃掉目标数量之后的结果。
type Fill struct {
	// Want 是想要的数量,Got 是盘口实际能给的。Got < Want 说明吃穿了
	Want int64 `json:"want"`
	Got  int64 `json:"got"`
	// VWAP 是成交量加权的实际均价,Best 是最优价(第一档)
	VWAP float64 `json:"vwap"`
	Best int64   `json:"best"`
	// Worst 是吃到的最差那一档的价格
	Worst int64 `json:"worst"`
	// Slippage 是 (VWAP - Best) / Best,买入为正、卖出取绝对值。
	// 这个数直接回答"做大单要多付多少代价"
	Slippage float64 `json:"slippage"`
	Total    float64 `json:"total"`
	// Levels 是实际吃到的档位,界面上画阶梯图
	Levels []Level `json:"levels"`
	// Filled 为 false 表示盘口不够深,拿不到想要的量
	Filled bool `json:"filled"`
}

// Walk 按价格从优到劣吃掉 want 件。
//
// 调用方负责把 levels 排好序:买入(吃卖单)时价格升序,
// 卖出(吃买单)时价格降序。函数本身不关心方向,只顺序吃——
// 这样同一份逻辑两边都能用。
func Walk(levels []Level, want int64) Fill {
	f := Fill{Want: want}
	if want <= 0 || len(levels) == 0 {
		return f
	}
	f.Best = levels[0].Price

	remaining := want
	for _, l := range levels {
		if remaining <= 0 {
			break
		}
		if l.Qty <= 0 {
			continue
		}
		take := l.Qty
		if take > remaining {
			take = remaining
		}
		f.Levels = append(f.Levels, Level{Price: l.Price, Qty: take})
		f.Total += float64(l.Price) * float64(take)
		f.Worst = l.Price
		f.Got += take
		remaining -= take
	}
	if f.Got > 0 {
		f.VWAP = f.Total / float64(f.Got)
		if f.Best > 0 {
			f.Slippage = (f.VWAP - float64(f.Best)) / float64(f.Best)
			if f.Slippage < 0 {
				f.Slippage = -f.Slippage // 卖单方向价格递减,滑点取绝对值
			}
		}
	}
	f.Filled = f.Got >= want
	return f
}

// Support 回答一个 Walk 回答不了的问题:**这个最优价背后到底有没有人**。
//
// Walk 问的是"我要买 N 件、实际要付多少",是吃单方的视角。挂单方的问题
// 反过来:我挂个买单在这儿,会不会有人砸给我?两者需要的判据完全不同。
//
// 判据是最优价**附近**的件数,不是总件数——挂单簿底下永远躺着一堆
// 1 银的占位单。实测 T6_METALBAR_LEVEL4@4 @ Martlock 的买单阶梯:
//
//	260,066   -0.00%      1 件
//	260,065   -0.00%      2 件
//	260,064   -0.00%      1 件
//	 ...(前 7 档合计 11 件)
//	 53,001  -79.62%     15 件   ← 断崖
//	      1 -100.00%   2,000+ 件  ← 占位单
//
// 买方总件数 2,890,但最优价 5% 以内只有 11 件(0.4%)。对照同城的大宗
// T4_METALBAR @ Thetford:285(3,116 件)/ 284(1,999)/ 283(4,927)… 是连续
// 阶梯,5% 以内 37,379 件(35%)。**只有近价件数分得开这两种形态。**
//
// tier 越高买方越薄、断崖越深;而纸面毛利恰恰是那些最诱人的。
type Support struct {
	Best      int64 `json:"best"`
	QtyAtBest int64 `json:"qty_at_best"`
	// QtyNear 是最优价 NearPct 以内的件数合计 —— 判"有没有人在收"就看它
	QtyNear int64 `json:"qty_near"`
	// QtyTotal 含 1 银那种占位单,只适合当参考,别拿它当深度
	QtyTotal   int64 `json:"qty_total"`
	LevelsNear int   `json:"levels_near"`
	// GapAfterNear 是从最优价到**近价窗口之外第一档**的落差。
	// 不是"到下一档":顶上那几档常常只差 1 银,测下一档永远是 0。
	// 要测的是"顶上这一小撮吃完之后,下一笔流动性在多远"
	GapAfterNear float64 `json:"gap_after_near"`
	NearPct      float64 `json:"near_pct"`
}

// Analyze 统计最优价附近的支撑。levels 的顺序约定和 Walk 一致:从优到劣
// (买单降序、卖单升序),levels[0] 是最优价。
func Analyze(levels []Level, nearPct float64) Support {
	s := Support{NearPct: nearPct}
	if len(levels) == 0 {
		return s
	}
	if nearPct <= 0 {
		nearPct = 0.05
		s.NearPct = nearPct
	}
	// 跳过空档再定锚,否则 Best 会锚在一个没有货的价上
	first := -1
	for i, l := range levels {
		if l.Qty > 0 {
			first = i
			break
		}
	}
	if first < 0 {
		return s
	}
	s.Best = levels[first].Price

	// 方向由数据本身定:买单降序、卖单升序
	descending := false
	for _, l := range levels[first+1:] {
		if l.Qty > 0 && l.Price != s.Best {
			descending = l.Price < s.Best
			break
		}
	}
	within := func(p int64) bool {
		if descending {
			return float64(p) >= float64(s.Best)*(1-nearPct)
		}
		return float64(p) <= float64(s.Best)*(1+nearPct)
	}

	outside := int64(0)
	hasOutside := false
	for _, l := range levels {
		if l.Qty <= 0 {
			continue
		}
		s.QtyTotal += l.Qty
		switch {
		case l.Price == s.Best:
			s.QtyAtBest += l.Qty
			s.QtyNear += l.Qty
			s.LevelsNear++
		case within(l.Price):
			s.QtyNear += l.Qty
			s.LevelsNear++
		case !hasOutside:
			outside, hasOutside = l.Price, true
		}
	}
	if hasOutside && s.Best > 0 {
		s.GapAfterNear = float64(outside)/float64(s.Best) - 1
		if s.GapAfterNear < 0 {
			s.GapAfterNear = -s.GapAfterNear
		}
	}
	return s
}

// FillPosition 是成交均价落在买卖价之间的什么位置。
//
//	→ 1  成交都贴着卖价,货是被人**按卖价买走**的,没人肯砸到买价上;
//	     挂买单进去大概率一直挂着,而创建费是下单当场扣、不成交也不退
//	→ 0  成交贴着买价,买单容易成交,难的是把货挂出去
//	→ 0.5 两侧都在成交,双挂说得通
//
// 实测两个高价附魔材料都是 0.96~0.97(游戏内「市场历史」的平均价紧贴
// 最低卖价),而它们的纸面价差是 38% —— 光看价差会以为是天大的机会。
//
// 注意口径限制:AODP 的 history item_count 只统计卖单成交,拿它算出来的
// 均价天生偏向卖价一侧。抓包的 markethistories 才是完整的。
func FillPosition(bid, ask int64, avg float64) (float64, bool) {
	if bid <= 0 || ask <= bid || avg <= 0 {
		return 0, false
	}
	return (avg - float64(bid)) / float64(ask-bid), true
}

// MaxQtyWithin 是"不超过这个价的前提下最多能吃多少"。
//
// 另一个问法:与其问"5000 件要多少钱",不如问"我只接受到 1100 为止,
// 那能吃到几件"。挂单党更常问后面这个。
func MaxQtyWithin(levels []Level, limit int64, buying bool) (qty int64, cost float64) {
	for _, l := range levels {
		if buying && l.Price > limit {
			break
		}
		if !buying && l.Price < limit {
			break
		}
		qty += l.Qty
		cost += float64(l.Price) * float64(l.Qty)
	}
	return qty, cost
}
