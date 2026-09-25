package scan

import (
	"context"
	"fmt"
	"sort"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/histagg"
	"albion-guild/internal/model"
	"albion-guild/internal/screen"
)

// CapturedSide 是自建抓包里一个盘口边剔除幽灵单后的阶梯。
// 字段和 store.BookSide 一一对应,换了个不依赖 store 的形状。
type CapturedSide struct {
	// Levels 从优到劣,LevelSeen[i] 是第 i 档最近一次被看到的时刻
	Levels    []depth.Level
	LevelSeen []time.Time
	// Newest 是这一边的"最近一眼"
	Newest time.Time
	// QtyTotal、LevelCount 不受档数截断;LevelCount > len(Levels) 说明截断了
	QtyTotal   int64
	LevelCount int
	Ghosts     int
	// Conflicted 说明这个盘口最近卷进过多开串城,读簿时暂停了幽灵剔除
	// (最近一眼可能是错归的单,拿它当权威会把真实挂单剔掉)
	Conflicted bool
}

// best 是第一档有货的下标,没有就是 -1。
func (cs CapturedSide) best() int {
	for i, l := range cs.Levels {
		if l.Qty > 0 && l.Price > 0 {
			return i
		}
	}
	return -1
}

// seenAt 是第 i 档的观测时刻。LevelSeen 缺了就退回整边的最近一眼——
// 只可能偏新,所以只在构造出错时兜底,不该依赖它
func (cs CapturedSide) seenAt(i int) time.Time {
	if i >= 0 && i < len(cs.LevelSeen) && !cs.LevelSeen[i].IsZero() {
		return cs.LevelSeen[i]
	}
	return cs.Newest
}

// CapturedItem 是抓包窗口里有挂单的一个物品,Qty 是件数合计。
type CapturedItem struct {
	ItemID string
	Qty    int64
}

// BookSource 给扫描提供抓包数据。仿照 hub.QuoteSource:scan 不 import store,
// 测试可以直接喂假数据。
type BookSource interface {
	// CaptureBooks 读一批盘口边。请求了但窗口内没有挂单的 key 不出现在结果里,
	// 当成"没有抓包"
	CaptureBooks(ctx context.Context, keys []model.QuoteKey, since time.Time) (map[model.QuoteKey]CapturedSide, error)
	// CapturedItems 列出 since 之后在 cities × qualities 里有挂单的物品,件数从多到少
	CapturedItems(ctx context.Context, cities []string, qualities []int, since time.Time) ([]CapturedItem, error)
}

// CaptureSummary 是一次评估里抓包参与了多少。
type CaptureSummary struct {
	// Enabled 是这次评估有没有打开融合(capture.enabled 且有读簿来源)
	Enabled bool `json:"enabled"`
	// RequestedKeys 是向库里要了多少个盘口边
	RequestedKeys int `json:"requested_keys"`
	// BookSides 是其中窗口内真有抓包挂单的边数
	BookSides int `json:"book_sides"`
	// AsksUsed/BidsUsed 是融合后卖单簿、买单簿用了抓包价的边数
	AsksUsed int `json:"asks_used"`
	BidsUsed int `json:"bids_used"`
	// Superseded 是抓包有数据、但 AODP 更新而且价不同、于是 AODP 胜出的边数
	Superseded int `json:"superseded"`
	// Synthesized 是 AODP 没返回、只靠抓包补出来的 (物品, 城市, 品质) 格数
	Synthesized int `json:"synthesized"`
	// Ghosts 是读簿时剔掉的幽灵单张数
	Ghosts int `json:"ghosts"`
	// ConflictKeys 是因为最近有多开串城、暂停了幽灵剔除的盘口边数
	ConflictKeys int `json:"conflict_keys"`
	// ExtraItems 是配置清单外、因为抓包窗口里有挂单才并进扫描的物品数;
	// ExtraDropped 是超出 capture.max_extra_items、按件数截掉的个数
	ExtraItems   int `json:"extra_items"`
	ExtraDropped int `json:"extra_dropped"`
	// ExtraPending 是全量之后新抓到、还在等补拉 AODP 的物品数(补拉有节流)。
	// 它们这一轮还不在机会板上
	ExtraPending int `json:"extra_pending"`
	// BackfilledAt 是最近一次给新抓到的物品补拉 AODP 的时刻,没补过不输出
	BackfilledAt time.Time `json:"backfilled_at,omitzero"`
	// Error 非空说明读抓包失败(读簿失败时这次评估退回了纯 AODP;
	// 列抓包物品失败时只扫配置清单)。几处错误用 "; " 连起来
	Error string `json:"error,omitempty"`
}

