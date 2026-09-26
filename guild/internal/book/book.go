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
// 按首次看到的时刻分组,卖单 183 组、买单 14 组恰好 50,50 以下是平滑分布,
// 没有别的"页大小"尖峰(详情页的买单一栏也是 50 封顶)。
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
	// Page 是这张单所在那次响应一共回了多少张单,**跨物品、跨品质数**(见 respKey)。
	// 线上由 store 在 SQL 里对整张表数好;0 = 调用方没给,Build / WithPages 退回按传进来的单
	// 自己数,只适合单测
	Page int
	// PageWorst 是那次响应整页最差的价(卖单最高、买单最低),同样跨物品、跨品质。
	// 续页判定靠它:列表按价排序,真正的续页接在前一页整页最差价之后(见 Build)。
	// 0 = 调用方没给,同 Page 一样退回自己算
	PageWorst int64
}

// respKey 标识游戏的一次响应:同一 城市 × 方向、last_seen 完全相同的那批单,**不分物品、不分品质**。
//
// 为什么这样分:客户端每解析一次挂单响应取一次 time.Now()(protocol.handleOrders),
// 入库时新单和只刷新 last_seen 的单用的都是这个时刻(ingest.Normalize 之后整批平移),
// 所以同一个时间戳就是同一次响应。一次响应本身会跨物品、跨品质:
//   - 装备不筛品质时一页横跨几个品质(实测 Brecilien T5_SHOES_LEATHER_HELL@2 一次 50 单 q1~q4)
//   - 按分类 / 搜索翻列表页时一页横跨几个物品。实测 Martlock 13:56:53 起 21 页、每页 50 单、
//     每页 2~12 个物品(T1_HIDE、T4_ROCK、T2_FIBER…),0.5 秒一页,价格首尾相接:
//     15-30、31-35、35-45 … 121-123。T5_MEAL_SOUP @ Brecilien 那一页 50 单里 33 张是它、
//     17 张是 T5_MEAL_SOUP_FISH
//
// 以前按物品分,跨物品的满页被数成 33、17,判成"翻完了",下一页那 10 张更深的单就被当残单剔掉。
// 两次响应恰好落在同一个时间戳(同一个时钟刻度里解析了两次)会被数成一页,
// 代价只是把"没翻完"多认几回,更深的旧单标 stale 而不是剔除。
type respKey struct {
	City string
	Side model.Side
	Seen int64 // UnixMicro:库里是微秒精度
}

func keyOf(o Order) respKey { return respKey{o.City, o.Side, o.LastSeen.UnixMicro()} }

// pageStat 是一次响应的张数和整页最差价。
type pageStat struct {
	n     int
	worst int64
}

func pageStats(orders []Order) map[respKey]pageStat {
	st := make(map[respKey]pageStat, len(orders)/8+1)
	for _, o := range orders {
		k := keyOf(o)
		s := st[k]
		if s.n == 0 || Better(o.Side, s.worst, o.Price) {
			s.worst = o.Price
		}
		s.n++
		st[k] = s
	}
	return st
}

// fillPages 给没带 Page / PageWorst 的单按 orders 自己数,原地改。带了的不动:
// 线上的数是 store 对整张表数的,比这里只看得到的几个物品准
func fillPages(orders []Order) {
	var st map[respKey]pageStat
	for i := range orders {
		o := &orders[i]
		if o.Page > 0 && o.PageWorst > 0 {
			continue
		}
		if st == nil {
			st = pageStats(orders)
		}
		s := st[keyOf(*o)]
		if o.Page <= 0 {
			o.Page = s.n
		}
		if o.PageWorst <= 0 {
			o.PageWorst = s.worst
		}
	}
}

