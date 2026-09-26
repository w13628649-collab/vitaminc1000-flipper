// Package book 把一个盘口边的原始挂单整理成阶梯:剔掉上一轮浏览留下的残单(幽灵单),
// 认出续页和没翻完的截断。
//
// 扫描(scan 的读簿)、WS 报价 / /api/quotes 和查价页(/api/lookup/grid、/api/lookup/book)
// 都走这里的 Build。以前扫描和报价用 store 里一条 SQL 剔幽灵单、查价页用另一套 Go 规则,
// 同一个盘口在机会页和查价页上能报出两个最优价;规则收成一份,两边才是同一个样子。
//
// 纯函数:不连库、不看钟,时间只来自每张单自己的 last_seen。
package book

import (
	"sort"
	"time"

	"albion-guild/internal/model"
)

// PageSize 是游戏一次响应最多回多少张单。
//
// **从数据形态推出来的,没看协议源码确认**:24 小时里 149 组 last_seen 完全相同的单
// 恰好 50 张、没有一组超过;103 个盘口恰好 50 单;T1_WOOD @ Martlock 分 7 次首尾相接到达。
// 游戏哪天改了分页大小,这里不改的话续页和截断都会认错(满页认不出来)。
const PageSize = 50

// Order 是一张活跃挂单的一次观测。**不按价位聚合**:判"这张单是不是上一轮
// 浏览留下来的"要看每张单自己的 last_seen,聚合以后就分不开了。
type Order struct {
	ItemID  string
	City    string
	Quality int
	Side    model.Side
	Price   int64
	Amount  int64
	// FirstSeen 只有查价页读(挂了多久);批量读簿不取——它不在 idx_live_book 的
	// INCLUDE 里,取了就要回表。没取时是零值
	FirstSeen time.Time
	LastSeen  time.Time
	// Page 是这张单所在那次响应一共回了多少张单,**跨品质数**(见 WithPages)。
	// 0 = 调用方没给,Build 退回按传进来的单自己数,只适合单品质的物品和单测
	Page int
}

// respKey 标识游戏的一次响应:同一 物品 × 城市 × 方向、last_seen 完全相同的那批单。
//
// 装备不筛品质时一页是跨品质的(实测 Brecilien 一次 50 单横跨 q1~q4),
// 所以"这一页满没满"必须跨品质数,在 城市 × 品质 里数永远凑不到 50。
// 物品要分开:两个物品同一时刻各翻一页,不能数成一页 100 张。
type respKey struct {
	Item string
	City string
	Side model.Side
	Seen int64 // UnixMicro:库里是微秒精度
}

func keyOf(o Order) respKey { return respKey{o.ItemID, o.City, o.Side, o.LastSeen.UnixMicro()} }

func countPages(orders []Order) map[respKey]int {
	n := make(map[respKey]int, len(orders)/8+1)
	for _, o := range orders {
		n[keyOf(o)]++
	}
	return n
}

// WithPages 按整次响应数单,返回一份填好 Page 的拷贝,输入不动。
// 传进来的应是一个物品在各城市、**所有品质**、两个方向上的单(查价页的 ItemOrders 就是)。
//
// 批量读簿(store.BookOrders)在 SQL 里按同一个口径数好了才返回,不用再调它:
// 它只回请求的品质,在 Go 里数就漏了别的品质
func WithPages(orders []Order) []Order {
	n := countPages(orders)
	out := make([]Order, len(orders))
	for i, o := range orders {
		o.Page = n[keyOf(o)]
		out[i] = o
	}
	return out
}

// Better 回答"对这一侧来说 a 是不是比 b 更优"。卖单价低者优,买单价高者优。
func Better(side model.Side, a, b int64) bool {
	if side == model.SideRequest {
		return a > b
	}
	return a < b
}

// Level 是阶梯上的一档。
type Level struct {
	Price  int64
	Qty    int64
	Orders int
	// Seen 是这一档里最近被看到的那张单的 last_seen,和 AODP 比新旧用它
	Seen time.Time
	// First 是这一档里最早的 first_seen;调用方没取 first_seen 时是零值
	First time.Time
}

