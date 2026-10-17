package flip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/book"
	"albion-guild/internal/depth"
	"albion-guild/internal/model"
	"albion-guild/internal/rank"
	"albion-guild/internal/refine"
	"albion-guild/internal/scan"
	"albion-guild/internal/store"
	"albion-guild/internal/world"
)

// 精炼页的服务层(设计 §4.3)。引擎 refine.Plan 是纯函数,这里负责把数据喂给它:
//
//  1. 由配方图收集要读的盘口(refine.ItemsFor):物品 × 城市 × 品质 1 × 两个方向
//  2. 用 storeBooks.readSides 一次读出,book.Build 剔幽灵单 —— 和扫描、WS 报价、查价页
//     是同一套口径。**不用 Service.Depth**:它走旧的 SQL,不剔幽灵单
//  3. 读单窗口是 refining.window_hours(默认 72h),不是扫描的 6h:用户是抓一次包、
//     之后再算。超过 6h 的价照用,引擎按年龄标黄(6h)/ 标红(24h)。窗口拉长以后,
//     续页只认隔得够近的(refinePageGap),每一档带自己的观测时刻进引擎
//  4. 抓包没有的盘口边、或者抓包整边比 sources.max_age_hours 还旧的,用 AODP 兜底
//     (scan.MergeSide,和扫描、查价页同一个择边函数),只有价格、没有件数,标「深度未知」
//  5. 成交量取 refining.volume_days(默认 14)个自然日,按「总量 ÷ 自然日」平均
//  6. 同样的参数 2 秒内只算一次,并发的同样请求合并成一次(ttlCache)
//
// 个人参数(FCE、今日专注、站费、k)由请求带上,服务端只放默认值(conf.Refining)。

// RefineSource 是精炼页的三路数据。Service.RefineSrc 为 nil 时用库和共享的 AODP 客户端;
// 测试(包括 api 包的 handler 测试)换成假的,不连库、不联网。
type RefineSource interface {
	// BookOrders 同 store.BookOrders:一批盘口边在 since 之后看得到的原始挂单,Page 已数好
	BookOrders(ctx context.Context, keys []model.QuoteKey, since time.Time) ([]store.LiveOrder, error)
	// AODPPrices 取 items × cities 品质 1 的 AODP 当前价
	AODPPrices(ctx context.Context, items, cities []string) ([]aodp.PriceRecord, error)
	// DailyVolume 同 store.DailyVolume
	DailyVolume(ctx context.Context, items []string, city string, quality int, first, last time.Time) ([]store.ItemDay, error)
}

// conflictCoverage 是 ConflictSource 的可选能力(*ingest.Ingestor 有):串城判定能覆盖到多早。
type conflictCoverage interface {
	ConflictCoverageSince(now time.Time) time.Time
}

// ParamError 是请求参数不合法。API 层据此回 400,其余错误回 500。
type ParamError struct{ Err error }

func (e *ParamError) Error() string { return e.Err.Error() }
func (e *ParamError) Unwrap() error { return e.Err }

func paramErr(format string, a ...any) error { return &ParamError{Err: fmt.Errorf(format, a...)} }

const (
	// refineCacheTTL:同样的参数 2 秒内直接回上一次的结果(设计 §4.3 第 5 步)。
	// 界面收到 WS 报价后防抖 1.5 秒重算,几个人同时开着精炼页也只算一次
	refineCacheTTL = 2 * time.Second
	// refineComputeTimeout 是一次计算(读库 + 等 AODP + 算)在后台最多跑多久。
	// 和发起它的 HTTP 请求脱钩:请求断了,算完的结果照样进缓存
	refineComputeTimeout = time.Minute
	// refinePricesTTL:AODP 当前价按 线 × 城市 缓存 60 秒,和查价页那份一样长
	refinePricesTTL = 60 * time.Second
	// refinePageGap 是精炼读簿时认「续页」的最大间隔(book.Opts.MaxPageGap)。同一次浏览
	// 翻页是几秒的事(实测 0.5 秒一页),30 分钟已经很宽;读簿窗口 72h,不设这个上限的话,
	// 隔了几天的旧页会被当成续页之前那一页、把早就没了的便宜单留下来(审查 finding 2)。
	// 认错成「重新翻」的代价只是少算几张更便宜的单,成本偏高、不会偏低
	refinePageGap = 30 * time.Minute
	// refineQuality:资源和半成品没有品质之分,只读品质 1
	refineQuality = 1
)

// refineState 是精炼页的缓存。零值可用(第一次调用时建),所以测试里直接构造的
// &Service{} 也能用;每个 Service 各一份,测试之间不会串。
type refineState struct {
	once   sync.Once
	plans  *ttlCache[*RefinePlan]
	matrix *ttlCache[*RefineMatrix]
	prices *ttlCache[[]aodp.PriceRecord]
	// clock 只给测试用:算价格年龄的「现在」和缓存过期都按它。nil = time.Now
	clock func() time.Time
}

