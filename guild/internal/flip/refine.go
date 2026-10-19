package flip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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
//  6. 1~5 是 I/O,按 (线, 城市) 预先建成快照(refine_snap.go);点击时只读快照、
//     按请求的 max_age_hours 合并成引擎的 Books,再跑引擎。同样的参数 + 同样的快照版本
//     60 秒内只算一次,并发的同样请求合并成一次(ttlCache)
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
	// refinePlanTTL:计算结果缓存 60 秒。键是 json(参数) + 用到的快照版本(refine_snap.go):
	// 数据没变时来回点同一批格子、切回看过的目标都秒回;数据一变版本就变,不会回旧的。
	// Now 不进键,价格年龄最多偏 60 秒,6h / 24h 的标线不受影响。
	// (以前是 2 秒、键里没有数据版本,每次点击几乎都未命中,读簿和 AODP 跟着重走一遍)
	refinePlanTTL = 60 * time.Second
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
	// fees 是抓到的工作站费率(refine_fees.go),按认多旧的窗口缓存 60 秒
	fees *ttlCache[[]store.StationFee]
	// clock 只给测试用:算价格年龄的「现在」和缓存过期都按它。nil = time.Now
	clock func() time.Time

	// 数据快照(refine_snap.go),都由 snapMu 管
	snapMu sync.Mutex
	snaps  map[snapKey]*refineSnap
	// building 是正在重建的快照:关上就是建完了(成功与否看 snaps / buildErr)
	building map[snapKey]chan struct{}
	buildErr map[snapKey]error
	// dirty 是入库标脏的时刻,比快照的读簿时刻新就要重建
	dirty map[snapKey]time.Time
	// active 是最近被请求用过的时刻,后台预热只管 refineWarmIdle 以内的
	active map[snapKey]time.Time
	// items 是快照覆盖的物品,按 配方表 × 线 缓存
	items map[string][]string
	ver   atomic.Uint64
	// kick 叫醒后台预热(容量 1,塞不进就算了)
	kick     chan struct{}
	warm     atomic.Bool
	warmLazy sync.Once
}