// Side 是一个盘口边整理之后的样子。
type Side struct {
	// Levels 是当前盘口:最近一轮,加上续页之前那几页(PrevPage)。从优到劣
	Levels []Level
	// Stale 是最近一轮没翻那么深、上一轮翻到的更深的旧单(只在 Truncated 时有)。
	// 从优到劣,只展示:不进最优价、近价件数、QtyTotal,也不能拿来吃单
	Stale []Level
	// Orders / QtyTotal 只算 Levels。QtyTotal 含 1 银占位单
	Orders   int
	QtyTotal int64
	// Newest 是所有单(含剔掉的)里最新的 last_seen,也就是这一边的"最近一眼"
	Newest time.Time
	// Worst 是最近一轮里最差的价。Truncated 时只是翻到的那几页的最远
	Worst int64
	// Truncated:最近一轮是满页,多半没翻完。这时"卖单最高""总件数"都只是下界
	Truncated bool
	// Dropped 是判定为已成交 / 已撤单而剔掉的残单
	Dropped    int
	DroppedQty int64
	// StaleOrders / StaleQty 是 Stale 那几档的张数 / 件数合计
	StaleOrders int
	StaleQty    int64
	// PrevPage:最近一轮只是续页(第 2 页往后)时,前面那几页是更早翻到的,
	// 这里是从那几页保留下来、算进当前盘口的单数。0 = 最近一轮从盘口第一页开始
	PrevPage int
}

// Build 是「最近一轮」规则,专门对付上一轮浏览留下的幽灵单。
// orders 是一个盘口边(物品 × 城市 × 品质 × 方向)在窗口内的全部单,Page 由调用方数好。
//
//   - newest = 所有单里最新的 last_seen;last_seen ≥ newest − slack 的算最近一轮,
//     best / worst 是这一轮的最优 / 最差价
//   - 旧单落在 (best, worst] 里 → 剔除。用户翻过的页里本该有它,没出现就是没了
//   - 旧单比 worst 更差(或正好压在满页的 worst 上)→ 这一轮没翻到那么深:
//     没截断就剔除(整本簿都看到了),截断了就保留但标 stale,不进 best / qty_near / support
//   - 旧单不比 best 差 → 两种可能,看更早那几张单自己是不是满页:
//     ① 不是满页:本轮是从第一页重新翻的,这些单要是还在就一定会出现 → 剔除
//     (实测 T6_LEATHER @ Martlock 上一轮有张 4906 的卖单,比最近一轮的最低价 4909
//     还便宜);② 是满页:本轮只是它的**续页**(第 2 页晚到了超过 slack),
//     那一页仍是对盘口前半截最新的一眼 → 递归地照同一套规则整理后保留。
//     唯一分不开的是"两轮之间有人一口气扫掉了整整一页",那种情况会按续页处理
//
// 为什么"更早那几张单是不是满页"能分开这两种:还在的单被重新看到时 last_seen 会前进,
// 离开原来那次响应。从第一页重翻的话,旧响应里只剩被买走的那几张,凑不满一页;
// 续页不会重新看到前一页的单,前一页原样满着。
//
// slack 不小于窗口时整个窗口都是"最近一轮",什么都不剔——规则前提不成立时的逃生口,
// 多开串城的盘口(最近一眼可能是错归的单)也靠它暂停剔除。
//
// 0 件、0 价的单直接忽略(库里也滤了,这里再防一道):0 价的单只可能是解析出错,
// 留着会被当成最优卖价,也不能拿它当"最近一眼"。
func Build(orders []Order, side model.Side, slack time.Duration) Side {
	valid := make([]Order, 0, len(orders))
	missing := false
	for _, o := range orders {
		if o.Amount <= 0 || o.Price <= 0 {
			continue
		}
		missing = missing || o.Page <= 0
		valid = append(valid, o)
	}
	if len(valid) == 0 {
		return Side{}
	}
	if missing {
		n := countPages(valid)
		for i := range valid {
			if valid[i].Page <= 0 {
				valid[i].Page = n[keyOf(valid[i])]
			}
		}
	}
	if slack < 0 {
		slack = 0
	}

	sp := splitRounds(valid, side, slack, false)
	out := Side{
		Levels:    aggregate(sp.cur, side),
		Stale:     aggregate(sp.stale, side),
		Newest:    newestOf(valid),
		Worst:     sp.worst,
		Truncated: sp.truncated,
		Dropped:   sp.dropped, DroppedQty: sp.droppedQty,
		StaleOrders: len(sp.stale),
		PrevPage:    sp.prevPage,
	}
	for _, l := range out.Levels {
		out.Orders += l.Orders
		out.QtyTotal += l.Qty
	}
	for _, o := range sp.stale {
		out.StaleQty += o.Amount
	}
	return out
}

func newestOf(orders []Order) time.Time {
	newest := orders[0].LastSeen
	for _, o := range orders[1:] {
		if o.LastSeen.After(newest) {
			newest = o.LastSeen
		}
	}
	return newest
}