func (r *refineState) init() {
	r.once.Do(func() {
		r.plans = newTTLCache[*RefinePlan](refineCacheTTL, refineComputeTimeout)
		r.matrix = newTTLCache[*RefineMatrix](refineCacheTTL, 2*refineComputeTimeout)
		r.prices = newTTLCache[[]aodp.PriceRecord](refinePricesTTL, 2*time.Minute)
		if r.clock != nil {
			r.plans.now, r.matrix.now, r.prices.now = r.clock, r.clock, r.clock
		}
	})
}

func (r *refineState) now() time.Time {
	if r.clock != nil {
		return r.clock()
	}
	return time.Now()
}

// ── 响应 ───────────────────────────────────────────────────────────────

// RefinePlan 是 POST /api/refine/plan 的响应:引擎的结果,加上这次读了哪些数据。
type RefinePlan struct {
	*refine.Result
	Books          RefineBooks    `json:"books"`
	ServerDefaults ServerDefaults `json:"server_defaults"`
}

// ServerDefaults 是服务端配置里的精炼默认值(conf.Refining),原样回给界面:
// 界面据此判断用户填的数是不是默认值,「未核实」徽章上的 k 录入日期和金价也从这里来。
type ServerDefaults struct {
	WindowHours               float64            `json:"window_hours"`
	VolumeDays                int                `json:"volume_days"`
	DailyBonus                map[string]float64 `json:"daily_bonus"`
	StationFeePer100          float64            `json:"station_fee_per100"`
	StationFeeByCity          map[string]float64 `json:"station_fee_by_city"`
	TransmuteFactor           float64            `json:"transmute_factor"`
	TransmuteMeasuredAt       string             `json:"transmute_measured_at"`
	TransmuteGoldPrice        float64            `json:"transmute_gold_price"`
	TransmuteStationFeePer100 float64            `json:"transmute_station_fee_per100"`
	FocusPerDay               float64            `json:"focus_per_day"`
	FocusCap                  float64            `json:"focus_cap"`
	Capital                   int64              `json:"capital"`
}

func (s *Service) serverDefaults() ServerDefaults {
	r := s.Cfg.Refining
	orEmpty := func(m map[string]float64) map[string]float64 {
		if m == nil {
			return map[string]float64{}
		}
		return m
	}
	return ServerDefaults{
		WindowHours: r.WindowHours, VolumeDays: r.VolumeDays, DailyBonus: orEmpty(r.DailyBonus),
		StationFeePer100: r.StationFeePer100, StationFeeByCity: orEmpty(r.StationFeeByCity),
		TransmuteFactor: r.TransmuteFactor, TransmuteMeasuredAt: r.TransmuteMeasuredAt,
		TransmuteGoldPrice: r.TransmuteGoldPrice, TransmuteStationFeePer100: r.TransmuteStationFeePer100,
		FocusPerDay: r.FocusPerDay, FocusCap: r.FocusCap, Capital: s.Cfg.Capital,
	}
}

// RefineBooks 汇总这一次喂给引擎的盘口从哪来。
type RefineBooks struct {
	// WindowHours 是读抓包的窗口(refining.window_hours)
	WindowHours float64   `json:"window_hours"`
	ReadAt      time.Time `json:"read_at"`
	// Sides 是要读的盘口边数;Capture / AODP / Empty 是其中用抓包阶梯、只有 AODP 价
	// (深度未知)、两边都没有的边数
	Sides   int `json:"sides"`
	Capture int `json:"capture"`
	AODP    int `json:"aodp"`
	Empty   int `json:"empty"`
	// Conflicted 是最近卷进多开串城、读簿时暂停了幽灵剔除的盘口边(物品|城市|卖单/买单)
	Conflicted []string `json:"conflicted"`
	// AODPNewer 是抓包有阶梯、但 AODP 之后报了不同的最优价的盘口边(见 refineBooks)
	AODPNewer []AODPNewer `json:"aodp_newer"`
	// AODPOverStale 是抓包整边都比 sources.max_age_hours 旧、改用了 AODP 价(深度未知)的
	// 盘口边(物品|城市|卖单/买单),算在 AODP 里
	AODPOverStale []string `json:"aodp_over_stale"`
	// ConflictsSince 是串城判定能覆盖到多早:冲突记录只在服务端内存里,重启就清空、
	// 超过保留期也删掉。比它早的串城看不到,那段时间的盘口照常剔幽灵单。
	// null = 没接串城检测(测试里)
	ConflictsSince *time.Time `json:"conflicts_since"`
	// AODPOK=false:这次没取到 AODP 当前价(只用了快照和查价页取过的),AODPError 说明原因
	AODPOK    bool   `json:"aodp_ok"`
	AODPError string `json:"aodp_error,omitempty"`
}

