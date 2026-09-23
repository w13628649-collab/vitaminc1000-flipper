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

// buildSide 把一侧的挂单(调用方已按 城市 × 品质 × 方向 过滤)整理成阶梯。
//
// 「最近一轮」规则,专门对付上一轮浏览留下的幽灵单:
//
//   - newest = 所有单里最新的 last_seen;last_seen ≥ newest − slack 的算最近一轮
//   - worst = 最近一轮里最差的价;单数是页大小的整数倍就算 truncated(可能没翻完)
//   - 不在最近一轮里的单:价格优于或等于 worst → 丢弃。用户翻过的页里本该有它,
//     没出现就是没了(实测 T6_LEATHER @ Martlock 上一轮有张 4906 的卖单,
//     比最近一轮的最低价 4909 还便宜,它要是还在就一定会出现)
//   - 比 worst 更差、且没截断 → 丢弃:整本簿都看到了,它不在里面
//   - 比 worst 更差、且截断了 → 保留但标 stale,不进 best / qty_near / support
func buildSide(orders []store.LiveOrder, side model.Side, now time.Time,
	slack time.Duration, nearPct float64) BookSide {

	out := BookSide{Levels: []LadderLevel{}}
	if len(orders) == 0 {
		return out
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
	worst := latest[0].Price
	for _, o := range latest[1:] {
		if better(side, worst, o.Price) {
			worst = o.Price
		}
	}
	out.Truncated = len(latest) >= lookupPageSize && len(latest)%lookupPageSize == 0

	var stale []store.LiveOrder
	for _, o := range older {
		if !better(side, worst, o.Price) || !out.Truncated {
			// o 不比 worst 差(在翻过的范围里却没再出现),或者整本簿都看过了
			out.DroppedOrders++
			out.DroppedQty += o.Amount
			continue
		}
		stale = append(stale, o)
		out.StaleOrders++
		out.StaleQty += o.Amount
	}

	fresh := aggregate(latest, side)
	old := aggregate(stale, side)

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
	out.far = worst
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
	sell := buildSide(sellOrders, model.SideOffer, now, lookupSlack, lookupNearPct)
	buy := buildSide(buyOrders, model.SideRequest, now, lookupSlack, lookupNearPct)
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
