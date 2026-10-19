package flip

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/bm"
	"albion-guild/internal/book"
	"albion-guild/internal/conf"
	"albion-guild/internal/model"
	"albion-guild/internal/scan"
)

// 黑市页(docs/black-market.md):黑市买单 × 主城卖单,解成一张按这单总赚排的采购单。
//
// 每个盘口边用抓包还是 AODP,和扫描同一个择边函数(scan.PickCapture)。抓包带件数;
// AODP 只有最优价,件数按 1 件算、标成 Est(见 bm.Ask)。
//
// 要看哪些物品:窗口内自己抓到过黑市买单的,加上 AODP(快照和现取过的)里黑市买单够新的。
// 抓包里的黑市物品多半不在扫描清单里,快照没有它们的主城价,所以每次现取一遍 AODP
// (同一物品 bmAODPReuse 内不重复问),取回来的并进 s.fresh,查价页和机会页也用得上。

const (
	// DefaultBMMaxAge / DefaultCityMaxAge 是两侧的数据龄上限,先照 Albion Free Market 的数:
	// 黑市买单被别的倒爷吃得快,必须比主城卖单严。实际跑一段再按命中率调
	DefaultBMMaxAge   = 45 * time.Minute
	DefaultCityMaxAge = 180 * time.Minute
	// MaxBMAge 是两个参数允许的上限
	MaxBMAge = 24 * time.Hour

	bmMaxRows = 300
	// bmMaxFetch 是一次最多现取多少个物品的 AODP 价,多出来的这次用快照里的
	bmMaxFetch  = 300
	bmAODPReuse = 3 * time.Minute
	// bmAODPWait 是最多等 AODP 多久,到点就用手上已有的价算
	bmAODPWait = 8 * time.Second
)

// BlackMarketParams 是黑市页的参数。零值用默认。
type BlackMarketParams struct {
	BMMaxAge   time.Duration
	CityMaxAge time.Duration
	// Cities 是去哪买,空 = 配置里除黑市以外的全部城市
	Cities []string
}

// BlackMarketRow 是采购单的一行,带上物品名。
type BlackMarketRow struct {
	bm.Row
	Name string `json:"name"`
}

// BlackMarketPlan 是黑市页的结果。
type BlackMarketPlan struct {
	GeneratedAt   time.Time        `json:"generated_at"`
	Tax           float64          `json:"tax"`
	BMMaxAgeMin   int              `json:"bm_max_age_min"`
	CityMaxAgeMin int              `json:"city_max_age_min"`
	Cities        []string         `json:"cities"`
	Rows          []BlackMarketRow `json:"rows"`
	// Truncated 是超出 bmMaxRows、没列出来的行数
	Truncated int `json:"truncated,omitempty"`
	// Items 是黑市买单够新的物品数,CaptureItems 是其中黑市那边有自己抓包的
	Items        int `json:"items"`
	CaptureItems int `json:"capture_items"`
	// BMNewest 是自己抓到的黑市买单里最近的一眼:多久没去黑市翻了。没有就不输出
	BMNewest time.Time `json:"bm_newest,omitzero"`
	// Error 非空说明有一路数据没取到(读抓包失败、AODP 没回),结果是用剩下的算的
	Error string `json:"error,omitempty"`
}

func (p *BlackMarketPlan) addErr(err error) {
	if err == nil {
		return
	}
	if p.Error != "" {
		p.Error += "; "
	}
	p.Error += err.Error()
}

// bmState 记着每个物品最近一次为黑市页问 AODP 的时刻。零值可用
type bmState struct {
	mu    sync.Mutex
	asked map[string]time.Time
}

// due 挑出 reuse 之内没问过的物品,记成 now 问过;最多 limit 个
func (b *bmState) due(items []string, now time.Time, limit int) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.asked == nil {
		b.asked = map[string]time.Time{}
	}
	for k, at := range b.asked {
		if now.Sub(at) >= bmAODPReuse {
			delete(b.asked, k)
		}
	}
	var out []string
	for _, it := range items {
		if len(out) >= limit {
			break
		}
		if _, ok := b.asked[it]; !ok {
			b.asked[it] = now
			out = append(out, it)
		}
	}
	return out
}

// forget 撤掉 due 的记号:没问成,下次还要问
func (b *bmState) forget(items []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, it := range items {
		delete(b.asked, it)
	}
}