// AODPNewer 是一个「AODP 比抓包新、价也不同」的盘口边。
type AODPNewer struct {
	Item         string  `json:"item"`
	City         string  `json:"city"`
	Side         string  `json:"side"` // sell 卖单 | buy 买单
	CapturePrice int64   `json:"capture_price"`
	CaptureAgeH  float64 `json:"capture_age_h"`
	AODPPrice    int64   `json:"aodp_price"`
	AODPAgeH     float64 `json:"aodp_age_h"`
}

// 服务层加的警告代码(引擎自己的见 refine/plan.go)。
const (
	WarnAODPNewer       = "aodp_newer"       // 抓包阶梯之后 AODP 报了不同的价,阶梯可能变了
	WarnAODPUnavailable = "aodp_unavailable" // AODP 当前价没取到
	WarnConflicted      = "conflicted"       // 盘口最近串过城,读簿时没剔幽灵单
)

// ── 入口 ───────────────────────────────────────────────────────────────

// RefineDefaults 是一次请求打底的参数:配置里的默认值(conf.Refining、conf.Economics),
// city 为空时用目标那条线的专精城。API 层把请求体解码到它上面。
func (s *Service) RefineDefaults(target, city string) refine.Params {
	return refine.DefaultsFrom(target, city, s.Cfg)
}

// Refine 算一个目标的候选路线。参数不合法返回 *ParamError;读库失败返回别的错误;
// 缺价、深度不够、AODP 没取到都不是错误,而是结果里的警告。
func (s *Service) Refine(ctx context.Context, p refine.Params) (*RefinePlan, error) {
	s.refine.init()
	p = refine.Normalized(p)
	if err := s.checkRefine(p); err != nil {
		return nil, err
	}
	key, err := json.Marshal(p) // Now 是 json:"-",不进键
	if err != nil {
		return nil, err
	}
	v, _, err := s.refine.plans.get(ctx, string(key), func(c context.Context) (*RefinePlan, error) {
		return s.computeRefine(c, p)
	})
	return v, err
}

// checkRefine 在读库之前校验:引擎的校验,再加几条服务端才知道的约束。
func (s *Service) checkRefine(p refine.Params) error {
	probe := p
	probe.Now = s.refine.now()
	if err := refine.Validate(probe); err != nil {
		return &ParamError{Err: err}
	}
	if err := refine.CheckTarget(s.Recipes(), p.Target); err != nil {
		return &ParamError{Err: err}
	}
	// 黑市在服务端城市列表里(倒爷那边要它当装备的销地),但精炼三个角色它一个都当不了:
	// 没有精炼站、只收装备、只有 NPC 买单没有卖单。界面那边 craftCities 早就把它滤掉了,
	// 这里补上服务端这一道,免得直接调 API 时算出一份空盘口的结果
	known := func(c string) bool { return c != world.BlackMarket && slices.Contains(s.Cfg.Cities, c) }
	if !known(p.City) {
		return paramErr("生产城市 %q 不在服务端的城市列表 %v 里", p.City, s.Cfg.Cities)
	}
	if !known(p.Sell.City) {
		return paramErr("出售城市 %q 不在服务端的城市列表 %v 里", p.Sell.City, s.Cfg.Cities)
	}
	for _, c := range p.Sources.Cities {
		if !known(c) {
			return paramErr("买料城市 %q 不在服务端的城市列表 %v 里", c, s.Cfg.Cities)
		}
	}
	if w := s.Cfg.Refining.WindowHours; p.Sources.MaxAgeHours > w {
		return paramErr("数据最大年龄 %g 小时超过服务端读抓包的窗口 %g 小时(refining.window_hours)",
			p.Sources.MaxAgeHours, w)
	}
	if c := s.Cfg.Refining.FocusCap; p.Focus.Budget > c {
		return paramErr("今日专注 %g 超过单个角色的上限 %g(专注按角色结算,D4)", p.Focus.Budget, c)
	}
	return nil
}

// computeRefine 是一次真正的计算,在缓存的后台 goroutine 里跑。
func (s *Service) computeRefine(ctx context.Context, p refine.Params) (*RefinePlan, error) {
	now := s.refine.now().UTC()
	p.Now = now
	recipes := s.Recipes()
	items, err := refine.ItemsFor(recipes, p.Target)
	if err != nil {
		return nil, &ParamError{Err: err}
	}
	line, _ := refine.LineOfItem(p.Target)
	keys := refineKeys(items, buyCities(p), []string{p.Target}, p.Sell.City)
	books, info, warns, err := s.refineBooks(ctx, keys, line, allCities(p), now, p.Sources.MaxAgeHours)
	if err != nil {
		return nil, err
	}
	vols, err := s.refineVolumes(ctx, []string{p.Target}, p.Sell.City, now)
	if err != nil {
		return nil, err
	}
	res, err := refine.Plan(refine.Input{Params: p, Books: books, Recipes: recipes, Volume: vols[p.Target]})
	if err != nil {
		return nil, err // 参数在 checkRefine 里校验过,到这里只能是内部问题(比如配方表是空的)
	}
	res.Warnings = append(res.Warnings, warns...)
	return &RefinePlan{Result: res, Books: info, ServerDefaults: s.serverDefaults()}, nil
}

