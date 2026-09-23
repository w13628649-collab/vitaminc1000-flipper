package flip

import (
	"context"
	"math"
	"sort"
	"time"

	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/econ"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

// 查价页的挂单簿:一个 城市 × 品质 的两侧完整阶梯。
//
// 和 /api/book 的区别:那个接口按 -fresh(30 分钟)读、只给一侧、不剔残单,
// 是给 WS 实时页用的。查价页要的是"最后一次看到的盘口 + 标龄",
// 窗口按 freshness.max_hours(默认 6h),残单靠"最近一轮"规则挡,不靠缩窗口。

const (
	// lookupNearPct 是近价窗口:卖一往上 / 买一往下 5% 以内。判"有没有人在收"就看这个数
	lookupNearPct = 0.05
	// lookupSlack 多宽算"同一眼看到的"。实测 293 个盘口里 283 个所有单都在 1 分钟内,
	// 分页翻的一轮也只有几秒。前提是 last_seen 只用一种时钟(见 docs/flipper.md)
	lookupSlack = 5 * time.Minute
	// lookupPageSize 是游戏一次响应最多回多少张单。从数据形态推出来的:
	// 103 个盘口恰好 50 单,T1_WOOD @ Martlock 分 7 次首尾相接到达。没看协议源码确认
	lookupPageSize = 50
	// lookupMaxLevels 是一侧最多回多少档。一页 50 单,翻几页也就一两百档
	lookupMaxLevels = 200
	// lookupMinBidDepth 是"买方太薄"的阈值,沿用 Python 版的 min_bid_depth。
	// conf 里还没有这个字段(conf 在另一条线上改),先写成常量,界面也用它
	lookupMinBidDepth = 20
	// lookupMaxWant 挡一下离谱的吃单量参数
	lookupMaxWant = 100_000_000
)

// LadderLevel 是阶梯上的一档。
type LadderLevel struct {
	Price  int64 `json:"price"`
	Qty    int64 `json:"qty"`
	Orders int   `json:"orders"`
	CumQty int64 `json:"cum_qty"`
	// Off 是相对本侧最优价的落差 |p/best − 1|,小数
	Off float64 `json:"off"`
	// AgeHours 是这一档最后一次被看到距今多久,StandingHours 是第一次看到距今多久。
	// 后者只能叫"我们看到它挂了多久":expires_at 在库里全是空的,拿不到真正的挂单时间
	AgeHours      float64 `json:"age_hours"`
	StandingHours float64 `json:"standing_hours"`
	// Stale 是上一轮翻到、这一轮没翻到那么深的单。只展示,不进最优价和近价件数
	Stale bool `json:"stale"`
}

// BookSide 是一侧挂单簿。
type BookSide struct {
	// Levels 从优到劣;stale 的档排在最后
	Levels []LadderLevel `json:"levels"`
	// Orders / LevelCount / QtyTotal 只算最近一轮。QtyTotal 含 1 银占位单
	Orders     int   `json:"orders"`
	LevelCount int   `json:"level_count"`
	QtyTotal   int64 `json:"qty_total"`
	// Truncated:最近一轮的单数是一页的整数倍,说明很可能没翻完,
	// 这时"卖单最高""总件数"都只是下界
	Truncated  bool       `json:"truncated"`
	NewestSeen *time.Time `json:"newest_seen"`
	AgeHours   *float64   `json:"age_hours"`
	// Dropped 是判定为已成交/已撤单而剔除的残单;Stale 见 LadderLevel.Stale
	DroppedOrders int   `json:"dropped_orders"`
	DroppedQty    int64 `json:"dropped_qty"`
	StaleOrders   int   `json:"stale_orders"`
	StaleQty      int64 `json:"stale_qty"`
	// PrevPageOrders:最近一轮只是续页(第 2 页往后)时,前面那几页是更早翻到的,
	// 这里是从那几页保留下来、算进当前盘口的单数。0 = 最近一轮从盘口第一页开始
	PrevPageOrders int `json:"prev_page_orders"`

	Support depth.Support `json:"support"`
	// Fill 只在请求带了 qty 时出现:吃掉这么多件的真实均价
	Fill *depth.Fill `json:"fill"`

	far          int64     // 最近一轮里最差的价
	bestSeen     time.Time // 最优价那一档的 last_seen,和 AODP 比新旧用它
	ordersAtBest int
}

// has 表示这一侧最近一轮里至少有一张单。
func (b BookSide) has() bool { return b.Orders > 0 }

// better 回答"对这一侧来说 a 是不是比 b 更优"。卖单价低者优,买单价高者优。
func better(side model.Side, a, b int64) bool {
	if side == model.SideRequest {
		return a > b
	}
	return a < b
}

type levelAcc struct {
	price    int64
	qty      int64
	orders   int
	maxSeen  time.Time
	minFirst time.Time
}

// aggregate 按价位合并,返回从优到劣排好的档。
func aggregate(orders []store.LiveOrder, side model.Side) []levelAcc {
	byPrice := map[int64]*levelAcc{}
	for _, o := range orders {
		a, ok := byPrice[o.Price]
		if !ok {
			a = &levelAcc{price: o.Price, maxSeen: o.LastSeen, minFirst: o.FirstSeen}
			byPrice[o.Price] = a
		}
		a.qty += o.Amount
		a.orders++
		if o.LastSeen.After(a.maxSeen) {
			a.maxSeen = o.LastSeen
		}
		if o.FirstSeen.Before(a.minFirst) {
			a.minFirst = o.FirstSeen
		}
	}
	out := make([]levelAcc, 0, len(byPrice))
	for _, a := range byPrice {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return better(side, out[i].price, out[j].price) })
	return out
}

