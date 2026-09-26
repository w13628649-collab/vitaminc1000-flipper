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
	// Seen 是这一边最近一次被看到的时刻(book.Side.Newest)。LevelSeen 没给的档按它算年龄
	Seen time.Time `json:"seen"`
	// LevelSeen 和 Levels 一一对应:每一档自己最近一次被看到的时刻(book.Level.Seen)。
	// 可以不给(空);给了就要和 Levels 一样长,零值的档退回 Seen。
	//
	// 为什么要按档记:一边的阶梯可能拼了几次浏览(续页之前那一页、串城时暂停剔除留下的旧单),
	// 整边只记最近一眼的话,吃到的旧档会被报成刚看过的(审查 finding 2)。步骤清单和候选的
	// 新鲜度取实际吃到的最旧那一档,比 max_age_hours 还旧的档不用
	LevelSeen []time.Time `json:"level_seen,omitempty"`
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
	levels []depth.Level
	// seens 和 levels 一一对应,每一档的观测时刻(没给的是整边的 seen)
	seens        []time.Time
	seen         time.Time
	truncated    bool
	source       string
	depthUnknown bool
}

// seenAt 是第 i 档的观测时刻。
func (l ladderSide) seenAt(i int) time.Time {
	if i >= 0 && i < len(l.seens) {
		return l.seens[i]
	}
	return l.seen
}

// oldestSeen 是前 n 档里最旧的观测时刻(零值 = 不知道)。
func (l ladderSide) oldestSeen(n int) time.Time {
	var oldest time.Time
	for i := 0; i < n && i < len(l.levels); i++ {
		if s := l.seenAt(i); !s.IsZero() && (oldest.IsZero() || s.Before(oldest)) {
			oldest = s
		}
	}
	return oldest
}

// normalize 按方向排好序、去掉 0 价 0 件的档。depthUnknown 时只留最优那一档。
func normalizeLadder(l Ladder, ascending bool) ladderSide {
	out := ladderSide{seen: l.Seen, truncated: l.Truncated, source: l.Source, depthUnknown: l.DepthUnknown}
	type row struct {
		lv   depth.Level
		seen time.Time
	}
	var rows []row
	for i, lv := range l.Levels {
		if lv.Price <= 0 || (lv.Qty <= 0 && !l.DepthUnknown) {
			continue
		}
		seen := l.Seen
		if len(l.LevelSeen) == len(l.Levels) && !l.LevelSeen[i].IsZero() {
			seen = l.LevelSeen[i]
		}
		rows = append(rows, row{lv, seen})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if ascending {
			return rows[i].lv.Price < rows[j].lv.Price
		}
		return rows[i].lv.Price > rows[j].lv.Price
	})
	if l.DepthUnknown && len(rows) > 1 {
		rows = rows[:1]
	}
	for _, r := range rows {
		out.levels = append(out.levels, r.lv)
		out.seens = append(out.seens, r.seen)
	}
	if out.source == "" {
		out.source = "capture"
	}
	return out
}

// keepFresh 只留 tooOld 说不旧的档(观测时刻不知道的也留着)。
func (l ladderSide) keepFresh(tooOld func(time.Time) bool) ladderSide {
	out := l
	out.levels, out.seens = nil, nil
	for i, lv := range l.levels {
		if s := l.seenAt(i); !s.IsZero() && tooOld(s) {
			continue
		}
		out.levels = append(out.levels, lv)
		out.seens = append(out.seens, l.seenAt(i))
	}
	return out
}