// buyCities 是买料的城市:生产城市(挂买单收料只在这里)加上可以买料的城市。
func buyCities(p refine.Params) []string {
	return uniqStrings(append([]string{p.City}, p.Sources.Cities...))
}

// allCities 是这次要向 AODP 要价的城市。
func allCities(p refine.Params) []string {
	return uniqStrings(append(buyCities(p), p.Sell.City))
}

func uniqStrings(in []string) []string {
	var out []string
	for _, s := range in {
		if s != "" && !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// refineKeys:items 在每个买料城市的两边,加上 targets 在出售城市的两边。
// 两边都要:卖单是买料的价,买单是挂买单收料、砸买单估余料和吃买单出货的价。
func refineKeys(items, cities, targets []string, sellCity string) []model.QuoteKey {
	seen := map[model.QuoteKey]bool{}
	var out []model.QuoteKey
	add := func(item, city string) {
		for _, side := range []model.Side{model.SideOffer, model.SideRequest} {
			k := model.QuoteKey{ItemID: item, LocationID: city, Quality: refineQuality, Side: side}
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	for _, item := range items {
		for _, c := range cities {
			add(item, c)
		}
	}
	for _, t := range targets {
		add(t, sellCity)
	}
	return out
}

// ── 数据 ───────────────────────────────────────────────────────────────

func (s *Service) refineSource() RefineSource {
	if s.RefineSrc != nil {
		return s.RefineSrc
	}
	return serviceRefineSource{s}
}

// serviceRefineSource 是线上的数据来源:库(测试里的 ladder 优先)和共享的 AODP 客户端。
type serviceRefineSource struct{ s *Service }

func (r serviceRefineSource) BookOrders(ctx context.Context, keys []model.QuoteKey, since time.Time) ([]store.LiveOrder, error) {
	switch {
	case r.s.ladder != nil:
		return r.s.ladder.BookOrders(ctx, keys, since)
	case r.s.Store != nil:
		return r.s.Store.BookOrders(ctx, keys, since)
	}
	return nil, errors.New("没有库,读不了抓包")
}

func (r serviceRefineSource) AODPPrices(ctx context.Context, items, cities []string) ([]aodp.PriceRecord, error) {
	if r.s.aodp == nil {
		return nil, errors.New("没有 AODP 客户端")
	}
	// 和扫描、查价页共用一个客户端:限流器的状态在 Client 里
	return r.s.aodp.FetchPrices(ctx, items, cities, []int{refineQuality})
}

func (r serviceRefineSource) DailyVolume(ctx context.Context, items []string, city string, quality int,
	first, last time.Time) ([]store.ItemDay, error) {
	if r.s.Store == nil {
		return nil, errors.New("没有库,读不了成交历史")
	}
	return r.s.Store.DailyVolume(ctx, items, city, quality, first, last)
}

// ordersOnly 把 RefineSource 的读簿包成 storeBooks 要的 captureReader。
// 精炼页不扩物品集合,CapturedItems 用不上。
type ordersOnly struct{ src RefineSource }

func (o ordersOnly) BookOrders(ctx context.Context, keys []model.QuoteKey, since time.Time) ([]store.LiveOrder, error) {
	return o.src.BookOrders(ctx, keys, since)
}

func (o ordersOnly) CapturedItems(context.Context, []string, []int, time.Time) ([]store.CapturedItem, error) {
	return nil, nil
}

// refineBooks 读盘口、融合 AODP,转成引擎的输入。
//
// 每个盘口边按 scan.MergeSide 择边(扫描、查价页同一个函数),但有一处不同:
// **抓包有阶梯时一律用抓包**,哪怕 MergeSide 因为 AODP 更新、价不同而选了 AODP。
// 精炼要沿阶梯吃深度,AODP 只有一个价、没有件数,换成它等于假设那个价有无限件
// (卖一只有几件、却要吃二十几件时,会全按卖一算)。所以留着阶梯,把这种边列进
// books.aodp_newer、加一条 aodp_newer 警告,让人知道阶梯顶上可能已经变了。
//
// 反过来,抓包这一边整个比 maxAgeHours(请求的 sources.max_age_hours)还旧、而 AODP 的价
// 在这个年龄以内时,用 AODP(深度未知):引擎会把太旧的抓包当缺价丢掉,留着它等于把
// 新的 AODP 价也挡在外面(审查 finding 8)。这种边列进 books.aodp_over_stale。
func (s *Service) refineBooks(ctx context.Context, keys []model.QuoteKey, line refine.Line,
	cities []string, now time.Time, maxAgeHours float64) (refine.Books, RefineBooks, []refine.Warning, error) {

	src := s.refineSource()
	window := s.Cfg.Refining.Window()
	since := now.Add(-window)
	info := RefineBooks{WindowHours: window.Hours(), ReadAt: now, Sides: len(keys),
		Conflicted: []string{}, AODPNewer: []AODPNewer{}, AODPOverStale: []string{}}
	if cc, ok := s.Conflicts.(conflictCoverage); ok {
		t := cc.ConflictCoverageSince(now).UTC()
		info.ConflictsSince = &t
	}

	// 串城的盘口边把 slack 放到整个读单窗口(见 rounds):窗口是 72h,不是扫描的 6h。
	// 续页只认隔得够近的(refinePageGap)
	sb := &storeBooks{st: ordersOnly{src}, conflicts: s.Conflicts, slack: s.Cfg.SnapshotSlack(), window: window,
		pageGap: refinePageGap}
	sides, flagged, err := sb.readSides(ctx, keys, since, since)
	if err != nil {
		return nil, info, nil, fmt.Errorf("读抓包挂单: %w", err)
	}

	prices, aerr := s.refinePrices(ctx, src, line.Items(), cities, now)
	info.AODPOK = aerr == nil
	var warns []refine.Warning
	if aerr != nil {
		info.AODPError = aodpErrText(aerr)
		warns = append(warns, refine.Warning{Code: WarnAODPUnavailable,
			Msg: "这次没取到 AODP 当前价(" + info.AODPError + "),抓包没有的盘口只能用之前取过的 AODP,没有的按缺价算"})
	}
	type cellKey struct{ item, city string }
	idx := map[cellKey]aodp.PriceRecord{}
	for _, p := range prices {
		if p.Quality == refineQuality {
			idx[cellKey{p.ItemID, p.City}] = p
		}
	}

	books := refine.Books{}
	for _, k := range keys {
		pr := idx[cellKey{k.ItemID, k.LocationID}]
		px, at := pr.SellPriceMin, pr.SellPriceMinDate
		if k.Side == model.SideRequest {
			px, at = pr.BuyPriceMax, pr.BuyPriceMaxDate
		}
		if !at.Valid() {
			px = 0 // 没时间戳的价查不了新鲜度,不用
		}
		side, has := sides[k]
		var cs scan.CapturedSide
		if has {
			cs = scan.CapturedFrom(side, 0)
		}
		m := scan.MergeSide(px, at, cs, s.Cfg, now)
		aodpAge, aodpOK := at.AgeHours(now)
		aodpOK = aodpOK && px > 0
		// 抓包整边都比 max_age_hours 旧(最近一眼 side.Newest 都过了),AODP 却在这个年龄以内
		captureStale := has && now.Sub(side.Newest).Hours() > maxAgeHours
		useAODP := captureStale && aodpOK && aodpAge <= maxAgeHours

		var l refine.Ladder
		switch {
		case has && !useAODP:
			l = ladderOf(side)
			info.Capture++
			if flagged[k] {
				info.Conflicted = append(info.Conflicted, sideName(k))
			}
			if m.Superseded {
				capAge := now.Sub(side.Levels[0].Seen).Hours()
				info.AODPNewer = append(info.AODPNewer, AODPNewer{
					Item: k.ItemID, City: k.LocationID, Side: sideWord(k.Side),
					CapturePrice: side.Levels[0].Price, CaptureAgeH: round3(capAge),
					AODPPrice: px, AODPAgeH: round3(aodpAge),
				})
			}
		case aodpOK:
			// 抓包没有这条边,或者抓包太旧、AODP 够新:只有价格,没有件数
			l = refine.Ladder{Levels: []depth.Level{{Price: px}}, Seen: at.T, Source: "aodp", DepthUnknown: true}
			info.AODP++
			if has {
				info.AODPOverStale = append(info.AODPOverStale, sideName(k))
			}
		default:
			info.Empty++
			continue
		}
		b := books[refine.Key{ItemID: k.ItemID, City: k.LocationID}]
		if k.Side == model.SideRequest {
			b.Bids = l
		} else {
			b.Asks = l
		}
		books[refine.Key{ItemID: k.ItemID, City: k.LocationID}] = b
	}
	sort.Strings(info.Conflicted)
	sort.Strings(info.AODPOverStale)
	sort.Slice(info.AODPNewer, func(i, j int) bool {
		a, b := info.AODPNewer[i], info.AODPNewer[j]
		if a.Item != b.Item {
			return a.Item < b.Item
		}
		if a.City != b.City {
			return a.City < b.City
		}
		return a.Side < b.Side
	})
	if n := len(info.AODPNewer); n > 0 {
		warns = append(warns, refine.Warning{Code: WarnAODPNewer,
			Msg: fmt.Sprintf("%d 个盘口边在抓包之后 AODP 报了不同的最优价(%s):仍按抓包阶梯算,阶梯顶上可能已经变了,见 books.aodp_newer",
				n, newerSample(info.AODPNewer))})
	}
	if n := len(info.Conflicted); n > 0 {
		warns = append(warns, refine.Warning{Code: WarnConflicted,
			Msg: fmt.Sprintf("%d 个盘口边最近卷进过多开串城,读簿时没剔幽灵单,可能多算了已经成交的挂单", n)})
	}
	return books, info, warns, nil
}

// ladderOf 把 book.Build 整理好的一边转成引擎的阶梯。stale 的档不进来:那些单多半已经没了。
// 每一档带上自己的观测时刻(book.Level.Seen):续页之前那一页、串城时暂停剔除留下的旧单
// 都比整边的「最近一眼」旧,新鲜度要按实际吃到的那几档报(审查 finding 2)。
func ladderOf(side book.Side) refine.Ladder {
	l := refine.Ladder{Levels: make([]depth.Level, 0, len(side.Levels)),
		LevelSeen: make([]time.Time, 0, len(side.Levels)),
		Seen:      side.Newest, Truncated: side.Truncated, Source: "capture"}
	for _, lv := range side.Levels {
		l.Levels = append(l.Levels, depth.Level{Price: lv.Price, Qty: lv.Qty})
		l.LevelSeen = append(l.LevelSeen, lv.Seen)
	}
	return l
}

func sideWord(s model.Side) string {
	if s == model.SideRequest {
		return "buy"
	}
	return "sell"
}

func sideName(k model.QuoteKey) string {
	if k.Side == model.SideRequest {
		return k.ItemID + "|" + k.LocationID + "|买单"
	}
	return k.ItemID + "|" + k.LocationID + "|卖单"
}

func newerSample(xs []AODPNewer) string {
	var parts []string
	for i, x := range xs {
		if i == 3 {
			parts = append(parts, "…")
			break
		}
		parts = append(parts, x.Item+"@"+x.City)
	}
	return strings.Join(parts, "、")
}

func round3(v float64) float64 { return float64(int64(v*1000+0.5)) / 1000 }

// refinePrices 是这条线在 cities 的 AODP 当前价:这次取回的 ∪ 快照里的 ∪ 查价页取过的,
// 逐字段新的胜(和查价格子、机会页重算同一个并法,见 aodp_fresh.go)。按 线 × 城市 缓存 60 秒:
// 同一条线的几个目标、矩阵的 25 个格子共用一次请求。最多等 lookupAODPWait,到点就先用手里有的,
// 请求留在后台跑完进缓存。
func (s *Service) refinePrices(ctx context.Context, src RefineSource, items, cities []string,
	now time.Time) ([]aodp.PriceRecord, error) {

	key := strings.Join(items, ",") + "|" + strings.Join(cities, ",")
	// AODP 已知连不上时只等一小会儿(见 aodp_wait.go),不然精炼页每次重算都卡 12 秒
	wait, down := s.aodpWait(now)
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	fetched, _, err := s.refine.prices.get(waitCtx, key, func(c context.Context) ([]aodp.PriceRecord, error) {
		return src.AODPPrices(c, items, cities)
	})
	err = asDown(err, down)
	if err != nil {
		slog.Warn("精炼页 AODP 当前价没取到", "items", len(items), "cities", cities, "err", err)
	}
	return s.mergePrices(items, fetched, now), err
}

// mergePrices 是 gridPrices 的多物品版本。
func (s *Service) mergePrices(items []string, fetched []aodp.PriceRecord, now time.Time) []aodp.PriceRecord {
	s.fresh.note(fetched, now)
	want := map[string]bool{}
	for _, it := range items {
		want[it] = true
	}
	var since time.Time
	var base []aodp.PriceRecord
	if snap := s.snap.Load(); snap != nil {
		since = snap.FetchedAt
		for _, p := range snap.Prices {
			if want[p.ItemID] {
				base = append(base, p)
			}
		}
	}
	idx := map[freshKey]int{}
	var out []aodp.PriceRecord
	add := func(r aodp.PriceRecord) {
		if !want[r.ItemID] {
			return
		}
		k := freshKey{r.ItemID, r.City, r.Quality}
		if i, ok := idx[k]; ok {
			out[i], _ = scan.NewerPrice(out[i], r)
			return
		}
		idx[k] = len(out)
		out = append(out, r)
	}
	for _, r := range fetched {
		add(r)
	}
	for _, r := range base {
		add(r)
	}
	for _, r := range s.fresh.since(since, "") {
		add(r)
	}
	return out
}

// refineVolumes 是每个物品在 city 的日成交量:最近 volume_days 个完整自然日(不含今天,
// rank.Window),总量 ÷ 天数。**不用「÷ 有数据的天数」**:那样会高估稀疏的物品
// (T8.4 钢条 @ Thetford 14 天 65 根、只有 8 天有数据:按自然日 4.6 根/天,按有数据的天 8.1)。
// 一行都没有是「无数据」(AODP 覆盖空洞,规则表 G3),Known=false,不当成 0。
func (s *Service) refineVolumes(ctx context.Context, items []string, city string,
	now time.Time) (map[string]refine.Volume, error) {

	days := s.Cfg.Refining.VolumeDays
	first, last := rank.Window(now, days)
	rows, err := s.refineSource().DailyVolume(ctx, items, city, refineQuality, first, last)
	if err != nil {
		return nil, fmt.Errorf("读成交历史: %w", err)
	}
	type acc struct {
		sum     int64
		withQty int
		rows    int
		sources map[string]bool
	}
	per := map[string]*acc{}
	for _, r := range rows {
		a := per[r.ItemID]
		if a == nil {
			a = &acc{sources: map[string]bool{}}
			per[r.ItemID] = a
		}
		a.rows++
		a.sum += r.ItemCount
		if r.ItemCount > 0 {
			a.withQty++
		}
		a.sources[r.Source] = true
	}
	out := make(map[string]refine.Volume, len(items))
	for _, it := range items {
		v := refine.Volume{Days: days}
		if a := per[it]; a != nil && a.rows > 0 {
			v.Known = true
			v.PerDay = float64(a.sum) / float64(days)
			v.DaysWithData = a.withQty
			var src []string
			for k := range a.sources {
				src = append(src, k)
			}
			sort.Strings(src)
			v.Source = strings.Join(src, "+")
		}
		out[it] = v
	}
	return out, nil
}

// ── 矩阵 ───────────────────────────────────────────────────────────────

// RefineMatrix 是 GET /api/refine/matrix 的响应:一条线 25 个档位,各取最好的那条候选
// (替代表格的排行榜)。每一格只给摘要,步骤清单点进去再调 /api/refine/plan。
type RefineMatrix struct {
	Line string `json:"line"`
	City string `json:"city"`
	Qty  int    `json:"qty"`
	// ParamsUsed 是 25 格共用的参数(Target 为空)
	ParamsUsed     refine.Params           `json:"params_used"`
	Now            time.Time               `json:"now"`
	Unverified     []refine.UnverifiedUsed `json:"unverified"`
	Cells          []MatrixCell            `json:"cells"`
	Books          RefineBooks             `json:"books"`
	Warnings       []refine.Warning        `json:"warnings"`
	RecipeSource   string                  `json:"recipe_source"`
	ServerDefaults ServerDefaults          `json:"server_defaults"`
}

// MatrixCell 是矩阵的一格。
type MatrixCell struct {
	Target  string `json:"target"`
	Tier    int    `json:"tier"`
	Enchant int    `json:"enchant"`
	// Error 非空说明这一格没算出来(不该发生:参数在进来之前校验过)
	Error  string           `json:"error,omitempty"`
	Sell   *refine.SellInfo `json:"sell"`
	Volume *refine.Volume   `json:"volume"`
	Data   *refine.DataInfo `json:"data"`
	// Best 是排第一的普通候选(不含参照行);tier 没解锁、拿不满料时也照样给,看 filled
	Best *MatrixBest `json:"best"`
	// Candidates 是这一格一共列了几条普通候选
	Candidates int `json:"candidates"`
	// Warnings 是和路线无关的警告(卖价、断崖、成交量、未解锁)
	Warnings []refine.Warning `json:"warnings"`
}

// MatrixBest 是一条候选的摘要:refine.Candidate 去掉步骤清单和余料明细。
type MatrixBest struct {
	ID             string           `json:"id"`
	Label          string           `json:"label"`
	Filled         bool             `json:"filled"`
	UnitCost       float64          `json:"unit_cost"`
	TotalCost      float64          `json:"total_cost"`
	UpfrontCapital float64          `json:"upfront_capital"`
	Profit         refine.Profits   `json:"profit"`
	Focus          refine.FocusUse  `json:"focus"`
	SilverPerFocus *float64         `json:"silver_per_focus"`
	Daily          *refine.Daily    `json:"daily"`
	OldestAgeH     *float64         `json:"oldest_age_h"`
	AgeLevel       string           `json:"age_level"`
	Warnings       []refine.Warning `json:"warnings"`
	// MainRawBreakeven 是主原料(refine.Result.MainRaw)的盈亏平衡价:照这条路线、别的价不变,
	// 主原料到手均价涨到这里利润归零。料不满、没有主方式利润时为 nil
	MainRawBreakeven *refine.Breakeven `json:"main_raw_breakeven"`
}

// RefineMatrix 算一条线 25 个档位。base 是共用的参数(Target 不看);专注模式不能是 custom:
// 自定义的 tier 集合对低 tier 的目标没有意义。
func (s *Service) RefineMatrix(ctx context.Context, lineKey string, base refine.Params) (*RefineMatrix, error) {
	s.refine.init()
	line, ok := refine.LineByKey(lineKey)
	if !ok {
		return nil, paramErr("线 %q 不认识,只有 ORE / HIDE / FIBER / WOOD", lineKey)
	}
	if base.Focus.Mode == refine.FocusCustom {
		return nil, paramErr("矩阵不支持自定义专注(custom):每一格的链不一样长")
	}
	base.Target = ""
	base = refine.Normalized(base)
	probe := base
	probe.Target = line.RefinedID(refine.MaxTier, refine.MaxEnchant)
	if err := s.checkRefine(probe); err != nil {
		return nil, err
	}
	key, err := json.Marshal(struct {
		Line string
		P    refine.Params
	}{line.Key, base})
	if err != nil {
		return nil, err
	}
	v, _, err := s.refine.matrix.get(ctx, string(key), func(c context.Context) (*RefineMatrix, error) {
		return s.computeMatrix(c, line, base)
	})
	return v, err
}

func (s *Service) computeMatrix(ctx context.Context, line refine.Line, base refine.Params) (*RefineMatrix, error) {
	now := s.refine.now().UTC()
	base.Now = now
	recipes := s.Recipes()
	targets := line.Targets()
	var items []string
	for _, t := range targets {
		its, err := refine.ItemsFor(recipes, t)
		if err != nil {
			return nil, err
		}
		items = append(items, its...)
	}
	items = uniqStrings(items)
	sort.Strings(items)
	keys := refineKeys(items, buyCities(base), targets, base.Sell.City)
	books, info, warns, err := s.refineBooks(ctx, keys, line, allCities(base), now, base.Sources.MaxAgeHours)
	if err != nil {
		return nil, err
	}
	vols, err := s.refineVolumes(ctx, targets, base.Sell.City, now)
	if err != nil {
		return nil, err
	}

	cells := make([]MatrixCell, len(targets))
	results := make([]*refine.Result, len(targets))
	// 25 次 Plan 互不相干,引擎是纯函数、只读 books 和配方表,并行算
	workers := min(runtime.GOMAXPROCS(0), len(targets))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				p := base
				p.Target = targets[i]
				res, err := refine.Plan(refine.Input{Params: p, Books: books, Recipes: recipes, Volume: vols[targets[i]]})
				cells[i] = matrixCell(targets[i], res, err)
				results[i] = res
			}
		}()
	}
	for i := range targets {
		select {
		case next <- i:
		case <-ctx.Done():
			close(next)
			wg.Wait()
			return nil, ctx.Err()
		}
	}
	close(next)
	wg.Wait()

	out := &RefineMatrix{
		Line: line.Key, City: base.City, Qty: base.Qty, ParamsUsed: base, Now: now,
		Unverified: []refine.UnverifiedUsed{}, Cells: cells, Books: info, Warnings: warns,
		RecipeSource: recipes.Source(), ServerDefaults: s.serverDefaults(),
	}
	if out.Warnings == nil {
		out.Warnings = []refine.Warning{}
	}
	// 未核实参数按最长的那条链(T8.4)列:25 格共用同一组参数
	if res := results[len(results)-1]; res != nil {
		out.Unverified = res.ParamsUsed.Unverified
	}
	return out, nil
}

func matrixCell(target string, res *refine.Result, err error) MatrixCell {
	_, tier, enchant, _ := refine.ParseItemID(target)
	c := MatrixCell{Target: target, Tier: tier, Enchant: enchant, Warnings: []refine.Warning{}}
	if err != nil {
		c.Error = err.Error()
		return c
	}
	c.Sell, c.Volume, c.Data = &res.Sell, &res.Volume, &res.Data
	c.Warnings = res.Warnings
	for _, cand := range res.Candidates {
		if cand.Reference != "" {
			continue
		}
		c.Candidates++
		if c.Best == nil {
			c.Best = &MatrixBest{
				ID: cand.ID, Label: cand.Label, Filled: cand.Filled,
				UnitCost: cand.UnitCost, TotalCost: cand.TotalCost, UpfrontCapital: cand.UpfrontCapital,
				Profit: cand.Profit, Focus: cand.Focus, SilverPerFocus: cand.SilverPerFocus, Daily: cand.Daily,
				OldestAgeH: cand.OldestAgeH, AgeLevel: cand.AgeLevel, Warnings: cand.Warnings,
			}
			for i := range cand.Breakeven {
				if cand.Breakeven[i].Item == res.MainRaw {
					be := cand.Breakeven[i]
					c.Best.MainRawBreakeven = &be
					break
				}
			}
		}
	}
	return c
}

// ── 配方树 ─────────────────────────────────────────────────────────────

// RefineRecipe 是 GET /api/refine/recipe:item 的配方树和转化格子。k、fee 为 nil 时用配置的默认值。
// 只读内存里的配方表,不读库、不打 AODP。
func (s *Service) RefineRecipe(item string, k, fee *float64) (*refine.Tree, error) {
	kk, ff := s.Cfg.Refining.TransmuteFactor, s.Cfg.Refining.TransmuteStationFeePer100
	if k != nil {
		kk = *k
	}
	if fee != nil {
		ff = *fee
	}
	t, err := refine.RecipeTree(s.Recipes(), item, kk, ff)
	if err != nil {
		return nil, &ParamError{Err: err}
	}
	return t, nil
}