func (r *refineState) init() {
	r.once.Do(func() {
		r.plans = newTTLCache[*RefinePlan](refinePlanTTL, refineComputeTimeout)
		r.matrix = newTTLCache[*RefineMatrix](refinePlanTTL, 2*refineComputeTimeout)
		r.prices = newTTLCache[[]aodp.PriceRecord](refinePricesTTL, 2*time.Minute)
		r.fees = newTTLCache[[]store.StationFee](stationFeeCacheTTL, time.Minute)
		r.snaps, r.building, r.buildErr = map[snapKey]*refineSnap{}, map[snapKey]chan struct{}{}, map[snapKey]error{}
		r.dirty, r.active, r.items = map[snapKey]time.Time{}, map[snapKey]time.Time{}, map[string][]string{}
		r.kick = make(chan struct{}, 1)
		if r.clock != nil {
			r.plans.now, r.matrix.now, r.prices.now, r.fees.now = r.clock, r.clock, r.clock, r.clock
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
	// StationFee 说明精炼站费从哪来(你填的 / 抓包 / 配置)。计算结果是共享缓存,
	// 这一项由 API 层按这次请求填进副本(见 SettleStationFee)
	StationFee StationFeeInfo `json:"station_fee"`
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
	// Timing 是这份结果的耗时拆分(诉求 #1/#8 的埋点):卡在哪一段一眼看得出来
	Timing RefineTiming `json:"timing"`
}

// RefineTiming 是一次精炼计算的耗时。读簿 / AODP / 成交量是用到的快照建的时候花的
// (命中快照时那是之前花的,这次没花,看 Cache);引擎是这次算的。
type RefineTiming struct {
	// ReadMS 是 BookOrders + book.Build
	ReadMS float64 `json:"read_ms"`
	// AODPMS 是等 AODP 当前价;AODPWaited=false 表示没真去打(缓存命中、沿用上一份或已知连不上)
	AODPMS     float64 `json:"aodp_ms"`
	AODPWaited bool    `json:"aodp_waited"`
	VolumeMS   float64 `json:"volume_ms"`
	EngineMS   float64 `json:"engine_ms"`
	// WaitMS 是这次请求在等快照上花的时间(命中时是 0)
	WaitMS float64 `json:"wait_ms"`
	// SnapshotAgeS 是用到的快照里最旧的那份读了多久
	SnapshotAgeS float64 `json:"snapshot_age_s"`
	// Cache:hit 快照现成 | miss 这次现建的 | stale 要重建没等到、先用了旧的
	Cache string `json:"cache"`
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
	// Dropped 是按 AODP 新价剔掉的抓包档数(比新价更优的那几档,见 spliceAODP)
	Dropped int `json:"dropped"`
}

// 服务层加的警告代码(引擎自己的见 refine/plan.go)。
const (
	WarnAODPNewer       = "aodp_newer"       // 抓包阶梯之后 AODP 报了不同的价,已按新价修正阶梯顶部
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
	return s.RefineWith(ctx, p, RefineOpts{})
}

// RefineOpts 是计算之外的请求选项。
type RefineOpts struct {
	// After:客户端收到的那条 WS 推送的服务端推送时刻(model.Quote.Pushed)。用到的快照比它旧
	// 就重建(最多等 refineSnapWait),免得推送触发的重算拿到推送之前读的那份
	After time.Time
	// StationByTier / TransmuteByTier:矩阵按每格的 tier 换精炼站费 / 转化主人费率
	// (StationFeeInfo.ByTier)。站只能炼到本级为止,整张表都套能炼 T8 的那台会高估低 tier 的费用。
	// 只在这两项是抓包来的时候给;nil = 整张表用参数里那一个
	StationByTier   map[int]float64
	TransmuteByTier map[int]float64
}

// RefineWith 同 Refine,带请求选项。
func (s *Service) RefineWith(ctx context.Context, p refine.Params, o RefineOpts) (*RefinePlan, error) {
	s.refine.init()
	p = refine.Normalized(p)
	if err := s.checkRefine(p); err != nil {
		return nil, err
	}
	line, _ := refine.LineOfItem(p.Target)
	cities := allCities(p)
	snaps, use, err := s.ensureSnaps(ctx, line, cities, o.After)
	if err != nil {
		return nil, err
	}
	key, err := json.Marshal(p) // Now 是 json:"-",不进键
	if err != nil {
		return nil, err
	}
	v, _, err := s.refine.plans.get(ctx, string(key)+"|"+snapVers(snaps, cities), func(c context.Context) (*RefinePlan, error) {
		return s.computeRefine(c, p, snaps, use)
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

// computeRefine 是一次真正的计算,在缓存的后台 goroutine 里跑。不做 I/O:
// 数据都在 snaps 里(ensureSnaps 备好的),这里只按请求合并、跑引擎。
func (s *Service) computeRefine(_ context.Context, p refine.Params, snaps map[string]*refineSnap,
	use snapUse) (*RefinePlan, error) {

	now := s.refine.now().UTC()
	p.Now = now
	recipes := s.Recipes()
	items, err := refine.ItemsFor(recipes, p.Target)
	if err != nil {
		return nil, &ParamError{Err: err}
	}
	keys := refineKeys(items, buyCities(p), []string{p.Target}, p.Sell.City)
	books, info, warns := s.mergeBooks(keys, snaps, now, p.Sources.MaxAgeHours)
	t0 := time.Now()
	res, err := refine.Plan(refine.Input{Params: p, Books: books, Recipes: recipes,
		Volume: snaps[p.Sell.City].volume(p.Target, s.Cfg.Refining.VolumeDays)})
	if err != nil {
		return nil, err // 参数在 checkRefine 里校验过,到这里只能是内部问题(比如配方表是空的)
	}
	info.Timing = timingOf(snaps, use, now, time.Since(t0))
	logRefine("精炼计算", p.Target, p.City, info)
	res.Warnings = append(res.Warnings, warns...)
	return &RefinePlan{Result: res, Books: info, ServerDefaults: s.serverDefaults()}, nil
}

// logRefine 把耗时拆分打进日志:改动前后的对照、线上卡在哪一段都靠它。
func logRefine(what, target, city string, info RefineBooks) {
	t := info.Timing
	slog.Info(what, "target", target, "city", city, "cache", t.Cache, "wait_ms", t.WaitMS,
		"read_ms", t.ReadMS, "aodp_ms", t.AODPMS, "aodp_waited", t.AODPWaited, "volume_ms", t.VolumeMS,
		"engine_ms", t.EngineMS, "snapshot_age_s", t.SnapshotAgeS, "sides", info.Sides)
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

// StationFees:线上从库里读抓到的工作站费率(StationFeeSource,见 refine_fees.go)。
func (r serviceRefineSource) StationFees(ctx context.Context, since time.Time) ([]store.StationFee, error) {
	if r.s.Store == nil {
		return nil, nil // 没有库就是没抓到,按配置算;不当错误,免得每次请求都告警
	}
	return r.s.Store.StationFees(ctx, since)
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

// mergeBooks 把快照里的盘口和 AODP 按这次请求合并成引擎的输入。纯内存,不做 I/O:
// 读簿、取 AODP 在建快照时做(refine_snap.go)。max_age_hours 是请求参数,所以合并放在请求时。
//
// 每个盘口边按 scan.MergeSide 择边(扫描、查价页同一个函数),但有一处不同:
// MergeSide 因为 AODP 更新、价不同而选了 AODP 时,**不整边换成 AODP**,而是把 AODP 的新价
// 拼进抓包阶梯(spliceAODP)。精炼要沿阶梯吃深度,AODP 只有一个价、没有件数,整边换成它
// 等于假设那个价有无限件;可原样留着旧阶梯,顶上那几档可能早被买走了,算出来的利润是虚的
// (用户实测:查价页已经是新价,精炼页还按旧价算,算着赚、做了亏)。这种边照旧列进
// books.aodp_newer,带上剔掉了几档。
//
// 反过来,抓包这一边整个比 maxAgeHours(请求的 sources.max_age_hours)还旧、而 AODP 的价
// 在这个年龄以内时,用 AODP(深度未知):引擎会把太旧的抓包当缺价丢掉,留着它等于把
// 新的 AODP 价也挡在外面(审查 finding 8)。这种边列进 books.aodp_over_stale。
func (s *Service) mergeBooks(keys []model.QuoteKey, snaps map[string]*refineSnap, now time.Time,
	maxAgeHours float64) (refine.Books, RefineBooks, []refine.Warning) {

	window := s.Cfg.Refining.Window()
	info := RefineBooks{WindowHours: window.Hours(), ReadAt: now, Sides: len(keys),
		Conflicted: []string{}, AODPNewer: []AODPNewer{}, AODPOverStale: []string{}, AODPOK: true}
	if cc, ok := s.Conflicts.(conflictCoverage); ok {
		t := cc.ConflictCoverageSince(now).UTC()
		info.ConflictsSince = &t
	}
	// 读簿时刻按用到的快照里最旧的那份报:界面拿它和 WS 报价时刻比,早于报价就再要一次
	cities := make([]string, 0, len(snaps))
	for c := range snaps {
		cities = append(cities, c)
	}
	sort.Strings(cities)
	for _, c := range cities {
		sn := snaps[c]
		if sn.at.Before(info.ReadAt) {
			info.ReadAt = sn.at
		}
		if sn.aodpErr != nil && info.AODPOK {
			info.AODPOK, info.AODPError = false, aodpErrText(sn.aodpErr)
		}
	}
	var warns []refine.Warning
	if !info.AODPOK {
		warns = append(warns, refine.Warning{Code: WarnAODPUnavailable,
			Msg: "这次没取到 AODP 当前价(" + info.AODPError + "),抓包没有的盘口只能用之前取过的 AODP,没有的按缺价算"})
	}

	books := refine.Books{}
	for _, k := range keys {
		sn := snaps[k.LocationID]
		if sn == nil {
			info.Empty++ // 不该发生:快照按请求的城市建
			continue
		}
		pr := sn.prices[refineCell{k.ItemID, k.LocationID}]
		px, at := pr.SellPriceMin, pr.SellPriceMinDate
		if k.Side == model.SideRequest {
			px, at = pr.BuyPriceMax, pr.BuyPriceMaxDate
		}
		if !at.Valid() {
			px = 0 // 没时间戳的价查不了新鲜度,不用
		}
		side, has := sn.sides[k]
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
			if sn.flagged[k] {
				info.Conflicted = append(info.Conflicted, sideName(k))
			}
			if m.Superseded {
				capAge := now.Sub(side.Levels[0].Seen).Hours()
				aodpAt := at.T
				if aodpAt.After(now) {
					aodpAt = now
				}
				var dropped int
				l, dropped = spliceAODP(l, k.Side, px, aodpAt)
				info.AODPNewer = append(info.AODPNewer, AODPNewer{
					Item: k.ItemID, City: k.LocationID, Side: sideWord(k.Side),
					CapturePrice: side.Levels[0].Price, CaptureAgeH: round3(capAge),
					AODPPrice: px, AODPAgeH: round3(aodpAge), Dropped: dropped,
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
			Msg: fmt.Sprintf("%d 个盘口边在抓包之后 AODP 报了不同的最优价(%s):已按新价修正阶梯顶部,新价那档件数不知道、按 1 件算,见 books.aodp_newer",
				n, newerSample(info.AODPNewer))})
	}
	if n := len(info.Conflicted); n > 0 {
		warns = append(warns, refine.Warning{Code: WarnConflicted,
			Msg: fmt.Sprintf("%d 个盘口边最近卷进过多开串城,读簿时没剔幽灵单,可能多算了已经成交的挂单", n)})
	}
	return books, info, warns
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

// spliceAODP 把一个比抓包新、价又不同的 AODP 最优价拼进抓包阶梯。
//
// 挂单列表按价排序,AODP 的新最优价就是那一刻盘口的第一档:
//   - 抓包里比它更优的档已经没了(被吃掉或撤单),剔掉
//   - 和它同价的那档还在,件数照抓包的,观测时刻改成 AODP 的
//   - 它比抓包所有档都优(有人新挂了更好的单):放在最上面,件数不知道,按 1 件算,
//     再往下吃就是抓包的深度——不假设新价有无限件
//
// 抓包的档全被剔掉时,盘口只剩一个不知道件数的价,和 AODP 兜底同一个样子(DepthUnknown)。
// 返回拼好的阶梯和剔掉的档数。
func spliceAODP(l refine.Ladder, side model.Side, px int64, at time.Time) (refine.Ladder, int) {
	out := refine.Ladder{Seen: at, Truncated: l.Truncated, Source: "capture+aodp"}
	if l.Seen.After(at) {
		out.Seen = l.Seen
	}
	dropped := 0
	top := false // 抓包里有和新价同价的档
	for i, lv := range l.Levels {
		seen := l.Seen
		if len(l.LevelSeen) == len(l.Levels) && !l.LevelSeen[i].IsZero() {
			seen = l.LevelSeen[i]
		}
		switch {
		case book.Better(side, lv.Price, px):
			dropped++
			continue
		case lv.Price == px:
			top, seen = true, at
		}
		out.Levels = append(out.Levels, lv)
		out.LevelSeen = append(out.LevelSeen, seen)
	}
	if len(out.Levels) == 0 {
		return refine.Ladder{Levels: []depth.Level{{Price: px}}, Seen: at, Source: "aodp", DepthUnknown: true}, dropped
	}
	if !top {
		out.Levels = append([]depth.Level{{Price: px, Qty: 1}}, out.Levels...)
		out.LevelSeen = append([]time.Time{at}, out.LevelSeen...)
	}
	return out, dropped
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

// mergePrices 是 gridPrices 的多物品版本:这次取回的 ∪ 扫描快照里的 ∪ 查价页取过的,
// 逐字段新的胜(和查价格子、机会页重算同一个并法,见 aodp_fresh.go)。
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

// refineRecentDays 是挂卖价拿来对照的成交均价取最近几个完整自然日
const refineRecentDays = 7

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
	// 近几天的成交加权均价,和挂卖价比:差太多就是刚涨上去的价(refine.WarnPriceSpike)
	recentDays := min(refineRecentDays, days)
	recentFrom := last.AddDate(0, 0, -(recentDays - 1))
	type acc struct {
		sum     int64
		withQty int
		rows    int
		sources map[string]bool
		// 近 recentDays 天:成交额和件数
		recentAmt, recentQty float64
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
			if !r.Day.Before(recentFrom) && r.AvgPrice > 0 {
				a.recentAmt += float64(r.AvgPrice) * float64(r.ItemCount)
				a.recentQty += float64(r.ItemCount)
			}
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
			if a.recentQty > 0 {
				v.RecentAvg = math.Round(a.recentAmt / a.recentQty)
				v.RecentDays = recentDays
			}
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
	// StationFee 同 RefinePlan.StationFee
	StationFee StationFeeInfo `json:"station_fee"`
	// Keys 是这张表读了哪些盘口边(model.QuoteKey.String()):界面开着矩阵时订阅它们,
	// 这条线上任何一处有新抓包就重算,不只是当前目标的配方树
	Keys []string `json:"keys"`
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
	// NoFocus / WithFocus 是换一种专注开法再算一遍的最好那条,一格里就能比开不开专注:
	// 参数开着专注时给 NoFocus(不开专注),参数是不开专注时给 WithFocus(按「自动」开)
	NoFocus   *MatrixBest `json:"no_focus,omitempty"`
	WithFocus *MatrixBest `json:"with_focus,omitempty"`
	// Bid 是「挂买单收料」那条参照路线(和 Best 同一种专注开法)
	Bid *MatrixBest `json:"bid,omitempty"`
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
	return s.RefineMatrixWith(ctx, lineKey, base, RefineOpts{})
}

// RefineMatrixWith 同 RefineMatrix,带请求选项。
func (s *Service) RefineMatrixWith(ctx context.Context, lineKey string, base refine.Params, o RefineOpts) (*RefineMatrix, error) {
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
	cities := allCities(base)
	snaps, use, err := s.ensureSnaps(ctx, line, cities, o.After)
	if err != nil {
		return nil, err
	}
	key, err := json.Marshal(struct {
		Line               string
		P                  refine.Params
		Station, Transmute map[int]float64
	}{line.Key, base, o.StationByTier, o.TransmuteByTier})
	if err != nil {
		return nil, err
	}
	v, _, err := s.refine.matrix.get(ctx, string(key)+"|"+snapVers(snaps, cities), func(c context.Context) (*RefineMatrix, error) {
		return s.computeMatrix(c, line, base, snaps, use, o)
	})
	return v, err
}

func (s *Service) computeMatrix(ctx context.Context, line refine.Line, base refine.Params,
	snaps map[string]*refineSnap, use snapUse, o RefineOpts) (*RefineMatrix, error) {

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
	books, info, warns := s.mergeBooks(keys, snaps, now, base.Sources.MaxAgeHours)
	sellSnap := snaps[base.Sell.City]
	t0 := time.Now()

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
				if _, tier, _, ok := refine.ParseItemID(targets[i]); ok {
					// 换成抓到的费率就和卡片一样算核对过(SettleStationFee 对单个目标也这么补)。
					// Confirmed 先拷一份:base 的底层数组几个 goroutine 共用
					confirm := func(key string) {
						if !slices.Contains(p.Confirmed, key) {
							p.Confirmed = append(slices.Clone(p.Confirmed), key)
						}
					}
					if f, ok := o.StationByTier[tier]; ok {
						p.Fees.StationPer100 = f
						confirm("fees.station_per100")
					}
					if f, ok := o.TransmuteByTier[tier]; ok {
						p.Fees.TransmuteStationPer100 = f
						confirm("fees.transmute_station_per100")
					}
				}
				vol := sellSnap.volume(targets[i], s.Cfg.Refining.VolumeDays)
				res, err := refine.Plan(refine.Input{Params: p, Books: books, Recipes: recipes, Volume: vol})
				cells[i] = matrixCell(targets[i], res, err)
				results[i] = res
				if err != nil {
					continue
				}
				// 换一种专注开法再算一遍:用户想在一张表里看开不开专注各赚多少
				alt := p
				if p.Focus.Mode == refine.FocusOff {
					alt.Focus.Mode = refine.FocusAuto
				} else {
					alt.Focus.Mode = refine.FocusOff
				}
				if r2, err := refine.Plan(refine.Input{Params: alt, Books: books, Recipes: recipes, Volume: vol}); err == nil {
					b, _ := matrixBests(r2)
					if p.Focus.Mode == refine.FocusOff {
						cells[i].WithFocus = b
					} else {
						cells[i].NoFocus = b
					}
				}
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
	info.Timing = timingOf(snaps, use, now, time.Since(t0))
	logRefine("精炼矩阵", line.Key, base.City, info)

	out := &RefineMatrix{
		Line: line.Key, City: base.City, Qty: base.Qty, ParamsUsed: base, Now: now,
		Unverified: []refine.UnverifiedUsed{}, Cells: cells, Books: info, Warnings: warns,
		RecipeSource: recipes.Source(), ServerDefaults: s.serverDefaults(),
		Keys: make([]string, len(keys)),
	}
	for i, k := range keys {
		out.Keys[i] = k.String()
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
	c.Best, c.Bid = matrixBests(res)
	for _, cand := range res.Candidates {
		if cand.Reference == "" {
			c.Candidates++
		}
	}
	return c
}

// matrixBests 取一次计算里排第一的普通候选,和「挂买单收料」那条参照路线(没有就是 nil)。
func matrixBests(res *refine.Result) (best, bid *MatrixBest) {
	for i := range res.Candidates {
		cand := &res.Candidates[i]
		switch {
		case cand.Reference == "" && best == nil:
			best = matrixBestOf(cand, res.MainRaw)
		case cand.Reference == "bid" && bid == nil:
			bid = matrixBestOf(cand, res.MainRaw)
		}
	}
	return best, bid
}

func matrixBestOf(cand *refine.Candidate, mainRaw string) *MatrixBest {
	b := &MatrixBest{
		ID: cand.ID, Label: cand.Label, Filled: cand.Filled,
		UnitCost: cand.UnitCost, TotalCost: cand.TotalCost, UpfrontCapital: cand.UpfrontCapital,
		Profit: cand.Profit, Focus: cand.Focus, SilverPerFocus: cand.SilverPerFocus, Daily: cand.Daily,
		OldestAgeH: cand.OldestAgeH, AgeLevel: cand.AgeLevel, Warnings: cand.Warnings,
	}
	for i := range cand.Breakeven {
		if cand.Breakeven[i].Item == mainRaw {
			be := cand.Breakeven[i]
			b.MainRawBreakeven = &be
			break
		}
	}
	return b
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
