package flip

import (
	"sync"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/scan"
)

// 机会页的卡片和查价页的面板喂给 scan.PickCapture 的 AODP 当前价要是同一份。
//
// 以前不是:卡片是定时重算拿全量扫描那份快照(-scan 默认 30 分钟一次)算的,
// 面板每次现取 AODP(缓存 60 秒)。残单规则和择边函数已经是同一个,输入的 AODP 价和
// 时间戳不同,照样选出不同的来源 —— 实测全量后 13 分钟,T6_METALBAR @ Thetford 买方
// 卡片用抓包 2634(快照里 AODP 2700、9.8 小时前),面板用 AODP 2701(现取的、4 分钟前)。
//
// 现在两边都用"快照 ∪ 查价页现取过的"里逐字段时间戳较新的那个(scan.NewerPrice):
// 查价格子取价时把快照并进来(gridPrices),重算和全量评估时把查价页取回的并进快照
// (evalSnapshot)。查价之后卡片在下一次重算(capture.reeval_seconds,默认 60 秒)追上面板。

// freshKeep 是查价页现取的 AODP 最多留多久。正常情况下下一次全量拉回来的就不比它旧,
// 那时就淘汰了(见 since);这一条只管 -scan 0、一直不跑全量时别让它随查过的物品数一直涨
const freshKeep = 6 * time.Hour

type freshKey struct {
	item, city string
	quality    int
}

type freshEntry struct {
	rec aodp.PriceRecord
	at  time.Time // 最近一次并进来的时刻
}

// freshPrices 记下查价页现取回来的 AODP 当前价,同一格逐字段"时间戳新的胜"。零值可用。
type freshPrices struct {
	mu sync.Mutex
	m  map[freshKey]freshEntry
}

func (f *freshPrices) note(recs []aodp.PriceRecord, at time.Time) {
	if len(recs) == 0 {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[freshKey]freshEntry{}
	}
	for _, r := range recs {
		k := freshKey{r.ItemID, r.City, r.Quality}
		if e, ok := f.m[k]; ok {
			r, _ = scan.NewerPrice(e.rec, r)
		}
		f.m[k] = freshEntry{rec: r, at: at}
	}
	for k, e := range f.m {
		if at.Sub(e.at) > freshKeep {
			delete(f.m, k)
		}
	}
}

// since 返回 t 之后并进来的记录,item 非空时只要这个物品的。t 之前的顺手淘汰:
// t 是当前快照开始拉 AODP 的时刻,那之前查价页取到的,快照里的都不会比它旧
// (AODP 的每个价只会往后更新)。淘汰只为省内存,留着也不会盖掉更新的价
func (f *freshPrices) since(t time.Time, item string) []aodp.PriceRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []aodp.PriceRecord
	for k, e := range f.m {
		if e.at.Before(t) {
			delete(f.m, k)
			continue
		}
		if item == "" || k.item == item {
			out = append(out, e.rec)
		}
	}
	return out
}

// evalSnapshot 是拿去评估的那份快照:s.snap 并上查价页现取过的 AODP。
// s.snap 本身不换:全量换了快照,这里记下的照样有效,下一次评估再并一遍
func (s *Service) evalSnapshot(snap *scan.Snapshot) *scan.Snapshot {
	if snap == nil {
		return nil
	}
	return snap.WithPrices(s.fresh.since(snap.FetchedAt, ""))
}

// gridPrices 是查价格子用的 AODP 当前价:这次取回的 ∪ 以前查价取回的 ∪ 快照里这个物品的,
// 逐字段新的胜。和 evalSnapshot 同一个并法,卡片和面板用的 AODP 因此是同一份。
//
// 这次没取到(fetched 为空)时就是快照里的那份 —— 正是卡片在用的。
func (s *Service) gridPrices(itemID string, fetched []aodp.PriceRecord, now time.Time) []aodp.PriceRecord {
	s.fresh.note(fetched, now)
	var since time.Time
	var base []aodp.PriceRecord
	if snap := s.snap.Load(); snap != nil {
		since = snap.FetchedAt
		for _, p := range snap.Prices {
			if p.ItemID == itemID {
				base = append(base, p)
			}
		}
	}
	idx := map[freshKey]int{}
	var out []aodp.PriceRecord
	add := func(r aodp.PriceRecord) {
		k := freshKey{r.ItemID, r.City, r.Quality}
		if i, ok := idx[k]; ok {
			out[i], _ = scan.NewerPrice(out[i], r)
			return
		}
		idx[k] = len(out)
		out = append(out, r)
	}
	// 先放这次取回的:它带着 AODP 对每个组合都回的那一行(没数据时全 0),顺序也和以前一样
	for _, r := range fetched {
		add(r)
	}
	for _, r := range base {
		add(r)
	}
	for _, r := range s.fresh.since(since, itemID) {
		add(r)
	}
	return out
}
