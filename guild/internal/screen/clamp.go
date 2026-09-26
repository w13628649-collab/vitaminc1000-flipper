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

// ClampWarnings 把挂单腿被封这件事写成人话,放进 warnings。
//
// bookBid 是挂买腿排的那个买单簿的买一,bookAsk 是挂卖腿排的那个卖单簿的卖一——
// 说"卖一是均价的 2.1 倍"要用盘口原价,不是压过一档的挂价。buyWhere/sellWhere 是
// "产地"/"销地"(跨城),同城传空串。buy/sell 是封过之后的挂价。
//
// 价一律千分位:这些句子原样显示在界面上,旁边格子里都是 9,995 这种写法
func ClampWarnings(c *econ.Clamp, buy, sell, bookBid, bookAsk int64, buyWhere, sellWhere string) []string {
	if c == nil {
		return nil
	}
	band := fmt.Sprintf("%.3g%%", c.Band*100)
	var out []string
	if c.BuyClamped && c.BuyRef > 0 {
		out = append(out, fmt.Sprintf("挂买价按%s 7 日成交均价 %s 下浮 %s 抬到 %s:买一 %s 只有均价的 %.2f 倍,照它挂多半收不到货",
			buyWhere, thousandsF(c.BuyRef), band, Thousands(buy), Thousands(bookBid), float64(bookBid)/c.BuyRef))
	}
	if c.SellClamped && c.SellRef > 0 {
		out = append(out, fmt.Sprintf("挂卖价按%s 7 日成交均价 %s 上浮 %s 封到 %s:卖一 %s 是均价的 %.2f 倍,照它挂多半没人买",
			sellWhere, thousandsF(c.SellRef), band, Thousands(sell), Thousands(bookAsk), float64(bookAsk)/c.SellRef))
	}
	return out
}