func (s *Service) bmParams(p BlackMarketParams) (BlackMarketParams, error) {
	if p.BMMaxAge == 0 {
		p.BMMaxAge = DefaultBMMaxAge
	}
	if p.CityMaxAge == 0 {
		p.CityMaxAge = DefaultCityMaxAge
	}
	if p.BMMaxAge < time.Minute || p.BMMaxAge > MaxBMAge || p.CityMaxAge < time.Minute || p.CityMaxAge > MaxBMAge {
		return p, paramErr("数据龄只能是 1 分钟到 %v", MaxBMAge)
	}
	var known []string
	for _, c := range s.Cfg.Cities {
		if c != conf.BlackMarket {
			known = append(known, c)
		}
	}
	if len(p.Cities) == 0 {
		p.Cities = known
		return p, nil
	}
	var cities []string
	for _, c := range p.Cities {
		if !slices.Contains(known, c) {
			return p, paramErr("不认识的城市 %q,可选 %v", c, known)
		}
		if !slices.Contains(cities, c) {
			cities = append(cities, c)
		}
	}
	p.Cities = cities
	return p, nil
}

// BlackMarket 算黑市页的采购单。参数不对返回 *ParamError;数据源出错不返回错误,
// 记在 Error 里、用剩下的数据算。
func (s *Service) BlackMarket(ctx context.Context, p BlackMarketParams) (*BlackMarketPlan, error) {
	p, err := s.bmParams(p)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	plan := &BlackMarketPlan{GeneratedAt: now, Tax: s.Cfg.Economics.MarketTax,
		BMMaxAgeMin: int(p.BMMaxAge / time.Minute), CityMaxAgeMin: int(p.CityMaxAge / time.Minute),
		Cities: p.Cities, Rows: []BlackMarketRow{}}
	bmCut, cityCut := now.Add(-p.BMMaxAge), now.Add(-p.CityMaxAge)

	// 一、自己抓的黑市买单
	books := map[model.QuoteKey]book.Side{}
	items := map[string]bool{}
	sb := s.storeBooks()
	conflictSince := now.Add(-s.Cfg.CaptureWindow())
	if sb != nil {
		got, err := s.bmCapturedBids(ctx, sb, bmCut, conflictSince)
		plan.addErr(err)
		for k, side := range got {
			books[k] = side
			items[k.ItemID] = true
			if side.Newest.After(plan.BMNewest) {
				plan.BMNewest = side.Newest
			}
		}
	}
	plan.CaptureItems = len(items)

	// 二、AODP:快照 ∪ 现取过的 ∪ 这次给抓包物品现取的。AODP 里黑市买单够新的物品也算进来
	prices, err := s.bmPrices(ctx, sortedKeys(items), p.Cities, now)
	plan.addErr(err)
	for k, r := range prices {
		if k.city == conf.BlackMarket && r.BuyPriceMax > 0 && r.BuyPriceMaxDate.Valid() && !r.BuyPriceMaxDate.T.Before(bmCut) {
			items[k.item] = true
		}
	}
	plan.Items = len(items)

	// 三、这些物品在各城的卖单
	if sb != nil && len(items) > 0 {
		var keys []model.QuoteKey
		for _, it := range sortedKeys(items) {
			for _, c := range p.Cities {
				for _, q := range scan.GameQualities {
					keys = append(keys, model.QuoteKey{ItemID: it, LocationID: c, Quality: int16(q), Side: model.SideOffer})
				}
			}
		}
		got, _, err := sb.readSides(ctx, keys, cityCut, conflictSince)
		if err != nil {
			plan.addErr(fmt.Errorf("读主城抓包: %w", err))
		}
		for k, side := range got {
			books[k] = side
		}
	}

	// 四、逐物品解分配
	prefer := s.Cfg.PreferSlack()
	tax := s.Cfg.Economics.MarketTax
	var rows []bm.Row
	for _, it := range sortedKeys(items) {
		var bids []bm.Bid
		var asks []bm.Ask
		for _, q := range scan.GameQualities {
			k := model.QuoteKey{ItemID: it, LocationID: conf.BlackMarket, Quality: int16(q), Side: model.SideRequest}
			r := prices[freshKey{it, conf.BlackMarket, q}]
			for _, l := range bmLevels(books[k], r.BuyPriceMax, r.BuyPriceMaxDate, bmCut, prefer, now) {
				bids = append(bids, bm.Bid{Quality: q, Price: l.price, Qty: l.qty, Seen: l.seen, Est: l.est})
			}
		}
		if len(bids) == 0 {
			continue
		}
		for _, c := range p.Cities {
			for _, q := range scan.GameQualities {
				k := model.QuoteKey{ItemID: it, LocationID: c, Quality: int16(q), Side: model.SideOffer}
				r := prices[freshKey{it, c, q}]
				for _, l := range bmLevels(books[k], r.SellPriceMin, r.SellPriceMinDate, cityCut, prefer, now) {
					asks = append(asks, bm.Ask{City: c, Quality: q, Price: l.price, Qty: l.qty, Seen: l.seen, Est: l.est})
				}
			}
		}
		rows = append(rows, bm.Plan(it, asks, bids, tax)...)
	}
	bm.SortRows(rows)
	if len(rows) > bmMaxRows {
		plan.Truncated = len(rows) - bmMaxRows
		rows = rows[:bmMaxRows]
	}
	cat := s.Catalog()
	for _, r := range rows {
		name := r.ItemID
		if cat != nil {
			name = cat.NameOf(r.ItemID)
		}
		plan.Rows = append(plan.Rows, BlackMarketRow{Row: r, Name: name})
	}
	return plan, nil
}

