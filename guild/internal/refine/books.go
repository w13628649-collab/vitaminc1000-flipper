package refine

import (
	"sort"
	"time"

	"albion-guild/internal/catalog"
	"albion-guild/internal/depth"
)

// Key 是一个物品在一个城市的盘口。只算品质 1:资源和半成品都没有品质之分。
type Key struct {
	ItemID string `json:"item"`
	City   string `json:"city"`
}

// Ladder 是一个盘口边整理好的阶梯。
//
// 两个方向分开记观测时间和截断:抓包里卖单和买单常常不是同一次响应,
// 一边翻了三页、另一边只看了第一页是常事(book.Side 本来就是按边的)。
type Ladder struct {
	// Levels 从优到劣:卖单价格升序,买单降序。引擎会防御性地再排一次
	Levels []depth.Level `json:"levels"`
	// Seen 是这一边最近一次被看到的时刻(book.Side.Newest),新鲜度按它算
	Seen time.Time `json:"seen"`
	// Truncated:最近一轮是满页,更深的档没翻到(规则表 G1)。吃穿可见深度时
	// 算出来的成本只是下界
	Truncated bool `json:"truncated,omitempty"`
	// Source 是价格来源:capture(抓包)/ aodp / 其他调用方自定的标签
	Source string `json:"source,omitempty"`
	// DepthUnknown:只有价格、没有件数(AODP 兜底)。这时只用第一档的价,
	// 件数按不限量算,并在结果里标「深度未知」
	DepthUnknown bool `json:"depth_unknown,omitempty"`
}

// empty:这一边没有任何可用的档。
func (l Ladder) empty() bool {
	for _, lv := range l.Levels {
		if lv.Price > 0 && (lv.Qty > 0 || l.DepthUnknown) {
			return false
		}
	}
	return true
}

// Book 是一个 物品 × 城市 的两侧。
type Book struct {
	Asks Ladder `json:"asks"`
	Bids Ladder `json:"bids"`
}

// Books 是一次计算用到的全部盘口。缺的 key 就是缺价。
type Books map[Key]Book

// Volume 是目标产物在出售城市的成交量。由调用方从成交历史算好。
type Volume struct {
	// PerDay 按「总量 ÷ 自然日」算(设计 §2.5),不用 histagg 的「÷ 有数据的天数」:
	// 后者会高估稀疏的物品
	PerDay       float64 `json:"per_day"`
	Days         int     `json:"days"`
	DaysWithData int     `json:"days_with_data"`
	Source       string  `json:"source,omitempty"`
	// Known=false 表示没有成交数据(比如 AODP 覆盖空洞,规则表 G3)。
	// 这时成交量不参与日化封顶,只标「无数据」——不能当成 0
	Known bool `json:"known"`
}

// Input 是一次计算的全部输入。引擎不连库、不看钟:同一份 Input 永远得到同一个结果。
type Input struct {
	Params  Params
	Books   Books
	Recipes *catalog.RecipeBook
	Volume  Volume
}

// ladderSide 是一边阶梯在引擎里的样子:排好序、去掉空档。
type ladderSide struct {
	levels       []depth.Level
	seen         time.Time
	truncated    bool
	source       string
	depthUnknown bool
}

// normalize 按方向排好序、去掉 0 价 0 件的档。depthUnknown 时只留最优那一档。
func normalizeLadder(l Ladder, ascending bool) ladderSide {
	out := ladderSide{seen: l.Seen, truncated: l.Truncated, source: l.Source, depthUnknown: l.DepthUnknown}
	for _, lv := range l.Levels {
		if lv.Price <= 0 || (lv.Qty <= 0 && !l.DepthUnknown) {
			continue
		}
		out.levels = append(out.levels, lv)
	}
	sort.SliceStable(out.levels, func(i, j int) bool {
		if ascending {
			return out.levels[i].Price < out.levels[j].Price
		}
		return out.levels[i].Price > out.levels[j].Price
	})
	if l.DepthUnknown && len(out.levels) > 1 {
		out.levels = out.levels[:1]
	}
	if out.source == "" {
		out.source = "capture"
	}
	return out
}
