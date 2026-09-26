package flip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/book"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
	"albion-guild/internal/histagg"
	"albion-guild/internal/model"
	"albion-guild/internal/scan"
	"albion-guild/internal/screen"
	"albion-guild/internal/store"
)

// 查价页的价格矩阵:所有城市 × 所有品质,每格两侧逐边融合抓包和 AODP,
// 再带上这一格的成交统计和日线。
//
// 旧的 /api/lookup 不动:它按整格融合、行级 source,旧客户端还在用。

// lookupAODPWait 是 handler 最多等 AODP 多久。到点就先回抓包那部分,
// 请求留在后台跑完进缓存(见 lookup_cache.go)
const lookupAODPWait = 12 * time.Second

// BlackMarket 是 AODP 里黑市的城市名。它只收不卖,卖侧天然是空的。
const BlackMarket = "Black Market"

// Brecilien 是 AODP 和 world.City 都认的城市名。旧版查价页没有它,
// 但成员在那里翻市场时抓包是有数据的,不放进来就又是"刚翻过却显示无数据"
const Brecilien = "Brecilien"

// lookupCities 是查价矩阵的行:配置里的城市,加 Brecilien,黑市固定放最后一行。
func lookupCities(cfgCities []string) []string {
	out := make([]string, 0, len(cfgCities)+2)
	seen := map[string]bool{}
	add := func(c string) {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, c := range cfgCities {
		if c != BlackMarket {
			add(c)
		}
	}
	add(Brecilien)
	add(BlackMarket)
	return out
}

// lookupQualities 是查 AODP 时带的品质。固定 1..5:目录里的 max_quality
// 偶尔不准,多带几档不多花请求。
var lookupQualities = []int{1, 2, 3, 4, 5}

// ── 响应形状 ──────────────────────────────────────────────

type LookupGrid struct {
	Item      catalog.Item `json:"item"`
	FetchedAt time.Time    `json:"fetched_at"`
	Cities    []string     `json:"cities"`
	Qualities []int        `json:"qualities"`
	Params    GridParams   `json:"params"`
	AODP      GridAODP     `json:"aodp"`
	Capture   GridCapture  `json:"capture"`
	Cells     []GridCell   `json:"cells"`
}

// GridParams 是界面判"新不新鲜""薄不薄""离不离谱"要用的阈值。
// 比例一律是小数,界面自己乘 100。
type GridParams struct {
	CaptureWindowHours   float64 `json:"capture_window_hours"`
	MaxHours             float64 `json:"max_hours"`
	HighConfidenceHours  float64 `json:"high_confidence_hours"`
	NearPct              float64 `json:"near_pct"`
	SnapshotSlackMinutes float64 `json:"snapshot_slack_minutes"`
	PageSize             int     `json:"page_size"`
	Friction             float64 `json:"friction"`
	Breakeven            float64 `json:"breakeven"`
	MaxMargin            float64 `json:"max_margin"`
	MinBidDepth          int64   `json:"min_bid_depth"`
	BaselineDays         int     `json:"baseline_days"`
	HistoryDays          int     `json:"history_days"`
	// PreferSlackMinutes / DepthMaxHours 是择边(scan.PickCapture)和深度闸门的两个窗口。
	// 界面写"这一侧为什么用了那一路""闸门只信多近的档"时要用:以前文案里写的是
	// "两路谁新用谁",和实际规则对不上
	PreferSlackMinutes float64 `json:"prefer_slack_minutes"`
	DepthMaxHours      float64 `json:"depth_max_hours"`
}

type GridAODP struct {
	PricesOK  bool   `json:"prices_ok"`
	HistoryOK bool   `json:"history_ok"`
	Error     string `json:"error"`
	Cached    bool   `json:"cached"`
}

type GridCapture struct {
	Orders int `json:"orders"`
	// OtherLocations 是抓包里有、但不在矩阵行里的地点(3003 这类没收敛的 id),按单数计。
	// 黑市的抓包位置没核实,它要是出现,多半就在这里面
	OtherLocations map[string]int `json:"other_locations"`
}

type GridCell struct {
	City    string       `json:"city"`
	Quality int          `json:"quality"`
	Sell    GridSide     `json:"sell"`
	Buy     GridSide     `json:"buy"`
	Spread  *GridSpread  `json:"spread"`
	History *GridHistory `json:"history"`
}

// GridSide 是逐边融合后的一侧。选哪一路和扫描是同一个函数(scan.MergeSide →
// scan.PickCapture):抓包最优档不比 AODP 旧 capture.prefer_slack_minutes 以上、
// 或者两边同价 → 抓包;否则 AODP。被比下去的那一路照样放在子对象里,悬停能对照。
type GridSide struct {
	Pick     string       `json:"pick"`   // "capture" | "aodp" | ""(两边都没有)
	Source   string       `json:"source"` // 同 pick,留着和其他接口的字段名一致
	Best     int64        `json:"best"`
	AgeHours *float64     `json:"age_hours"`
	Capture  *CaptureSide `json:"capture"`
	AODP     *AODPSide    `json:"aodp"`
	// Depth / Note 是扫描的深度闸门对这一边的看法,和机会页卡片上同一边的 depth / note
	// 是 scan.MergeSide 算出来的同一份:选中抓包、最优档在 capture.depth_max_hours 以内时,
	// Depth 是闸门实际用的近价件数(只算这个可信窗口内、capture.book_levels 以内的档);
	// 最优档太旧时 Depth 为空、Note 写"未参与判定";选了 AODP 时两个都空。
	//
	// capture 子对象里的 qty_near 是面板展示整本簿的口径(全部当前档,不管多旧),
	// 两边都有数时一样,不一样的是"有没有数":以前卡片写着"未参与判定",面板却给个近价件数
	Depth *screen.DepthView `json:"depth"`
	Note  string            `json:"note,omitempty"`
}

// CaptureSide 是这一侧的抓包摘要,件数判据全在这里。
type CaptureSide struct {
	Best int64 `json:"best"`
	// Far 是最近一轮里最远的一档(卖单最高 / 买单最低)。Truncated 时只是翻到的那几页的最远
	Far           int64   `json:"far"`
	AgeHours      float64 `json:"age_hours"`
	QtyAtBest     int64   `json:"qty_at_best"`
	OrdersAtBest  int     `json:"orders_at_best"`
	QtyNear       int64   `json:"qty_near"`
	QtyTotal      int64   `json:"qty_total"`
	Orders        int     `json:"orders"`
	Levels        int     `json:"levels"`
	LevelsNear    int     `json:"levels_near"`
	GapAfterNear  float64 `json:"gap_after_near"`
	Truncated     bool    `json:"truncated"`
	DroppedOrders int     `json:"dropped_orders"`
	StaleOrders   int     `json:"stale_orders"`
	// PrevPageOrders 见 BookSide.PrevPageOrders:>0 表示最近一轮是续页,
	// 最优价那几档来自更早翻到的前一页,AgeHours 也就是那一页的龄
	PrevPageOrders int `json:"prev_page_orders"`
}

// AODPSide 是 AODP 那一侧:最优价和最远价(sell_price_max / buy_price_min),各带龄。
type AODPSide struct {
	Best        int64    `json:"best"`
	AgeHours    *float64 `json:"age_hours"`
	Far         int64    `json:"far"`
	FarAgeHours *float64 `json:"far_age_hours"`
}

// GridSpread 是同城挂买→挂卖一轮的账,按 econ.Quote 的挂买挂卖模式算。
type GridSpread struct {
	Raw           float64 `json:"raw"`    // 卖一/买一 − 1,未扣税费
	Margin        float64 `json:"margin"` // 税后毛利率
	MyBid         int64   `json:"my_bid"`
	MyAsk         int64   `json:"my_ask"`
	ProfitPerUnit float64 `json:"profit_per_unit"`
	// Suspect:毛利率超过 filters.max_margin,多半是某一侧挂了 troll 单
	Suspect bool `json:"suspect"`
}

// GridHistory 是一格的成交统计。**卡片和右栏用的是同一个对象**。
type GridHistory struct {
	// Source 是整条 series 来自哪边:"aodp" | "capture"。Stored 表示 AODP 这次没取到,
	// 用的是库里以前存下的 AODP 日线
	Source        string  `json:"source"`
	Stored        bool    `json:"stored"`
	Avg7d         float64 `json:"avg_7d"`
	Avg30d        float64 `json:"avg_30d"`
	DailyQty7d    float64 `json:"daily_qty_7d"`
	DailyQty30d   float64 `json:"daily_qty_30d"`
	DailySilver7d float64 `json:"daily_silver_7d"`
	Days7d        int     `json:"days_7d"`
	Days30d       int     `json:"days_30d"`
	LastDay       string  `json:"last_day"`
	LastAvg       int64   `json:"last_avg"`
	LastQty       int64   `json:"last_qty"`
	PriceMin      int64   `json:"price_min"`
	PriceMax      int64   `json:"price_max"`
	StdDev30d     float64 `json:"stddev_30d"`
	CV            float64 `json:"cv"`
	// Trend30d 是 30 日回归涨跌幅,和本接口其他比例一样是**小数**(0.05 = 涨 5%)。
	// histagg.TrendPct30d 是百分数,这里除过 100 了
	Trend30d float64 `json:"trend_30d"`
	TrendFit float64 `json:"trend_fit"`
	// Series 是窗口内的完整日,升序,不含今天。每个点 [日期, 件数, 均价]
	Series []SeriesPoint `json:"series"`
}

// SeriesPoint 序列化成 ["2026-08-24", 213000, 355],30 个点 × 几十格也就几十 KB。
type SeriesPoint struct {
	Day      string
	Qty      int64
	AvgPrice int64
}

func (p SeriesPoint) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{p.Day, p.Qty, p.AvgPrice})
}