// AddError 把一处错误追加进 Error,nil 忽略。
func (s *CaptureSummary) AddError(err error) {
	if err == nil {
		return
	}
	if s.Error != "" {
		s.Error += "; "
	}
	s.Error += err.Error()
}

// extraItems 从抓到的物品里挑出要并进扫描的:不在 base 里、目录里查得到,
// 按件数从多到少(同件数按 id)截到 limit 个。
//
// 目录里查不到的跳过:多半是游戏更新后的新物品还没同步目录,或者客户端
// 解析出了怪 id,拿去问 AODP 只会白占 URL 预算。
func extraItems(captured []CapturedItem, base []string, cat *catalog.Catalog,
	limit int) (extra []string, dropped, unknown int) {
	if limit <= 0 {
		return nil, 0, 0
	}
	have := make(map[string]bool, len(base)+len(captured))
	for _, id := range base {
		have[id] = true
	}
	sorted := append([]CapturedItem(nil), captured...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Qty != sorted[j].Qty {
			return sorted[i].Qty > sorted[j].Qty
		}
		return sorted[i].ItemID < sorted[j].ItemID
	})
	for _, c := range sorted {
		if c.ItemID == "" || have[c.ItemID] {
			continue
		}
		have[c.ItemID] = true
		if cat != nil {
			if _, ok := cat.Get(c.ItemID); !ok {
				unknown++
				continue
			}
		}
		if len(extra) >= limit {
			dropped++
			continue
		}
		extra = append(extra, c.ItemID)
	}
	return extra, dropped, unknown
}

// captureKeys 拼出要读的盘口边。城市只取 cfg.Cities:3003(黑市)、没收敛成
// 城市名的原始地点 id 都进不来,和 AODP 那边的口径一致。Brecilien 在默认城市里
// (抓包的 5003 已收敛成这个名字),不在 cfg.Cities 里时同样进不来。
func captureKeys(itemIDs, cities []string, qualities []int) []model.QuoteKey {
	keys := make([]model.QuoteKey, 0, len(itemIDs)*len(cities)*len(qualities)*2)
	for _, item := range itemIDs {
		for _, city := range cities {
			for _, q := range qualities {
				for _, side := range []model.Side{model.SideOffer, model.SideRequest} {
					keys = append(keys, model.QuoteKey{
						ItemID: item, LocationID: city, Quality: int16(q), Side: side,
					})
				}
			}
		}
	}
	return keys
}

// PickCapture 决定一个盘口边用抓包还是 AODP。扫描和查价页共用这一个函数,
// 两边的口径才不会漂。规则按顺序:
//
//  1. 抓包这一边没数据 → AODP
//  2. AODP 这一边没价或没时间戳 → 抓包
//  3. 抓包最优档不比 AODP 旧 preferSlack 以上(含抓包更新)→ 抓包
//  4. 两边最优价相同 → 抓包(时间戳由调用方取两者中较新的)
//  5. 其余 → AODP,记一次 superseded
//
// 为什么不是"抓包无条件优先":两小时前的便宜卖单可能早被买走了,
// AODP 五分钟前已经报了新价,还拿旧抓包去盖,就是在造幽灵机会。
// 新鲜度不能因为来源更"高级"就倒退。
func PickCapture(aodpPx int64, aodpAt aodp.Stamp, cs CapturedSide, preferSlack time.Duration) (use, superseded bool) {
	bi := cs.best()
	if bi < 0 {
		return false, false
	}
	if aodpPx <= 0 || !aodpAt.Valid() {
		return true, false
	}
	if aodpAt.T.Sub(cs.seenAt(bi)) <= preferSlack {
		return true, false
	}
	if cs.Levels[bi].Price == aodpPx {
		return true, false
	}
	return false, true
}

