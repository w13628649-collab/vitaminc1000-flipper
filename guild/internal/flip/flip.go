// Package flip 把目录、扫描和榜单串成一个服务,给 HTTP 层调用。
//
// 这些能力原本在第一阶段的 Python 单机版里,搬过来之后数据落 PostgreSQL,
// 全公会共用一份,而不是每个人本地一个 SQLite。
package flip

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/model"
	"albion-guild/internal/portfolio"
	"albion-guild/internal/rank"
	"albion-guild/internal/scan"
	"albion-guild/internal/store"
)

type Service struct {
	Store *store.Store
	Cfg   conf.Config
	// Conflicts 是 ingest 那边的串城记录,扫描读簿时对最近串过城的盘口
	// 暂停幽灵剔除。可以不设:不设就不做这层处理
	Conflicts ConflictSource

	cat      atomic.Pointer[catalog.Catalog]
	last     atomic.Pointer[scan.Result]
	scanning atomic.Bool
	icons    *http.Client
	// aodp 是**共用一个**。限流器的状态在 Client 里,每次调用新建一个
	// 就等于各限各的,几个查价请求并撞上定时扫描,合起来直接击穿
	// AODP 那 300 次/5 分钟的配额
	aodp *aodp.Client
}

func New(st *store.Store, cfg conf.Config) *Service {
	return &Service{
		Store: st, Cfg: cfg,
		icons: &http.Client{Timeout: 30 * time.Second},
		aodp:  aodp.New(cfg.BaseURL(), cfg.API),
	}
}

// Catalog 可能是 nil——目录还没同步过的时候。调用方要判。
func (s *Service) Catalog() *catalog.Catalog { return s.cat.Load() }

// LastScan 是最近一次扫描结果,没扫过是 nil。
func (s *Service) LastScan() *scan.Result { return s.last.Load() }

// LoadCatalog 从库里读目录。库是空的就去上游同步一次。
func (s *Service) LoadCatalog(ctx context.Context) error {
	items, cats, syncedAt, err := s.Store.LoadCatalog(ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		slog.Info("物品目录是空的,去上游同步")
		return s.SyncCatalog(ctx)
	}
	s.cat.Store(catalog.New(items, cats, syncedAt))
	slog.Info("物品目录已载入", "items", len(items), "synced_at", syncedAt)
	return nil
}

// SyncCatalog 从 ao-bin-dumps 拉一份新的写库,再换进内存。
func (s *Service) SyncCatalog(ctx context.Context) error {
	items, cats, err := catalog.Fetch(ctx)
	if err != nil {
		return err
	}
	if err := s.Store.SaveCatalog(ctx, items, cats); err != nil {
		return err
	}
	s.cat.Store(catalog.New(items, cats, time.Now().UTC().Format(time.RFC3339)))
	slog.Info("物品目录已同步", "items", len(items))
	return nil
}

// Scan 跑一次扫描,顺手把拉回来的成交历史存进库。
//
// 存历史这一步很重要:AODP 的 history 只给最近一段,自己存就能越攒越长,
// 跑上几个月手里就有一份上游给不了的长历史。
func (s *Service) Scan(ctx context.Context) (*scan.Result, error) {
	cat := s.cat.Load()
	if cat == nil {
		return nil, fmt.Errorf("物品目录还没同步,先调用 /api/catalog/sync")
	}
	if !s.scanning.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("已经有一次扫描在跑了")
	}
	defer s.scanning.Store(false)

	res, err := scan.RunWithCapture(ctx, s.aodp, s.Cfg, cat, s.books(), time.Now().UTC())
	if err != nil {
		return nil, err
	}
	s.last.Store(res)

	if n, err := s.Store.WriteHistory(ctx, res.History, store.SourceAODP); err != nil {
		// 历史没存上不该让整次扫描白跑,报出来继续
		slog.Warn("成交历史入库失败", "err", err)
	} else {
		slog.Info("成交历史已入库", "rows", n)
	}
	return res, nil
}

// Run 按间隔自动扫描。第一次立刻跑,这样服务端起来就有数据。
func (s *Service) Run(ctx context.Context, interval time.Duration) {
	scanOnce := func() {
		start := time.Now()
		res, err := s.Scan(ctx)
		if err != nil {
			slog.Warn("定时扫描失败", "err", err)
			return
		}
		slog.Info("定时扫描完成", "机会", len(res.Opportunities),
			"拒绝", len(res.Rejected), "耗时", time.Since(start).Round(time.Second))
	}
	scanOnce()

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			scanOnce()
		}
	}
}