// ── 纯函数部分:不连库、不打网络,单测直接喂数据 ──────────────

type gridInput struct {
	Item   catalog.Item
	Cities []string
	Now    time.Time
	Cfg    conf.Config
	Window time.Duration

	Prices        []aodp.PriceRecord
	PricesErr     error
	PricesCached  bool
	History       []aodp.HistorySeries
	HistoryErr    error
	HistoryCached bool

	DBHistory []store.HistoryRow
	Orders    []store.LiveOrder
	// Conflicted 是 Orders 里最近卷进过多开串城的盘口边,整理时暂停幽灵剔除(见 rounds)。
	// nil = 没有串城
	Conflicted map[model.QuoteKey]time.Time
}

type cellKey struct {
	City    string
	Quality int
}

func hoursPtr(v float64) *float64 { return &v }

func ageOf(st aodp.Stamp, now time.Time) *float64 {
	if a, ok := st.AgeHours(now); ok {
		return hoursPtr(math.Max(0, a))
	}
	return nil
}

// mergeSide 逐边融合。cb 为 nil 表示这一侧没有抓包。
//
// 选哪一路、选中抓包时报多大的龄,都交给 scan.MergeSide:扫描逐边融合用的就是它,
// 同一个盘口在机会页和查价页上的最优价才是同一个数。以前这里自己写了一套
// "谁新用谁、平手用抓包",抓包比 AODP 旧几分钟时两页一个用 AODP、一个用抓包
func mergeSide(cb *BookSide, aodpBest int64, aodpDate aodp.Stamp, aodpFar int64,
	aodpFarDate aodp.Stamp, cfg conf.Config, now time.Time) GridSide {

	var g GridSide
	var cs scan.CapturedSide
	if cb != nil && cb.has() {
		cs = scan.CapturedFrom(cb.raw, cfg.Capture.BookLevels)
		g.Capture = &CaptureSide{
			Best: cb.Support.Best, Far: cb.far,
			AgeHours:      math.Max(0, now.Sub(cb.bestSeen).Hours()),
			QtyAtBest:     cb.Support.QtyAtBest,
			OrdersAtBest:  cb.ordersAtBest,
			QtyNear:       cb.Support.QtyNear,
			QtyTotal:      cb.QtyTotal,
			Orders:        cb.Orders,
			Levels:        cb.LevelCount,
			LevelsNear:    cb.Support.LevelsNear,
			GapAfterNear:  cb.Support.GapAfterNear,
			Truncated:     cb.Truncated,
			DroppedOrders: cb.DroppedOrders,
			StaleOrders:   cb.StaleOrders,

			PrevPageOrders: cb.PrevPageOrders,
		}
	}
	if aodpBest > 0 {
		g.AODP = &AODPSide{Best: aodpBest, AgeHours: ageOf(aodpDate, now)}
		if aodpFar > 0 {
			g.AODP.Far, g.AODP.FarAgeHours = aodpFar, ageOf(aodpFarDate, now)
		}
	}

	m := scan.MergeSide(aodpBest, aodpDate, cs, cfg, now)
	switch {
	case m.UsedCapture:
		// 龄按 MergeSide 给的时间戳算:两边同价时取较新的那个,和扫描一致
		g.Pick, g.Best = "capture", m.Price
		g.AgeHours = hoursPtr(math.Max(0, now.Sub(m.At.T).Hours()))
		g.Depth, g.Note = m.Side.Depth, m.Side.Note
	case g.AODP != nil:
		g.Pick, g.Best, g.AgeHours = "aodp", g.AODP.Best, g.AODP.AgeHours
	}
	g.Source = g.Pick
	return g
}