// Merged 是一个盘口边融合后的结果。
type Merged struct {
	// Price/At 是要写回 PriceRecord 的价和时间戳
	Price int64
	At    aodp.Stamp
	Side  screen.Side
	// UsedCapture 为 true 时 Price/At 来自抓包
	UsedCapture bool
	Superseded  bool
}

// MergeSide 融合一个盘口边:按 PickCapture 选来源,选中抓包时带上深度统计,
// 落选的那个来源记成 Alt。导出给查价页复用,和扫描同一套口径。
//
// 抓包的观测时刻钳到 now:读簿发生在扫描的 now 之后,不钳的话
// 刚翻过的挂单会被 screen 的零容差判成 future_timestamp。
func MergeSide(aodpPx int64, aodpAt aodp.Stamp, cs CapturedSide, cfg conf.Config, now time.Time) Merged {
	m := Merged{Price: aodpPx, At: aodpAt}
	if aodpPx > 0 {
		m.Side = screen.Side{Source: screen.SourceAODP, Price: aodpPx, AgeHours: stampAge(aodpAt, now)}
	}
	bi := cs.best()
	if bi < 0 {
		return m
	}
	capPx := cs.Levels[bi].Price
	seen := cs.seenAt(bi)
	if seen.After(now) {
		seen = now
	}

	use, superseded := PickCapture(aodpPx, aodpAt, cs, cfg.PreferSlack())
	if !use {
		m.Superseded = superseded
		m.Side.Alt = &screen.AltQuote{Source: screen.SourceCapture, Price: capPx, AgeHours: now.Sub(seen).Hours()}
		return m
	}

	// 同价时时间戳取两者中较新的:两个来源说的是同一个价,较新的那个
	// 证明它到那时还在。但不采信超前于 now 的 AODP 时间戳——那是它自己的钟
	at := seen
	if capPx == aodpPx && aodpAt.Valid() && aodpAt.T.After(at) && !aodpAt.T.After(now) {
		at = aodpAt.T
	}
	side := screen.Side{Source: screen.SourceCapture, Price: capPx, AgeHours: now.Sub(at).Hours()}
	if aodpPx > 0 {
		side.Alt = &screen.AltQuote{Source: screen.SourceAODP, Price: aodpPx, AgeHours: stampAge(aodpAt, now)}
	}

	// 深度只信最近的档:幽灵单规则只验证得了"最近一眼"覆盖到的那段价,
	// 更旧的档可能早就没了。最优档本身都太旧的话,深度整个不参与判定
	cutoff := now.Add(-cfg.DepthWindow())
	if seen.Before(cutoff) {
		side.Note = fmt.Sprintf("深度快照 %.1fh 前,未参与判定", now.Sub(seen).Hours())
	} else {
		var lv []depth.Level
		for i, l := range cs.Levels {
			if l.Qty > 0 && l.Price > 0 && !cs.seenAt(i).Before(cutoff) {
				lv = append(lv, l)
			}
		}
		sup := depth.Analyze(lv, cfg.Filters.NearPct)
		// 合计沿用读簿给的全量(含窗口外、被截断的档),只当参考
		sup.QtyTotal = cs.QtyTotal
		side.Depth = &screen.DepthView{
			Support: sup,
			// 截断了、而读到的档又全在近价窗口内:近价件数只是下限
			Truncated: cs.LevelCount > len(cs.Levels) && sup.LevelsNear == len(lv),
		}
		side.Levels = lv
	}

	m.Price, m.At, m.Side, m.UsedCapture = capPx, aodp.Stamp{T: at}, side, true
	return m
}

func stampAge(s aodp.Stamp, now time.Time) float64 {
	age, _ := s.AgeHours(now)
	return age
}

