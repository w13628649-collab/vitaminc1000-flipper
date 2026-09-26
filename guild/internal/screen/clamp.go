package screen

import (
	"fmt"

	"albion-guild/internal/econ"
	"albion-guild/internal/histagg"
)

// MakerBand 是这一条腿对、这两城的挂单腿带宽参数(econ.QuoteBanded)。
// 参考价是各自那座城的 7 日成交均价,和偏离度判据同一个数;没有统计时是 0,那条腿不钳。
// 同城两个都传同一份 stats,跨城传产地、销地各自的。
func MakerBand(width float64, buyStats, sellStats *histagg.Stats) econ.Band {
	return econ.Band{Width: width, BuyRef: avg7d(buyStats), SellRef: avg7d(sellStats)}
}

func avg7d(s *histagg.Stats) float64 {
	if s == nil {
		return 0
	}
	return s.AvgPrice7d
}

// BothClampedWarning 是两条挂单腿都被封时的那句话。这时两个挂价都是按带宽推出来的,
// 毛利跟着 filters.maker_price_band 走、不跟着盘口走(审查实测:同一批这种机会 band 0.2 时
// 毛利全落在 36.6~37.0%,0.15 时 23.2~23.7%,0.1 时 11.4~11.9%)。可信度要压到 low
func BothClampedWarning(c *econ.Clamp) string {
	return fmt.Sprintf("两边盘口都离参考价超过 ±%s:挂买价、挂卖价都是按带宽推出来的,不是盘口给的,"+
		"这个价差跟着带宽走、不跟着盘口走,可信度按 low 算", bandPct(c.Band))
}

func bandPct(b float64) string { return fmt.Sprintf("%.3g%%", b*100) }

// ClampWarnings 把挂单腿被封这件事写成人话,放进 warnings。
//
// bookBid 是挂买腿排的那个买单簿的买一,bookAsk 是挂卖腿排的那个卖单簿的卖一——
// 说"卖一是均价的 2.1 倍"要用盘口原价,不是压过一档的挂价。buyWhere/sellWhere 是
// "产地"/"销地"(跨城),同城传空串。buy/sell 是封过之后的挂价。
//
// 参考价取自对面盘口(econ.RefSellMin / RefBuyMax)时要说清楚:这时均价已经跟不上行情,
// 光说"按 7 日均价"会和格子里的均价对不上。
//
// 价一律千分位:这些句子原样显示在界面上,旁边格子里都是 9,995 这种写法
func ClampWarnings(c *econ.Clamp, buy, sell, bookBid, bookAsk int64, buyWhere, sellWhere string) []string {
	if c == nil {
		return nil
	}
	band := bandPct(c.Band)
	var out []string
	if c.BuyClamped && c.BuyRef > 0 {
		if c.BuyRefFrom == econ.RefSellMin {
			out = append(out, fmt.Sprintf("挂买价按%s卖一 %s 下浮 %s 抬到 %s:整本盘口已跌到 7 日成交均价以下,均价跟不上行情,改按卖一算;"+
				"买一 %s 只有卖一的 %.2f 倍,照它挂多半收不到货",
				buyWhere, thousandsF(c.BuyRef), band, Thousands(buy), Thousands(bookBid), float64(bookBid)/c.BuyRef))
		} else {
			out = append(out, fmt.Sprintf("挂买价按%s 7 日成交均价 %s 下浮 %s 抬到 %s:买一 %s 只有均价的 %.2f 倍,照它挂多半收不到货",
				buyWhere, thousandsF(c.BuyRef), band, Thousands(buy), Thousands(bookBid), float64(bookBid)/c.BuyRef))
		}
	}
	if c.SellClamped && c.SellRef > 0 {
		if c.SellRefFrom == econ.RefBuyMax {
			out = append(out, fmt.Sprintf("挂卖价按%s买一 %s 上浮 %s 封到 %s:整本盘口已涨过 7 日成交均价,均价跟不上行情,改按买一算;"+
				"卖一 %s 是买一的 %.2f 倍,照它挂多半没人买",
				sellWhere, thousandsF(c.SellRef), band, Thousands(sell), Thousands(bookAsk), float64(bookAsk)/c.SellRef))
		} else {
			out = append(out, fmt.Sprintf("挂卖价按%s 7 日成交均价 %s 上浮 %s 封到 %s:卖一 %s 是均价的 %.2f 倍,照它挂多半没人买",
				sellWhere, thousandsF(c.SellRef), band, Thousands(sell), Thousands(bookAsk), float64(bookAsk)/c.SellRef))
		}
	}
	if c.Both() {
		out = append(out, BothClampedWarning(c))
	}
	return out
}