func spreadOf(sell, buy int64, cfg conf.Config) *GridSpread {
	if sell <= 0 || buy <= 0 {
		return nil
	}
	b := econ.Book{SellMin: sell, BuyMax: buy}
	u := econ.Quote(b, b, makerMaker, cfg.Economics)
	return &GridSpread{
		Raw:    float64(sell)/float64(buy) - 1,
		Margin: u.Margin, MyBid: u.MyBid, MyAsk: u.MyAsk,
		ProfitPerUnit: u.ProfitPerUnit,
		Suspect:       cfg.Filters.MaxMargin > 0 && u.Margin > cfg.Filters.MaxMargin,
	}
}

// dailySeries 把点按天合并(同一天多条按件数加权),只留 historyDays 窗口内的
// **完整日**,升序。口径和 histagg 的窗口一致:今天没走完,不要。
func dailySeries(points []aodp.HistoryPoint, now time.Time, historyDays int) []SeriesPoint {
	today := now.UTC().Truncate(24 * time.Hour)
	cutoff := now.AddDate(0, 0, -historyDays)
	type acc struct{ qty, amount float64 }
	byDay := map[time.Time]*acc{}
	for _, p := range points {
		if !p.Timestamp.Valid() || p.Timestamp.T.Before(cutoff) {
			continue
		}
		day := p.Timestamp.T.UTC().Truncate(24 * time.Hour)
		if !day.Before(today) {
			continue
		}
		a, ok := byDay[day]
		if !ok {
			a = &acc{}
			byDay[day] = a
		}
		a.qty += float64(p.ItemCount)
		a.amount += float64(p.AvgPrice) * float64(p.ItemCount)
	}
	days := make([]time.Time, 0, len(byDay))
	for d := range byDay {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })
	out := make([]SeriesPoint, 0, len(days))
	for _, d := range days {
		a := byDay[d]
		avg := 0.0
		if a.qty > 0 {
			avg = a.amount / a.qty
		}
		out = append(out, SeriesPoint{Day: d.Format("2006-01-02"),
			Qty: int64(a.qty), AvgPrice: int64(math.Round(avg))})
	}
	return out
}