// bmCapturedBids 读窗口内自己抓到的黑市买单,按 (物品, 品质) 一边一份。
func (s *Service) bmCapturedBids(ctx context.Context, sb *storeBooks, since, conflictSince time.Time) (map[model.QuoteKey]book.Side, error) {
	listed, err := sb.st.CapturedItems(ctx, []string{conf.BlackMarket}, scan.GameQualities, since)
	if err != nil {
		return nil, fmt.Errorf("列黑市抓包: %w", err)
	}
	keys := make([]model.QuoteKey, 0, len(listed))
	for _, c := range listed {
		keys = append(keys, model.QuoteKey{ItemID: c.ItemID, LocationID: conf.BlackMarket,
			Quality: int16(c.Quality), Side: model.SideRequest})
	}
	got, _, err := sb.readSides(ctx, keys, since, conflictSince)
	if err != nil {
		return nil, fmt.Errorf("读黑市抓包: %w", err)
	}
	return got, nil
}

// bmPrices 是黑市页用的 AODP 当前价:快照里的、查价页和黑市页现取过的、这次给 items 现取的,
// 同一格逐字段新的胜(scan.NewerPrice,和查价页同一个并法)。现取失败不影响别的来源。
func (s *Service) bmPrices(ctx context.Context, items, cities []string, now time.Time) (map[freshKey]aodp.PriceRecord, error) {
	out := map[freshKey]aodp.PriceRecord{}
	add := func(r aodp.PriceRecord) {
		k := freshKey{r.ItemID, r.City, r.Quality}
		if e, ok := out[k]; ok {
			r, _ = scan.NewerPrice(e, r)
		}
		out[k] = r
	}
	var since time.Time
	if snap := s.snap.Load(); snap != nil {
		since = snap.FetchedAt
		for _, r := range snap.Prices {
			add(r)
		}
	}
	var err error
	if fetch := s.bmFetcher(); fetch != nil {
		if want := s.bm.due(items, now, bmMaxFetch); len(want) > 0 {
			fctx, cancel := context.WithTimeout(ctx, bmAODPWait)
			locs := append(slices.Clone(cities), conf.BlackMarket)
			recs, ferr := fetch(fctx, want, locs, scan.GameQualities)
			cancel()
			if ferr != nil {
				s.bm.forget(want)
				if errors.Is(ferr, context.DeadlineExceeded) {
					ferr = fmt.Errorf("等了 %v 没等到", bmAODPWait)
				}
				err = fmt.Errorf("现取 AODP: %w", ferr)
			}
			s.fresh.note(recs, now)
		}
	}
	for _, r := range s.fresh.since(since, "") {
		add(r)
	}
	return out, err
}

// bmFetcher 是现取 AODP 价的函数。测试用 bmFetch 换掉;没有 AODP 客户端时是 nil
func (s *Service) bmFetcher() func(ctx context.Context, items, cities []string, qualities []int) ([]aodp.PriceRecord, error) {
	if s.bmFetch != nil {
		return s.bmFetch
	}
	if s.aodp != nil {
		return s.aodp.FetchPrices
	}
	return nil
}

type bmLevel struct {
	price, qty int64
	seen       time.Time
	est        bool
}

// bmLevels 决定一个盘口边用抓包的档还是 AODP 的最优价,口径同扫描(scan.PickCapture)。
// 抓包只留 cut 之后看到的档;AODP 的价比 cut 旧就不用,只有一个价,件数按 1 件(est)。
// 超前于 now 的时间戳钳到 now(AODP 和客户端各有各的钟)
func bmLevels(side book.Side, aodpPx int64, aodpAt aodp.Stamp, cut time.Time, prefer time.Duration, now time.Time) []bmLevel {
	clamp := func(t time.Time) time.Time {
		if t.After(now) {
			return now
		}
		return t
	}
	if use, _ := scan.PickCapture(aodpPx, aodpAt, scan.CapturedFrom(side, 0), prefer); use {
		var out []bmLevel
		for _, l := range side.Levels {
			if l.Qty > 0 && l.Price > 0 && !l.Seen.Before(cut) {
				out = append(out, bmLevel{price: l.Price, qty: l.Qty, seen: clamp(l.Seen)})
			}
		}
		return out
	}
	if aodpPx > 0 && aodpAt.Valid() && !aodpAt.T.Before(cut) {
		return []bmLevel{{price: aodpPx, qty: 1, seen: clamp(aodpAt.T), est: true}}
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
