package screen

import (
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/depth"
	"albion-guild/internal/econ"
)

// 一个盘口边的价格来自哪。两个来源逐边选一个,从不取平均。
const (
	SourceAODP    = "aodp"
	SourceCapture = "capture"
)

// DepthView 是抓包那一边在"深度可信窗口"内的件数统计。
// Truncated 说明阶梯在 capture.book_levels 处被截断、而读到的档全在近价窗口内:
// 这时近价件数只是下限,深度判据不能拿它判拒
type DepthView struct {
	depth.Support
	Truncated bool `json:"truncated,omitempty"`
}

// AltQuote 是落选的那个来源的报价,留给界面对照:
// "AODP 说 1200、5 分钟前;抓包说 1190、30 分钟前"
type AltQuote struct {
	Source   string  `json:"source"`
	Price    int64   `json:"price"`
	AgeHours float64 `json:"age_hours"`
}

// Side 是订单簿的一边(ask = 卖单簿,bid = 买单簿)最后用了谁的价、多旧、深度如何。
//
// Depth 只在这一边来自抓包、且最优档落在深度可信窗口内时才有;
// AODP 那一边一律没有 Depth——它不给件数,深度判据对它不生效。
type Side struct {
	Source   string     `json:"source"`
	Price    int64      `json:"price"`
	AgeHours float64    `json:"age_hours"`
	Depth    *DepthView `json:"depth,omitempty"`
	Alt      *AltQuote  `json:"alt,omitempty"`
	Note     string     `json:"note,omitempty"`
	// Levels 是参与深度统计的那几档(从优到劣),给后面按档吃单用。不进 JSON:
	// 128 档 × 几百条机会,响应体会大一个数量级
	Levels []depth.Level `json:"-"`
}

// Captured 说明这一边的深度判据可以用:来自抓包,而且有可信的件数。
func (s Side) Captured() bool {
	return s.Source == SourceCapture && s.Depth != nil && s.Depth.Best > 0
}

// Sides 是一个 (物品, 城市, 品质) 的两边。零值就是纯 AODP。
type Sides struct {
	Ask Side
	Bid Side
}

// legSides 把执行方式的两条腿映射到订单簿的两边:
// 秒买吃卖单(ask)、挂买排在买单簿(bid)上;秒卖吃买单(bid)、挂卖排在卖单簿(ask)上。
func legSides(m econ.Mode, s Sides) (buyLeg, sellLeg Side) {
	buyLeg, sellLeg = s.Bid, s.Ask
	if m.Buy == econ.Taker {
		buyLeg = s.Ask
	}
	if m.Sell == econ.Taker {
		sellLeg = s.Bid
	}
	return buyLeg, sellLeg
}

// withAODP 给没被融合层填过的那一边补上 AODP 口径。
// 调用方传零值 Sides(纯 AODP 扫描、现有测试)时,输出照样带着来源和数据龄,
// 界面不用区分"没融合"和"融合了但 AODP 胜出"两种情况
func (s Side) withAODP(price int64, at aodp.Stamp, now time.Time) Side {
	if s.Source != "" || price <= 0 {
		return s
	}
	s.Source, s.Price = SourceAODP, price
	if age, ok := at.AgeHours(now); ok {
		s.AgeHours = age
	}
	return s
}