// summarizeHistory 用 histagg 的同一套口径算 7 日 / 30 日指标,
// 扫描、销量榜、查价三处的均价因此是一个数。
func summarizeHistory(item string, k cellKey, points []aodp.HistoryPoint, source string,
	stored bool, now time.Time, cfg conf.Config) *GridHistory {

	if len(points) == 0 {
		return nil
	}
	baseline, long := cfg.Sizing.BaselineDays, cfg.Sizing.HistoryDays
	if baseline <= 0 {
		baseline = 7
	}
	if long <= 0 {
		long = 30
	}
	qk := histagg.QualityKey{ItemID: item, City: k.City, Quality: k.Quality}
	stats, ok := histagg.AggregateByQuality([]aodp.HistorySeries{{
		Location: k.City, ItemID: item, Quality: k.Quality, Data: points,
	}}, now, baseline, long)[qk]
	if !ok || stats.DaysWithData30d == 0 {
		return nil
	}
	series := dailySeries(points, now, long)
	h := &GridHistory{
		Source: source, Stored: stored,
		Avg7d: stats.AvgPrice7d, Avg30d: stats.AvgPrice30d,
		DailyQty7d: stats.DailyVolumeQty, DailyQty30d: stats.DailyVolumeQty30d,
		DailySilver7d: stats.DailyVolumeSilver,
		Days7d:        stats.DaysWithData7d, Days30d: stats.DaysWithData30d,
		StdDev30d: stats.StdDev30d, CV: stats.CV,
		Trend30d: stats.TrendPct30d / 100, TrendFit: stats.TrendFit,
		Series: series,
	}
	for _, p := range series {
		if p.Qty <= 0 || p.AvgPrice <= 0 {
			continue
		}
		if h.PriceMin == 0 || p.AvgPrice < h.PriceMin {
			h.PriceMin = p.AvgPrice
		}
		if p.AvgPrice > h.PriceMax {
			h.PriceMax = p.AvgPrice
		}
	}
	if n := len(series); n > 0 {
		last := series[n-1]
		h.LastDay, h.LastAvg, h.LastQty = last.Day, last.AvgPrice, last.Qty
	}
	return h
}

