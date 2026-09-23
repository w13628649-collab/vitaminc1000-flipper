package screen

import (
	"fmt"

	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
)

// 深度闸门的拒绝原因。和 Rejected.Reason、ModeQuote.Blocked 同一套取值
const (
	ReasonNoBidSide = "no_bid_side"
	ReasonThinBook  = "thin_book"
)

// LegGate 是按执行方式的腿做的深度闸门:这个模式的挂单腿,盘口撑不撑得住。
//
// buyBid 是买入城的买单簿,sellAsk 是卖出城的卖单簿;同城时两者在同一个城。
// 只看挂单腿,而且只在对应那一边来自抓包、深度可信(Captured)时才判——
// AODP 不给件数,拿它判等于没判;吃单腿的深度问题是"能吃多少",不是"能不能做",
// 跨城那边按阶梯逐档算容量,不在这里拦。
//
//   - 挂买腿看买入城 bid 的近价件数:没人在最优价附近收,说明也没人会往这儿砸货,
//     挂买单进去只会一直挂着,创建费下单就扣、不成交不退
//   - 挂卖腿看卖出城 ask 的近价件数:挂卖价是卖一压一档,卖一要是一张孤单,
//     参考价本身就站不住,按它算的收入是高估的
//
// blocked 非空时这个模式不能做,detail 是给人看的理由;否则 edge 为 true
// 表示贴着阈值,要降置信,warns 是对应的说明。
//
// 阶梯被截断(Truncated)的那一边不判:读到的档全在近价窗口内,近价件数
// 只是下限,而 conf.Validate 保证了件数阈值不超过 book_levels,这时它必然够深。
func LegGate(m econ.Mode, buyBid, sellAsk Side, f conf.Filters) (blocked, detail string, edge bool, warns []string) {
	if m.Buy == econ.Maker && gated(buyBid) {
		d := buyBid.Depth
		pct := nearLabel(d.NearPct, f.NearPct)
		if f.MinBidDepth > 0 && d.QtyNear < f.MinBidDepth {
			return ReasonNoBidSide, fmt.Sprintf(
				"买一 %d 往下 %s 内只有 %d 件、%d 档在收,%s;挂买单大概率收不到货,还白付创建费",
				d.Best, pct, d.QtyNear, d.LevelsNear, cliffNote(d.GapAfterNear)), false, nil
		}
		if f.ThinBidEdgeQty > 0 && d.QtyNear < f.ThinBidEdgeQty {
			edge = true
			warns = append(warns, fmt.Sprintf("买一往下 %s 内只有 %d 件在收,挂买单收货会慢", pct, d.QtyNear))
		}
		if f.BidCliffEdgePct > 0 && d.GapAfterNear >= f.BidCliffEdgePct {
			edge = true
			warns = append(warns, fmt.Sprintf(
				"买方近价 %d 件之外断崖 %.1f%%:顶上这一小撮收完就没人接了", d.QtyNear, d.GapAfterNear*100))
		}
	}
	if m.Sell == econ.Maker && gated(sellAsk) {
		d := sellAsk.Depth
		pct := nearLabel(d.NearPct, f.NearPct)
		if f.MinBookQty > 0 && d.QtyNear < f.MinBookQty {
			return ReasonThinBook, fmt.Sprintf(
				"卖一 %d 往上 %s 内只有 %d 件,撑不起参考价;按它压价挂卖会高估收入",
				d.Best, pct, d.QtyNear), false, nil
		}
		if f.ThinBookEdgeQty > 0 && d.QtyNear < f.ThinBookEdgeQty {
			edge = true
			warns = append(warns, fmt.Sprintf("卖一往上 %s 内只有 %d 件,挂卖的参考价不稳", pct, d.QtyNear))
		}
	}
	return "", "", edge, warns
}

// gated 说明这一边的深度能拿来判:来自抓包、深度可信、没被截断。
func gated(s Side) bool { return s.Captured() && !s.Depth.Truncated }

// nearLabel 把近价窗口写成"5%"这种样子。Analyze 会把 0 补成默认值,
// 所以优先用深度统计自己记下的那个,它才是这次实际用的窗口
func nearLabel(used, cfg float64) string {
	if used <= 0 {
		used = cfg
	}
	return fmt.Sprintf("%.3g%%", used*100)
}

func cliffNote(gap float64) string {
	if gap <= 0 {
		return "近价窗口外再没看到买单"
	}
	return fmt.Sprintf("再往下一档掉 %.1f%%", gap*100)
}