// WithPages 返回一份补齐了 Page / PageWorst 的拷贝,输入不动。已经带了的(store 在 SQL 里
// 对整张表数好的)原样保留;没带的按传进来的单数,所以传进来的应是一次读出的全部单
// (一个物品各城市、所有品质、两个方向)。
func WithPages(orders []Order) []Order {
	out := append([]Order(nil), orders...)
	fillPages(out)
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
// orders 是一个盘口边(物品 × 城市 × 品质 × 方向)在窗口内的全部单,Page / PageWorst
// 由调用方数好(store 对整张表跨物品数)。
//
// 规则的前提是游戏的挂单列表**按价排序、一页至多 50 张**(卖单从低到高,买单从高到低),
// 一次响应里这个盘口边的单就是它在那一页价格范围里的全部单。成交和撤单都不会有消息,
// 库里的单只在被看到时刷新 last_seen,所以只能拿"最近一轮看到了什么"反推其余的还在不在:
//
//   - newest = 所有单里最新的 last_seen;last_seen ≥ newest − slack 的算最近一轮,
//     best / worst 是这一轮的最优 / 最差价
//   - 旧单落在 (best, worst] 里 → 剔除。用户翻过的页里本该有它,没出现就是没了
//   - 旧单比 worst 更差(或正好压在满页的 worst 上)→ 这一轮没翻到那么深:
//     没截断就剔除(列表翻到了底,它还在的话一定在这一页里),截断了就保留但标 stale,
//     不进 best / qty_near / support
//   - 旧单不比 best 差 → 两种可能:
//     ① 本轮是从第一页重新翻的,这些单要是还在就一定会出现 → 剔除
//     (实测 T6_LEATHER @ Martlock 上一轮有张 4906 的卖单,比最近一轮的最低价 4909 还便宜);
//     ② 本轮只是**续页**(第 2 页晚到了超过 slack),更早那一页仍是对盘口前半截最新的一眼
//     → 递归地照同一套规则整理后保留。
//     判成续页要同时满足:更早那一页是满页(没翻完才会有下一页),而且本轮最优价不比那一页
//     **整页**(跨物品、跨品质)的最差价更优 —— 按价排序的列表,下一页只能接在上一页末尾之后。
//     实测续页都是这样首尾相接的(见 respKey);不接着的是另一次浏览:比如先不筛品质翻了一页
//     (q1 5 张 + q2 45 张,满页),过后又筛 q1 从头翻,旧那页满着,可 q1 新的卖一比 q2 那几十张
//     便宜得多,不可能是它的下一页 → 旧的 q1 单照 ① 剔除。
//     仍然分不开的是"两轮之间有人一口气扫掉了整整一页、新挂的单又都比那页贵",那种会按续页处理
//   - 例外:最近一轮是满页而且整页一个价(best == worst),同价的旧单可能只是排到了下一页 → stale
//
// 为什么"更早那几张单是不是满页"能区分:还在的单被重新看到时 last_seen 会前进,
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
	for _, o := range orders {
		if o.Amount <= 0 || o.Price <= 0 {
			continue
		}
		valid = append(valid, o)
	}
	if len(valid) == 0 {
		return Side{}
	}
	fillPages(valid)
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
//   - 这一轮的单数是页大小的整数倍。同一页里有几张单几秒后又被另一次响应看到
//     (比如从列表点进详情页),last_seen 挪走了,按响应数就不满 50,按轮数还是 50
//   - 这一轮最深那一档所在的响应是满页。一页跨物品、跨品质,单个盘口边的单数凑不齐 50,
//     只能看整页
//
// 第二条在"满页 + 不满的下一页"而这个盘口边恰好没落在下一页时会误报为截断,
// 代价只是更深的旧单标成 stale(灰掉、不进统计)而不是直接剔掉。
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

// pageEdge 是最近一轮最深那一页的整页最差价(跨物品、跨品质)。几次响应里取最差的:
// 按价排序翻页,越往后的页越差,最差的那个就是最后一页的末尾。
// PageWorst 不会比单子自己的价更优,万一是(调用方给错了)按单价算
func pageEdge(latest []Order, side model.Side) int64 {
	edge := latest[0].Price
	for _, o := range latest {
		w := o.PageWorst
		if w <= 0 || Better(side, w, o.Price) {
			w = o.Price
		}
		if Better(side, edge, w) {
			edge = w
		}
	}
	return edge
}

// split 是 splitRounds 的结果:当前盘口、stale、剔除三份。
type split struct {
	cur, stale []Order
	dropped    int
	droppedQty int64
	prevPage   int   // cur 里来自续页之前那几页的单数
	truncated  bool  // 最近一轮(最深那一页)没翻完
	worst      int64 // 最近一轮里这个盘口边最差的价
	edge       int64 // 最近一轮最深那一页整页最差的价(跨物品、跨品质),判续页用
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
	sp.edge = pageEdge(latest, side)
	// 截断了、又不是在整理续页之前那几页:更深的旧单可能只是没翻到,标 stale
	keepDeeper := sp.truncated && !bounded

	var ahead []Order
	for _, o := range older {
		switch {
		case !Better(side, best, o.Price):
			ahead = append(ahead, o) // 不比本轮最优价差
		case Better(side, worst, o.Price) || (o.Price == worst && sp.truncated):
			if keepDeeper {
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
	// 续页:更早那一页没翻完,而且本轮接在它整页的末尾之后(见 Build)
	prev := splitRounds(ahead, side, slack, true)
	if prev.truncated && !Better(side, best, prev.edge) {
		sp.cur = append(sp.cur, prev.cur...)
		sp.prevPage = len(prev.cur)
		sp.dropped += prev.dropped
		sp.droppedQty += prev.droppedQty
		return sp
	}
	for _, o := range ahead {
		// ahead 里的单不比 best 差,等于 worst 就只能是 best == worst:本轮满页、整页一个价
		// (实测 T4_RUNE @ Martlock 50 张全是 9 银),同价的旧单可能只是排到了下一页。
		// 按"比 best 还优"一律剔掉的话,这一档底下几十张真单每次都被记成残单
		if o.Price == worst && keepDeeper {
			sp.stale = append(sp.stale, o)
		} else {
			sp.drop(o)
		}
	}
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