func rowsToPoints(rows []store.HistoryRow) []aodp.HistoryPoint {
	out := make([]aodp.HistoryPoint, 0, len(rows))
	for _, r := range rows {
		out = append(out, aodp.HistoryPoint{
			ItemCount: r.ItemCount, AvgPrice: r.AvgPrice, Timestamp: aodp.Stamp{T: r.Day},
		})
	}
	return out
}

func clampQuality(q int) int {
	if q < 1 {
		return 1
	}
	if q > 5 {
		return 5
	}
	return q
}

// buildGrid 是 LookupGrid 的全部业务逻辑。
func buildGrid(in gridInput) *LookupGrid {
	now := in.Now
	cities := in.Cities
	wanted := map[string]bool{}
	for _, c := range cities {
		wanted[c] = true
	}

	out := &LookupGrid{
		Item: in.Item, FetchedAt: now, Cities: cities,
		Capture: GridCapture{OtherLocations: map[string]int{}},
		Cells:   []GridCell{},
	}

	// ① 抓包:按 城市 × 品质 × 方向 整理,只收矩阵里有的城市
	maxQ := in.Item.MaxQuality
	if maxQ <= 0 {
		maxQ = 5
	}
	maxQ = clampQuality(maxQ)
	var kept []store.LiveOrder
	// 页满没满要按整次响应数(跨物品、跨品质)。ItemOrders 在 SQL 里对整张表数好了;
	// WithPages 只给没带数的补(单测),所以在按城市筛之前、对这个物品的全部单补
	for _, o := range book.WithPages(in.Orders) {
		if !wanted[o.City] {
			out.Capture.OtherLocations[o.City]++
			continue
		}
		if o.Quality < 1 || o.Quality > 5 {
			continue
		}
		kept = append(kept, o)
		if o.Quality > maxQ {
			maxQ = o.Quality // 目录说只有一档,可实际抓到了更高品质:信数据
		}
	}
	out.Capture.Orders = len(kept)
	books := map[bookKey]BookSide{}
	r := lookupRounds(in.Cfg, in.Conflicted)
	for k, orders := range splitOrders(kept) {
		slack, _ := r.slackOf(k.quoteKey(in.Item.ItemID))
		books[k] = buildSide(orders, k.Side, now, slack, in.Cfg.Filters.NearPct)
	}

	// ② AODP 当前价。每个 物品×城市×品质 都回一行,没数据是 0
	prices := map[cellKey]aodp.PriceRecord{}
	for _, p := range in.Prices {
		if !wanted[p.City] || p.Quality < 1 || p.Quality > 5 {
			continue
		}
		k := cellKey{p.City, p.Quality}
		if _, dup := prices[k]; dup {
			continue
		}
		prices[k] = p
		if (p.SellPriceMin > 0 || p.BuyPriceMax > 0) && p.Quality > maxQ {
			maxQ = p.Quality
		}
	}

	// ③ 成交历史:按 城市 × 品质 整条选一边,不逐桶混、不取平均
	aodpHist := map[cellKey][]aodp.HistoryPoint{}
	for _, s := range in.History {
		if !wanted[s.Location] {
			continue
		}
		k := cellKey{s.Location, s.Quality}
		aodpHist[k] = append(aodpHist[k], s.Data...)
	}
	dbCapture := map[cellKey][]store.HistoryRow{}
	dbAODP := map[cellKey][]store.HistoryRow{}
	for _, r := range in.DBHistory {
		k := cellKey{r.City, r.Quality}
		switch r.Source {
		case store.SourceCapture:
			dbCapture[k] = append(dbCapture[k], r)
		case store.SourceAODP:
			dbAODP[k] = append(dbAODP[k], r)
		}
	}
	history := func(k cellKey) *GridHistory {
		// 现场取的 AODP 优先:它有完整 30 天,抓包只有成员翻过的那几眼
		if in.HistoryErr == nil {
			if pts := aodpHist[k]; len(pts) > 0 {
				if h := summarizeHistory(in.Item.ItemID, k, pts, store.SourceAODP, false, now, in.Cfg); h != nil {
					return h
				}
			}
		}
		if rows := dbCapture[k]; len(rows) > 0 {
			if h := summarizeHistory(in.Item.ItemID, k, rowsToPoints(rows), store.SourceCapture, false, now, in.Cfg); h != nil {
				return h
			}
		}
		// AODP 这次没取到,退到库里以前存下的 AODP 日线
		if in.HistoryErr != nil {
			if rows := dbAODP[k]; len(rows) > 0 {
				return summarizeHistory(in.Item.ItemID, k, rowsToPoints(rows), store.SourceAODP, true, now, in.Cfg)
			}
		}
		return nil
	}

	qualities := make([]int, 0, maxQ)
	for q := 1; q <= maxQ; q++ {
		qualities = append(qualities, q)
	}
	out.Qualities = qualities

	for _, city := range cities {
		for _, q := range qualities {
			k := cellKey{city, q}
			var sellBook, buyBook *BookSide
			if b, ok := books[bookKey{city, q, model.SideOffer}]; ok {
				sellBook = &b
			}
			if b, ok := books[bookKey{city, q, model.SideRequest}]; ok {
				buyBook = &b
			}
			p := prices[k]
			cell := GridCell{
				City: city, Quality: q,
				Sell: mergeSide(sellBook, p.SellPriceMin, p.SellPriceMinDate,
					p.SellPriceMax, p.SellPriceMaxDate, in.Cfg, now),
				Buy: mergeSide(buyBook, p.BuyPriceMax, p.BuyPriceMaxDate,
					p.BuyPriceMin, p.BuyPriceMinDate, in.Cfg, now),
				History: history(k),
			}
			if cell.Sell.Best == 0 && cell.Buy.Best == 0 && cell.History == nil {
				continue // 只输出有数据的格子,缺的界面自己画"无数据"
			}
			cell.Spread = spreadOf(cell.Sell.Best, cell.Buy.Best, in.Cfg)
			out.Cells = append(out.Cells, cell)
		}
	}

	out.AODP = GridAODP{
		PricesOK:  in.PricesErr == nil,
		HistoryOK: in.HistoryErr == nil,
		Cached:    in.PricesCached && in.HistoryCached,
	}
	var errs []string
	if in.PricesErr != nil {
		errs = append(errs, "当前价:"+aodpErrText(in.PricesErr))
	}
	if in.HistoryErr != nil {
		errs = append(errs, "成交历史:"+aodpErrText(in.HistoryErr))
	}
	out.AODP.Error = strings.Join(errs, ";")

	baseline, long := in.Cfg.Sizing.BaselineDays, in.Cfg.Sizing.HistoryDays
	out.Params = GridParams{
		CaptureWindowHours:   in.Window.Hours(),
		MaxHours:             in.Cfg.Freshness.MaxHours,
		HighConfidenceHours:  in.Cfg.Freshness.HighConfidenceHours,
		NearPct:              in.Cfg.Filters.NearPct,
		SnapshotSlackMinutes: in.Cfg.SnapshotSlack().Minutes(),
		PageSize:             book.PageSize,
		Friction:             makerMaker.Friction(in.Cfg.Economics),
		Breakeven:            makerMaker.Breakeven(in.Cfg.Economics),
		MaxMargin:            in.Cfg.Filters.MaxMargin,
		MinBidDepth:          in.Cfg.Filters.MinBidDepth,
		BaselineDays:         baseline,
		HistoryDays:          long,
		PreferSlackMinutes:   in.Cfg.PreferSlack().Minutes(),
		DepthMaxHours:        in.Cfg.Capture.DepthMaxHours,
	}
	return out
}