// cell 是融合的粒度:(物品, 城市, 品质)。
type cell struct {
	item, city string
	quality    int
}

// overlay 把抓包盘口逐边盖到 AODP 快照上,返回新的价格表(输入不动)、
// 每格两边的来源说明,以及汇总计数。
//
// 只改 SellPriceMin/BuyPriceMax 和它们的时间戳,SellPriceMax/BuyPriceMin 不动。
// 抓包有、AODP 没返回的格子各合成一条零值行(按 key 排序,结果确定):
// 不依赖"AODP 对每个组合都回一行"这个没核实过的前提。
func overlay(prices []aodp.PriceRecord, books map[model.QuoteKey]CapturedSide,
	cfg conf.Config, now time.Time) ([]aodp.PriceRecord, map[histagg.QualityKey]screen.Sides, CaptureSummary) {

	var sum CaptureSummary
	if len(books) == 0 {
		return prices, nil, sum
	}

	cities := map[string]bool{}
	for _, c := range cfg.Cities {
		cities[c] = true
	}
	qualities := map[int]bool{}
	for _, q := range cfg.Qualities {
		qualities[q] = true
	}

	out := append([]aodp.PriceRecord(nil), prices...)
	index := make(map[cell]int, len(out))
	for i, rec := range out {
		k := cell{rec.ItemID, rec.City, rec.Quality}
		if _, dup := index[k]; !dup {
			index[k] = i
		}
	}

	// 有抓包的格子。读簿来源理应只返回请求过的 key,这里再按配置过一遍,
	// 防的是别的来源塞进黑市或别的品质
	var cells []cell
	seen := map[cell]bool{}
	for k, cs := range books {
		if !cities[k.LocationID] || !qualities[int(k.Quality)] {
			continue
		}
		if cs.best() >= 0 {
			sum.BookSides++
		}
		sum.Ghosts += cs.Ghosts
		if cs.Conflicted {
			sum.ConflictKeys++
		}
		c := cell{k.ItemID, k.LocationID, int(k.Quality)}
		if !seen[c] {
			seen[c] = true
			cells = append(cells, c)
		}
	}
	sort.Slice(cells, func(i, j int) bool {
		a, b := cells[i], cells[j]
		if a.item != b.item {
			return a.item < b.item
		}
		if a.city != b.city {
			return a.city < b.city
		}
		return a.quality < b.quality
	})

	sides := make(map[histagg.QualityKey]screen.Sides, len(cells))
	for _, c := range cells {
		askKey := model.QuoteKey{ItemID: c.item, LocationID: c.city, Quality: int16(c.quality), Side: model.SideOffer}
		bidKey := askKey
		bidKey.Side = model.SideRequest
		ask, bid := books[askKey], books[bidKey]
		if ask.best() < 0 && bid.best() < 0 {
			continue
		}

		i, ok := index[c]
		if !ok {
			out = append(out, aodp.PriceRecord{ItemID: c.item, City: c.city, Quality: c.quality})
			i = len(out) - 1
			index[c] = i
			sum.Synthesized++
		}
		rec := &out[i]

		ma := MergeSide(rec.SellPriceMin, rec.SellPriceMinDate, ask, cfg, now)
		if ma.UsedCapture {
			rec.SellPriceMin, rec.SellPriceMinDate = ma.Price, ma.At
			sum.AsksUsed++
		}
		mb := MergeSide(rec.BuyPriceMax, rec.BuyPriceMaxDate, bid, cfg, now)
		if mb.UsedCapture {
			rec.BuyPriceMax, rec.BuyPriceMaxDate = mb.Price, mb.At
			sum.BidsUsed++
		}
		if ma.Superseded {
			sum.Superseded++
		}
		if mb.Superseded {
			sum.Superseded++
		}
		sides[histagg.QualityKey{ItemID: c.item, City: c.city, Quality: c.quality}] =
			screen.Sides{Ask: ma.Side, Bid: mb.Side}
	}
	return out, sides, sum
}