// Rank 出榜单。
func (s *Service) Rank(ctx context.Context, opt rank.Options, quality int,
	category, subcategory string) ([]rank.Row, error) {

	cat := s.cat.Load()
	if cat != nil {
		opt.Names = cat
		if category != "" || subcategory != "" {
			wanted := map[string]struct{}{}
			for _, item := range cat.Browse(catalog.BrowseQuery{
				Category: category, Subcategory: subcategory, Enchantment: -1,
			}) {
				wanted[item.ItemID] = struct{}{}
			}
			opt.Wanted = wanted
		}
	}
	first, last := rank.Window(time.Now().UTC(), opt.WindowDays)
	daily, err := s.Store.DailyHistory(ctx, first, last, quality, opt.City)
	if err != nil {
		return nil, err
	}
	rows := rank.Aggregate(daily, opt)
	if rows == nil {
		rows = []rank.Row{}
	}
	return rows, nil
}

// Icon 先查库,没有就去 render 站拉一次再存进去。
//
// 直接让浏览器打 render.albiononline.com,翻一页分类就是上百个并发请求,
// 会被对面限流,列表里一半图标是空的。
func (s *Service) Icon(ctx context.Context, itemID string) ([]byte, error) {
	png, cached, err := s.Store.Icon(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if cached {
		return png, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		fmt.Sprintf(catalog.IconURL, itemID), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.icons.Do(req)
	if err != nil {
		return nil, nil // 网络抖动别写进库,下次还有机会
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = s.Store.SaveIcon(ctx, itemID, nil) // 上游确实没有,记下来别每次都问
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil
	}
	if err := s.Store.SaveIcon(ctx, itemID, body); err != nil {
		slog.Warn("图标入库失败", "item", itemID, "err", err)
	}
	return body, nil
}

// LookupRow 是查价页的一行:某物品在某城某品质的盘口。
type LookupRow struct {
	City    string `json:"city"`
	Quality int    `json:"quality"`
	SellMin int64  `json:"sell_min"`
	// SellQty/BuyQty 是最优价上的挂单件数。**只有抓包拿得到**,
	// AODP 的 prices 端点不返回数量——这正是 troll 挂单能骗人的原因。
	SellQty  int64   `json:"sell_qty"`
	BuyMax   int64   `json:"buy_max"`
	BuyQty   int64   `json:"buy_qty"`
	Source   string  `json:"source"`
	AgeHours float64 `json:"age_hours"`
}

type LookupResult struct {
	Item catalog.Item `json:"item"`
	Rows []LookupRow  `json:"rows"`
}

// Lookup 查一个物品在所有城市、所有品质的价格。
//
// 抓包数据优先:它带挂单数量,能直接看出"最低价只有 1 件"这种陷阱。
// 没有抓包覆盖的格子用 AODP 兜底。
func (s *Service) Lookup(ctx context.Context, itemID string, fresh time.Duration) (*LookupResult, error) {
	out := &LookupResult{Item: catalog.Item{ItemID: itemID}}
	if cat := s.cat.Load(); cat != nil {
		if item, ok := cat.Get(itemID); ok {
			out.Item = item
		}
	}

	type cell struct {
		city    string
		quality int
	}
	rows := map[cell]LookupRow{}

	captured, err := s.Store.QuotesByCity(ctx, itemID, fresh)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, q := range captured {
		age := 0.0
		if t, err := time.Parse(time.RFC3339, q.LastSeen); err == nil {
			age = now.Sub(t).Hours()
		}
		rows[cell{q.City, q.Quality}] = LookupRow{
			City: q.City, Quality: q.Quality,
			SellMin: q.SellMin, SellQty: q.SellQty,
			BuyMax: q.BuyMax, BuyQty: q.BuyQty,
			Source: "capture", AgeHours: age,
		}
	}

	// AODP 兜底。一个物品 × 所有城市 × 五档品质,一次请求就够
	prices, err := s.aodp.FetchPrices(ctx, []string{itemID}, s.Cfg.Cities, []int{1, 2, 3, 4, 5})
	if err != nil {
		slog.Warn("AODP 兜底查价失败", "item", itemID, "err", err)
	}
	for _, p := range prices {
		k := cell{p.City, p.Quality}
		if _, covered := rows[k]; covered {
			continue // 抓包已经覆盖,别用没有数量的数据盖掉有数量的
		}
		if p.SellPriceMin == 0 && p.BuyPriceMax == 0 {
			continue
		}
		age := 0.0
		if a, ok := p.SellPriceMinDate.AgeHours(now); ok {
			age = a
		}
		if a, ok := p.BuyPriceMaxDate.AgeHours(now); ok && a > age {
			age = a
		}
		rows[k] = LookupRow{
			City: p.City, Quality: p.Quality,
			SellMin: p.SellPriceMin, BuyMax: p.BuyPriceMax,
			Source: "aodp", AgeHours: age,
		}
	}

	out.Rows = make([]LookupRow, 0, len(rows))
	for _, r := range rows {
		out.Rows = append(out.Rows, r)
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		if out.Rows[i].City != out.Rows[j].City {
			return out.Rows[i].City < out.Rows[j].City
		}
		return out.Rows[i].Quality < out.Rows[j].Quality
	})
	return out, nil
}

// Portfolio 把同城机会和跨城路线放进同一个池子做资金分配。
//
// 两种玩法竞争的是**同一笔本金**,分开排名没有意义:
// 一条跨城路线可能比排第三的同城机会值得投,但如果它们各排各的,
// 用户根本看不出来。
func (s *Service) Portfolio(opt portfolio.Options) portfolio.Plan {
	res := s.last.Load()
	if res == nil {
		return portfolio.Plan{Note: "还没扫过"}
	}

	absorb := s.Cfg.Sizing.AbsorbRatio
	// 流动性桶的键是 (物品, 城市, 品质)。同一个桶里的货只有那么多,
	// 谁先排上谁先吃——不这么记的话,从同一座城发往三个方向的路线
	// 会各吃一份完整额度,累计收益里三分之二是同一批货
	poolKey := func(itemID, city string, quality int) string {
		return itemID + "|" + city + "|" + strconv.Itoa(quality)
	}

	var pool []portfolio.Candidate
	for _, o := range res.Opportunities {
		if o.AbsorbableQty < 1 {
			continue
		}
		// 同城买卖两端在同一个市场里,只占一个桶
		pool = append(pool, portfolio.Candidate{
			Key:         "flip:" + o.ItemID + "|" + o.City,
			Label:       o.ItemName + " · " + o.City,
			Kind:        "flip",
			CostPerUnit: o.CostPerUnit, ProfitPerUnit: o.ProfitPerUnit,
			HoursPerRound: o.HoursPerTurn,
			Pools: []portfolio.Pool{
				{Key: poolKey(o.ItemID, o.City, o.Quality), Capacity: o.AbsorbableQty},
			},
			Volatility: o.Volatility,
			Payload:    o,
		})
	}
	for _, r := range res.Routes {
		if r.Qty < 1 {
			continue
		}
		// 跨城要占产地和销地两个桶:进得去还得出得来
		pool = append(pool, portfolio.Candidate{
			Key:         "arb:" + r.ItemID + "|" + r.FromCity + "->" + r.ToCity,
			Label:       r.ItemName + " · " + r.FromCity + " → " + r.ToCity,
			Kind:        "arb",
			CostPerUnit: r.CostPerUnit, ProfitPerUnit: r.ProfitPerUnit,
			HoursPerRound: r.HoursPerTrip,
			Pools: []portfolio.Pool{
				{Key: poolKey(r.ItemID, r.FromCity, r.Quality), Capacity: r.SourceDaily * absorb},
				{Key: poolKey(r.ItemID, r.ToCity, r.Quality), Capacity: r.DestDaily * absorb},
			},
			Volatility: r.Volatility,
			Payload:    r,
		})
	}
	return portfolio.Build(pool, opt)
}

// Depth 走一遍盘口,算出要这么多货的真实成交均价。
//
// 这是抓包数据独有的能力,AODP 给不了——它只有价格没有数量。
func (s *Service) Depth(ctx context.Context, k model.QuoteKey, want int64,
	fresh time.Duration) (depth.Fill, error) {

	levels, err := s.Store.Book(ctx, k, fresh, 200)
	if err != nil {
		return depth.Fill{}, err
	}
	book := make([]depth.Level, 0, len(levels))
	for _, l := range levels {
		book = append(book, depth.Level{Price: l.Price, Qty: l.Depth})
	}
	return depth.Walk(book, want), nil
}