func aodpErrText(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "等了 " + lookupAODPWait.String() + " 还没回来(多半在和定时扫描排队限流),请求在后台继续,稍后再点一次"
	}
	return err.Error()
}

// ── 服务入口 ──────────────────────────────────────────────

// LookupGrid 查一个物品的价格矩阵。
//
// 每查一个新物品 = 2 次 AODP 请求(prices + history),和扫描共用一个限流器;
// 缓存命中时 0 次。库读失败是硬错误,AODP 失败只填 aodp.error、照样回抓包那部分。
func (s *Service) LookupGrid(ctx context.Context, itemID string) (*LookupGrid, error) {
	now := time.Now().UTC()
	item := catalog.Item{ItemID: itemID, MaxQuality: 5}
	if cat := s.cat.Load(); cat != nil {
		if it, ok := cat.Get(itemID); ok {
			item = it
		}
	}
	cities := lookupCities(s.Cfg.Cities)
	// 窗口和扫描读抓包同一个,见 LookupBook
	window := s.Cfg.CaptureWindow()
	historyDays := s.Cfg.Sizing.HistoryDays
	if historyDays <= 0 {
		historyDays = 30
	}

	// 先读库:快,而且不受 AODP 排队的影响
	since := now.Add(-window)
	orders, err := s.Store.ItemOrders(ctx, itemID, since)
	if err != nil {
		return nil, fmt.Errorf("读抓包挂单: %w", err)
	}
	dbHist, err := s.Store.ItemHistory(ctx, itemID, now.AddDate(0, 0, -historyDays-1))
	if err != nil {
		return nil, fmt.Errorf("读库里的成交历史: %w", err)
	}

	in := gridInput{
		Item: item, Cities: cities, Now: now, Cfg: s.Cfg, Window: window,
		DBHistory: dbHist, Orders: orders,
		Conflicted: s.itemConflicts(itemID, orders, since),
	}
	key := itemID + "|" + strings.Join(cities, ",")
	waitCtx, cancel := context.WithTimeout(ctx, lookupAODPWait)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		in.Prices, in.PricesCached, in.PricesErr = lookupPrices.get(waitCtx, key,
			func(c context.Context) ([]aodp.PriceRecord, error) {
				return s.aodp.FetchPrices(c, []string{itemID}, cities, lookupQualities)
			})
	}()
	go func() {
		defer wg.Done()
		in.History, in.HistoryCached, in.HistoryErr = lookupHistory.get(waitCtx, key,
			func(c context.Context) ([]aodp.HistorySeries, error) {
				return s.aodp.FetchHistory(c, []string{itemID}, cities, lookupQualities, historyDays, 24)
			})
	}()
	wg.Wait()
	if in.PricesErr != nil || in.HistoryErr != nil {
		slog.Warn("查价时 AODP 没取全", "item", itemID, "prices", in.PricesErr, "history", in.HistoryErr)
	}
	// 当前价和机会页的重算用同一份:并上快照、记下这次取回的(见 aodp_fresh.go)。
	// 这次没取到时格子上就是快照里那份 AODP,和卡片一样;aodp.prices_ok 照实报 false
	in.Prices = s.gridPrices(itemID, in.Prices, now)
	return buildGrid(in), nil
}