// pageTruncated 判一轮是不是没翻完。两条满足一条就算:
//
//   - 这一轮的单数是页大小的整数倍。同一页里有几张单几秒后被详情页又看了一眼,
//     last_seen 挪走了,按响应数就不满 50,按轮数还是 50
//   - 这一轮最深那一档所在的响应是满页。装备一页跨品质,单个品质的单数凑不齐 50,
//     只能看整页
//
// 第二条在"满页 + 不满的下一页"而这个品质恰好没落在下一页时会误报为截断,
// 代价只是更深的旧单标成 stale(灰掉、不进统计)而不是直接剔掉。
// 反过来,最深那一档要是来自另一次零星的响应(比如详情页只回了一张),就认不出截断。
// 要分清得按响应先后和价格区间把页串成链,等看过协议、确认分页单位再做。
func pageTruncated(latest []Order, worst int64) bool {
	if n := len(latest); n >= PageSize && n%PageSize == 0 {
		return true
	}
	for _, o := range latest {
		if o.Price == worst && o.Page >= PageSize {
			return true
		}
	}
	return false
}

// split 是 splitRounds 的结果:当前盘口、stale、剔除三份。
type split struct {
	cur, stale []Order
	dropped    int
	droppedQty int64
	prevPage   int   // cur 里来自续页之前那几页的单数
	truncated  bool  // 最近一轮(最深那一页)没翻完
	worst      int64 // 最近一轮里最差的价
}

func (sp *split) drop(o Order) {
	sp.dropped++
	sp.droppedQty += o.Amount
}

// splitRounds 见 Build 的说明。bounded:调用方是在整理续页之前的那几页,
// 更深的那一截已经被续页盖住,比这一轮 worst 更差的旧单一律剔除,不会是 stale。
func splitRounds(orders []Order, side model.Side, slack time.Duration, bounded bool) split {
	var sp split
	if len(orders) == 0 {
		return sp
	}
	cut := newestOf(orders).Add(-slack)
	var latest, older []Order
	for _, o := range orders {
		if !o.LastSeen.Before(cut) {
			latest = append(latest, o)
		} else {
			older = append(older, o)
		}
	}
	// newest 取自 orders 本身,latest 至少有一张
	best, worst := latest[0].Price, latest[0].Price
	for _, o := range latest[1:] {
		if Better(side, o.Price, best) {
			best = o.Price
		}
		if Better(side, worst, o.Price) {
			worst = o.Price
		}
	}
	sp.cur, sp.worst = latest, worst
	sp.truncated = pageTruncated(latest, worst)

	var ahead []Order
	for _, o := range older {
		switch {
		case !Better(side, best, o.Price):
			ahead = append(ahead, o) // 不比本轮最优价差
		case Better(side, worst, o.Price) || (o.Price == worst && sp.truncated):
			if sp.truncated && !bounded {
				sp.stale = append(sp.stale, o)
			} else {
				sp.drop(o)
			}
		default:
			sp.drop(o) // 落在本轮翻过的价格区间里却没再出现:已成交或撤单
		}
	}
	if len(ahead) == 0 {
		return sp
	}
	prev := splitRounds(ahead, side, slack, true)
	if !prev.truncated {
		for _, o := range ahead {
			sp.drop(o)
		}
		return sp
	}
	sp.cur = append(sp.cur, prev.cur...)
	sp.prevPage = len(prev.cur)
	sp.dropped += prev.dropped
	sp.droppedQty += prev.droppedQty
	return sp
}

// aggregate 按价位合并,返回从优到劣排好的档。
func aggregate(orders []Order, side model.Side) []Level {
	if len(orders) == 0 {
		return nil
	}
	byPrice := make(map[int64]*Level, len(orders))
	for _, o := range orders {
		l, ok := byPrice[o.Price]
		if !ok {
			l = &Level{Price: o.Price, Seen: o.LastSeen, First: o.FirstSeen}
			byPrice[o.Price] = l
		}
		l.Qty += o.Amount
		l.Orders++
		if o.LastSeen.After(l.Seen) {
			l.Seen = o.LastSeen
		}
		if o.FirstSeen.Before(l.First) {
			l.First = o.FirstSeen
		}
	}
	out := make([]Level, 0, len(byPrice))
	for _, l := range byPrice {
		out = append(out, *l)
	}
	sort.Slice(out, func(i, j int) bool { return Better(side, out[i].Price, out[j].Price) })
	return out
}

// Key 是一个盘口边:物品 × 城市 × 品质 × 方向。
type Key struct {
	ItemID  string
	City    string
	Quality int
	Side    model.Side
}

// Group 按盘口边分组。组内顺序和输入一致。
func Group(orders []Order) map[Key][]Order {
	out := map[Key][]Order{}
	for _, o := range orders {
		k := Key{o.ItemID, o.City, o.Quality, o.Side}
		out[k] = append(out[k], o)
	}
	return out
}
