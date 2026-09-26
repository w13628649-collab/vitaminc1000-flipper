package flip

import (
	"context"
	"math"
	"time"

	"albion-guild/internal/book"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/econ"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

// 查价页的挂单簿:一个 城市 × 品质 的两侧完整阶梯。
//
// 和 /api/book 的区别:那个接口按 -fresh(30 分钟)读、只给一侧、不剔残单,
// 是给 WS 实时页用的。查价页要的是"最后一次看到的盘口 + 标龄",窗口是扫描的
// 抓包窗口(conf.Config.CaptureWindow),残单靠"最近一轮"规则(book.Build)挡,不靠缩窗口。
//
// 残单规则、slack、串城时暂停剔除、近价比例、"买方太薄"的阈值都和扫描是同一份
// (book.Build + rounds + conf.Filters):以前这里有自己的一套常量(slack 5 分钟、
// 近价 5%、买方太薄 20 件),机会页的卡片和查价页的面板因此对不上。

const (
	// lookupMaxLevels 是一侧最多回多少档,只管 JSON 大小,不是口径:
	// 近价件数、总件数、档数都按全部档算。一页 50 单,翻几页也就一两百档
	lookupMaxLevels = 200
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
	// Orders / LevelCount / QtyTotal 只算当前盘口(不含 stale)。QtyTotal 含 1 银占位单
	Orders     int   `json:"orders"`
	LevelCount int   `json:"level_count"`
	QtyTotal   int64 `json:"qty_total"`
	// Truncated:最近一轮是满页,说明很可能没翻完,
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
	bestSeen     time.Time // 最优价那一档的 last_seen
	ordersAtBest int
	// raw 是 book.Build 的原样结果,逐边融合时转成 scan.CapturedSide 交给 scan.MergeSide,
	// 和扫描喂进去的是同一个东西
	raw book.Side
}

// has 表示这一侧当前盘口里至少有一张单。
func (b BookSide) has() bool { return b.Orders > 0 }

// buildSide 把一侧的挂单(调用方已按 城市 × 品质 × 方向 过滤)整理成阶梯,
// 取舍规则见 book.Build。orders 的 Page 要由调用方跨品质数好(book.WithPages);
// 没数的话按 orders 自己数,只适合单品质的物品和单测。
func buildSide(orders []store.LiveOrder, side model.Side, now time.Time,
	slack time.Duration, nearPct float64) BookSide {

	out := BookSide{Levels: []LadderLevel{}}
	s := book.Build(orders, side, slack)
	if len(s.Levels) == 0 {
		return out
	}
	out.raw = s
	out.Truncated = s.Truncated
	out.DroppedOrders, out.DroppedQty = s.Dropped, s.DroppedQty
	out.PrevPageOrders = s.PrevPage
	out.StaleOrders, out.StaleQty = s.StaleOrders, s.StaleQty

	best := s.Levels[0].Price
	hours := func(t time.Time) float64 {
		if t.IsZero() {
			return 0 // 没取 first_seen 的来源;查价页的 ItemOrders 一定带
		}
		return math.Max(0, now.Sub(t).Hours())
	}
	var cum int64
	emit := func(l book.Level, isStale bool) {
		cum += l.Qty
		off := 0.0
		if best > 0 {
			off = math.Abs(float64(l.Price)/float64(best) - 1)
		}
		if len(out.Levels) < lookupMaxLevels {
			out.Levels = append(out.Levels, LadderLevel{
				Price: l.Price, Qty: l.Qty, Orders: l.Orders, CumQty: cum, Off: off,
				AgeHours: hours(l.Seen), StandingHours: hours(l.First), Stale: isStale,
			})
		}
	}
	levels := make([]depth.Level, 0, len(s.Levels))
	for _, l := range s.Levels {
		emit(l, false)
		levels = append(levels, depth.Level{Price: l.Price, Qty: l.Qty})
	}
	for _, l := range s.Stale {
		emit(l, true)
	}
	out.Orders, out.QtyTotal = s.Orders, s.QtyTotal
	out.LevelCount = len(s.Levels)
	out.Support = depth.Analyze(levels, nearPct)
	out.far = s.Worst
	out.bestSeen = s.Levels[0].Seen
	out.ordersAtBest = s.Levels[0].Orders
	nt := s.Newest
	age := hours(nt)
	out.NewestSeen, out.AgeHours = &nt, &age
	return out
}

// walk 按当前盘口的档吃 want 件。stale 的档不算:那些单多半已经没了。
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

// splitOrders 按 城市 × 品质 × 方向 分组。查价页的单都是同一个物品,不按物品分。
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

func (k bookKey) quoteKey(itemID string) model.QuoteKey {
	return model.QuoteKey{ItemID: itemID, LocationID: k.City, Quality: int16(k.Quality), Side: k.Side}
}

// lookupRounds 是查价页的 slack 口径:和扫描、报价(storeBooks)同一份配置,
// 串城的盘口边同样暂停剔除。conflicted 为 nil 就是没有串城。
func lookupRounds(cfg conf.Config, conflicted map[model.QuoteKey]time.Time) rounds {
	return rounds{slack: cfg.SnapshotSlack(), window: cfg.CaptureWindow(), conflicted: conflicted}
}

// itemConflicts 问 ingest 这个物品在 orders 里出现过的盘口边中,哪些 since 之后串过城。
func (s *Service) itemConflicts(itemID string, orders []store.LiveOrder, since time.Time) map[model.QuoteKey]time.Time {
	if s.Conflicts == nil {
		return nil
	}
	seen := map[model.QuoteKey]struct{}{}
	var keys []model.QuoteKey
	for _, o := range orders {
		k := bookKey{o.City, o.Quality, o.Side}.quoteKey(itemID)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	return conflictsOf(s.Conflicts, keys, since)
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

// buildBook 是 LookupBook 的全部业务逻辑。orders 是这个物品所有城市、所有品质的单
// (页满没满要跨品质数),conflicted 是其中最近串过城的盘口边。
func buildBook(itemID, city string, quality int, orders []store.LiveOrder,
	now time.Time, want int64, cfg conf.Config, conflicted map[model.QuoteKey]time.Time) *LookupBook {

	orders = book.WithPages(orders)
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
	r := lookupRounds(cfg, conflicted)
	sellSlack, _ := r.slackOf(bookKey{city, quality, model.SideOffer}.quoteKey(itemID))
	buySlack, _ := r.slackOf(bookKey{city, quality, model.SideRequest}.quoteKey(itemID))
	sell := buildSide(sellOrders, model.SideOffer, now, sellSlack, cfg.Filters.NearPct)
	buy := buildSide(buyOrders, model.SideRequest, now, buySlack, cfg.Filters.NearPct)
	if want > lookupMaxWant {
		want = lookupMaxWant
	}
	sell.walk(want) // 买入 = 吃卖单,从低往高
	buy.walk(want)  // 卖出 = 吃买单,从高往低
	return &LookupBook{
		ItemID: itemID, City: city, Quality: quality, FetchedAt: now,
		WindowHours:          cfg.CaptureWindow().Hours(),
		SnapshotSlackMinutes: cfg.SnapshotSlack().Minutes(),
		NearPct:              cfg.Filters.NearPct,
		PageSize:             book.PageSize,
		MinBidDepth:          cfg.Filters.MinBidDepth,
		Sell:                 sell,
		Buy:                  buy,
		Summary:              summarizeBook(sell, buy, cfg.Economics),
	}
}

// LookupBook 读一个格子两侧的完整阶梯。没有抓包也返回 200:两侧为空,
// 界面据此提示"进游戏点进物品详情页",右栏照样显示成交走势。
//
// 窗口和扫描读抓包是同一个(conf.Config.CaptureWindow)。不用 -fresh 的 30 分钟:
// 那是 WS 实时推送的语义。实测一次翻市场 26 分钟,30 分钟窗口一过整本簿就从界面上
// 消失,又回到"刚翻过却显示无数据"。窗口放长不会让格子上的价变错:逐边融合走
// scan.PickCapture,抓包比 AODP 旧超过 prefer_slack 又不同价时 AODP 胜
func (s *Service) LookupBook(ctx context.Context, itemID, city string, quality int,
	want int64) (*LookupBook, error) {

	now := time.Now().UTC()
	since := now.Add(-s.Cfg.CaptureWindow())
	orders, err := s.Store.ItemOrders(ctx, itemID, since)
	if err != nil {
		return nil, err
	}
	return buildBook(itemID, city, quality, orders, now, want, s.Cfg,
		s.itemConflicts(itemID, orders, since)), nil
}