// respKey 标识游戏的一次响应:同一 城市 × 方向、last_seen 完全相同的那批单。
//
// 实测一次响应最多 50 单(24 小时里 149 组恰好 50 单,没有一组超过),
// 而且**装备不筛品质时一页是跨品质的**(Brecilien 一次 50 单横跨 q1~q4)。
// 所以"这一页满没满"必须跨品质数,在 城市 × 品质 里数永远凑不到 50。
type respKey struct {
	City string
	Side model.Side
	Seen int64 // UnixMicro:库里是微秒精度
}

type respSizes map[respKey]int

// countResponses 按响应数单。传进来的应是这个物品在各城市、**所有品质**上的单。
func countResponses(orders []store.LiveOrder) respSizes {
	out := respSizes{}
	for _, o := range orders {
		out[respKey{o.City, o.Side, o.LastSeen.UnixMicro()}]++
	}
	return out
}

// fullPage:这张单所在的那次响应是满页,后面多半还有下一页。
func (r respSizes) fullPage(o store.LiveOrder) bool {
	return r[respKey{o.City, o.Side, o.LastSeen.UnixMicro()}] >= lookupPageSize
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
func pageTruncated(latest []store.LiveOrder, worst int64, resp respSizes) bool {
	if n := len(latest); n >= lookupPageSize && n%lookupPageSize == 0 {
		return true
	}
	for _, o := range latest {
		if o.Price == worst && resp.fullPage(o) {
			return true
		}
	}
	return false
}

// sideSplit 是 splitRounds 的结果:当前盘口、stale、剔除三份。
type sideSplit struct {
	cur, stale []store.LiveOrder
	dropped    int
	droppedQty int64
	prevPage   int   // cur 里来自续页之前那几页的单数
	truncated  bool  // 最近一轮(最深那一页)没翻完
	worst      int64 // 最近一轮里最差的价
}

func (sp *sideSplit) drop(o store.LiveOrder) {
	sp.dropped++
	sp.droppedQty += o.Amount
}

// splitRounds 是「最近一轮」规则,专门对付上一轮浏览留下的幽灵单:
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
// bounded:调用方是在整理续页之前的那几页,更深的那一截已经被续页盖住,
// 比这一轮 worst 更差的旧单一律剔除,不会是 stale。
func splitRounds(orders []store.LiveOrder, side model.Side, slack time.Duration,
	resp respSizes, bounded bool) sideSplit {

	var sp sideSplit
	if len(orders) == 0 {
		return sp
	}
	newest := orders[0].LastSeen
	for _, o := range orders[1:] {
		if o.LastSeen.After(newest) {
			newest = o.LastSeen
		}
	}
	cut := newest.Add(-slack)
	var latest, older []store.LiveOrder
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
		if better(side, o.Price, best) {
			best = o.Price
		}
		if better(side, worst, o.Price) {
			worst = o.Price
		}
	}
	sp.cur, sp.worst = latest, worst
	sp.truncated = pageTruncated(latest, worst, resp)

	var ahead []store.LiveOrder
	for _, o := range older {
		switch {
		case !better(side, best, o.Price):
			ahead = append(ahead, o) // 不比本轮最优价差
		case better(side, worst, o.Price) || (o.Price == worst && sp.truncated):
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
	prev := splitRounds(ahead, side, slack, resp, true)
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

// buildSide 把一侧的挂单(调用方已按 城市 × 品质 × 方向 过滤)整理成阶梯,
// 取舍规则见 splitRounds。resp 是跨品质的响应计数(countResponses);
// nil 时就按 orders 自己数,只适合单品质的物品和单测。
func buildSide(orders []store.LiveOrder, side model.Side, now time.Time,
	slack time.Duration, nearPct float64, resp respSizes) BookSide {

	out := BookSide{Levels: []LadderLevel{}}
	if len(orders) == 0 {
		return out
	}
	if resp == nil {
		resp = countResponses(orders)
	}
	sp := splitRounds(orders, side, slack, resp, false)
	out.Truncated = sp.truncated
	out.DroppedOrders, out.DroppedQty = sp.dropped, sp.droppedQty
	out.PrevPageOrders = sp.prevPage
	out.StaleOrders = len(sp.stale)
	for _, o := range sp.stale {
		out.StaleQty += o.Amount
	}
	newest := orders[0].LastSeen
	for _, o := range orders[1:] {
		if o.LastSeen.After(newest) {
			newest = o.LastSeen
		}
	}

	fresh := aggregate(sp.cur, side)
	old := aggregate(sp.stale, side)

	best := fresh[0].price
	hours := func(t time.Time) float64 { return math.Max(0, now.Sub(t).Hours()) }
	var cum int64
	emit := func(a levelAcc, isStale bool) {
		cum += a.qty
		off := 0.0
		if best > 0 {
			off = math.Abs(float64(a.price)/float64(best) - 1)
		}
		if len(out.Levels) < lookupMaxLevels {
			out.Levels = append(out.Levels, LadderLevel{
				Price: a.price, Qty: a.qty, Orders: a.orders, CumQty: cum, Off: off,
				AgeHours: hours(a.maxSeen), StandingHours: hours(a.minFirst), Stale: isStale,
			})
		}
	}
	levels := make([]depth.Level, 0, len(fresh))
	for _, a := range fresh {
		emit(a, false)
		levels = append(levels, depth.Level{Price: a.price, Qty: a.qty})
		out.QtyTotal += a.qty
		out.Orders += a.orders
	}
	for _, a := range old {
		emit(a, true)
	}
	out.LevelCount = len(fresh)
	out.Support = depth.Analyze(levels, nearPct)
	out.far = sp.worst
	out.bestSeen = fresh[0].maxSeen
	out.ordersAtBest = fresh[0].orders
	nt := newest
	age := hours(newest)
	out.NewestSeen, out.AgeHours = &nt, &age
	return out
}

// walk 按最近一轮的档吃 want 件。stale 的档不算:那些单多半已经没了。
func (b *BookSide) walk(want int64) {
	if want <= 0 {
		return
	}
	levels := make([]depth.Level, 0, len(b.Levels))
	for _, l := range b.Levels {
		if !l.Stale {
			levels = append(levels, depth.Level{Price: l.Price, Qty: l.Qty})
		}
	}
	f := depth.Walk(levels, want)
	b.Fill = &f
}

// splitOrders 按 城市 × 品质 × 方向 分组。
type bookKey struct {
	City    string
	Quality int
	Side    model.Side
}

func splitOrders(orders []store.LiveOrder) map[bookKey][]store.LiveOrder {
	out := map[bookKey][]store.LiveOrder{}
	for _, o := range orders {
		k := bookKey{o.City, o.Quality, o.Side}
		out[k] = append(out[k], o)
	}
	return out
}

// BookSummary 是两侧最优价算出来的那几个数,全部只用抓包。
// 查价格子上显示的是逐边融合后的价,和这里可能不同 —— 界面两边都标了来源。
type BookSummary struct {
	BestSell      int64    `json:"best_sell"`
	BestBuy       int64    `json:"best_buy"`
	Spread        *float64 `json:"spread"` // 卖一/买一 − 1,未扣税费
	Margin        *float64 `json:"margin"` // 挂买→挂卖的税后毛利率
	MyBid         int64    `json:"my_bid"`
	MyAsk         int64    `json:"my_ask"`
	ProfitPerUnit *float64 `json:"profit_per_unit"`
	Breakeven     float64  `json:"breakeven"`
}

var makerMaker = econ.Mode{Buy: econ.Maker, Sell: econ.Maker}

func summarizeBook(sell, buy BookSide, cfg conf.Economics) BookSummary {
	s := BookSummary{Breakeven: makerMaker.Breakeven(cfg)}
	if sell.has() {
		s.BestSell = sell.Support.Best
	}
	if buy.has() {
		s.BestBuy = buy.Support.Best
	}
	if s.BestSell > 0 && s.BestBuy > 0 {
		b := econ.Book{SellMin: s.BestSell, BuyMax: s.BestBuy}
		u := econ.Quote(b, b, makerMaker, cfg)
		spread := float64(s.BestSell)/float64(s.BestBuy) - 1
		s.Spread, s.Margin, s.ProfitPerUnit = &spread, &u.Margin, &u.ProfitPerUnit
		s.MyBid, s.MyAsk = u.MyBid, u.MyAsk
	}
	return s
}

// LookupBook 是 GET /api/lookup/book 的响应。
//
// **不带成交历史**:面板复用查价表里同一格的 history 对象,
// 卡片和面板的成交数字因此由构造保证一致,不会是两份口径。
type LookupBook struct {
	ItemID               string      `json:"item_id"`
	City                 string      `json:"city"`
	Quality              int         `json:"quality"`
	FetchedAt            time.Time   `json:"fetched_at"`
	WindowHours          float64     `json:"window_hours"`
	SnapshotSlackMinutes float64     `json:"snapshot_slack_minutes"`
	NearPct              float64     `json:"near_pct"`
	PageSize             int         `json:"page_size"`
	MinBidDepth          int64       `json:"min_bid_depth"`
	Sell                 BookSide    `json:"sell"`
	Buy                  BookSide    `json:"buy"`
	Summary              BookSummary `json:"summary"`
}

// lookupWindow 是查价页读抓包的窗口,跟 freshness.max_hours 走。
//
// 不用 -fresh 的 30 分钟:那是 WS 实时推送的语义。实测一次翻市场 26 分钟,
// 30 分钟窗口一过整本簿就从界面上消失,又回到"刚翻过却显示无数据"。
// 窗口放长不会让价格变错:逐边融合是"谁新用谁",旧抓包盖不过新 AODP。
func (s *Service) lookupWindow() time.Duration {
	h := s.Cfg.Freshness.MaxHours
	if h <= 0 {
		h = 6
	}
	return time.Duration(h * float64(time.Hour))
}

func buildBook(itemID, city string, quality int, orders []store.LiveOrder,
	now time.Time, window time.Duration, want int64, cfg conf.Economics) *LookupBook {

	var sellOrders, buyOrders []store.LiveOrder
	for _, o := range orders {
		if o.City != city || o.Quality != quality {
			continue
		}
		if o.Side == model.SideRequest {
			buyOrders = append(buyOrders, o)
		} else {
			sellOrders = append(sellOrders, o)
		}
	}
	// 页满没满要跨品质数:orders 是这个物品所有品质的单
	resp := countResponses(orders)
	sell := buildSide(sellOrders, model.SideOffer, now, lookupSlack, lookupNearPct, resp)
	buy := buildSide(buyOrders, model.SideRequest, now, lookupSlack, lookupNearPct, resp)
	if want > lookupMaxWant {
		want = lookupMaxWant
	}
	sell.walk(want) // 买入 = 吃卖单,从低往高
	buy.walk(want)  // 卖出 = 吃买单,从高往低
	return &LookupBook{
		ItemID: itemID, City: city, Quality: quality, FetchedAt: now,
		WindowHours:          window.Hours(),
		SnapshotSlackMinutes: lookupSlack.Minutes(),
		NearPct:              lookupNearPct,
		PageSize:             lookupPageSize,
		MinBidDepth:          lookupMinBidDepth,
		Sell:                 sell,
		Buy:                  buy,
		Summary:              summarizeBook(sell, buy, cfg),
	}
}

// LookupBook 读一个格子两侧的完整阶梯。没有抓包也返回 200:两侧为空,
// 界面据此提示"进游戏点进物品详情页",右栏照样显示成交走势。
func (s *Service) LookupBook(ctx context.Context, itemID, city string, quality int,
	want int64) (*LookupBook, error) {

	now := time.Now().UTC()
	window := s.lookupWindow()
	orders, err := s.Store.ItemOrders(ctx, itemID, now.Add(-window))
	if err != nil {
		return nil, err
	}
	return buildBook(itemID, city, quality, orders, now, window, want, s.Cfg.Economics), nil
}
