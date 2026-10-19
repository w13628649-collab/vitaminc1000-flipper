package flip

import (
	"context"
	"runtime"
	"slices"
	"sync"

	"albion-guild/internal/refine"
)

// 最赚件数(个人优化建议 #9):做的越多,料沿着卖单阶梯越买越贵、吃买单越卖越便宜,
// 这一批的利润先涨后跌。引擎一次只算一个件数,以前要用户自己一个个试。
//
// 这里同一份数据快照、同一套参数,按一串件数各算一遍,取这一批利润最高的那个件数。
// 先粗扫(qtyGrid,几何间隔到 refine.MaxQty),再在最好那一格两边细扫一轮。

// qtyGrid 是粗扫的件数
var qtyGrid = []int{1, 2, 3, 5, 8, 10, 15, 20, 30, 50, 75, 100, 150, 200, 300, 500, 750,
	1000, 1500, 2000, 3000, 5000, 7500, refine.MaxQty}

// qtyFine 是细扫在最好那一格两边各取几个点
const qtyFine = 6

// RefineQtyPoint 是一个件数下,这一批最赚的那条路线。
type RefineQtyPoint struct {
	Qty int `json:"qty"`
	// Total / TotalPct / Unit 是主出售方式下这一批的利润、总利润率、每件利润
	Total    float64 `json:"total"`
	TotalPct float64 `json:"total_pct"`
	Unit     float64 `json:"unit"`
	Label    string  `json:"label"`
	// Filled=false:这个件数下没有一条路线拿得满料(盘口不够深),利润不能当真,不参与选最好
	Filled bool `json:"filled"`
}

// RefineQtyScan 是 POST /api/refine/qtyscan 的结果。
type RefineQtyScan struct {
	Target string `json:"target"`
	City   string `json:"city"`
	Mode   string `json:"mode"` // 主出售方式 post | take
	// Points 按件数从小到大,粗扫和细扫的都在里面
	Points []RefineQtyPoint `json:"points"`
	// Best 是这一批利润最高、拿得满料的那个件数;一个都不赚钱时为 nil
	Best *RefineQtyPoint `json:"best"`
	// Current 是请求里那个件数
	Current *RefineQtyPoint `json:"current"`
	// Unbounded:最好的件数顶到了上限,而且用到的价里有不知道深度的(只有 AODP 价)。
	// 利润一直涨只是因为那几个价按不限量算,不是真的能做这么多
	Unbounded bool `json:"unbounded"`
	// VolumePerDay 是出售城市的日均成交件数(没有数据时为 nil)。这一批的利润不管卖多久,
	// 界面拿它说"做这么多要卖几天"
	VolumePerDay *float64 `json:"volume_per_day"`
}

// RefineQtyScan 对同一个目标、同一套参数,按一串件数各算一遍,找这一批利润最高的件数。
func (s *Service) RefineQtyScan(ctx context.Context, p refine.Params, o RefineOpts) (*RefineQtyScan, error) {
	s.refine.init()
	p = refine.Normalized(p)
	if err := s.checkRefine(p); err != nil {
		return nil, err
	}
	line, _ := refine.LineOfItem(p.Target)
	cities := allCities(p)
	snaps, _, err := s.ensureSnaps(ctx, line, cities, o.After)
	if err != nil {
		return nil, err
	}
	now := s.refine.now().UTC()
	p.Now = now
	recipes := s.Recipes()
	items, err := refine.ItemsFor(recipes, p.Target)
	if err != nil {
		return nil, &ParamError{Err: err}
	}
	keys := refineKeys(items, buyCities(p), []string{p.Target}, p.Sell.City)
	books, _, _ := s.mergeBooks(keys, snaps, now, p.Sources.MaxAgeHours)
	vol := snaps[p.Sell.City].volume(p.Target, s.Cfg.Refining.VolumeDays)
	mode := refine.SellPost
	if len(p.Sell.Modes) > 0 {
		mode = p.Sell.Modes[0]
	}

	type outcome struct {
		pt      RefineQtyPoint
		unknown bool // 用到了不知道深度的价
	}
	results := map[int]outcome{}
	var mu sync.Mutex
	eval := func(qtys []int) {
		var todo []int
		mu.Lock()
		for _, q := range qtys {
			if _, done := results[q]; !done && q >= 1 && q <= refine.MaxQty && !slices.Contains(todo, q) {
				todo = append(todo, q)
			}
		}
		mu.Unlock()
		next := make(chan int)
		var wg sync.WaitGroup
		for range min(runtime.GOMAXPROCS(0), len(todo)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for q := range next {
					pq := p
					pq.Qty = q
					res, err := refine.Plan(refine.Input{Params: pq, Books: books, Recipes: recipes, Volume: vol})
					o := outcome{pt: RefineQtyPoint{Qty: q}}
					if err == nil {
						o.pt = bestAtQty(res, q, mode)
						o.unknown = len(res.Data.DepthUnknown) > 0
					}
					mu.Lock()
					results[q] = o
					mu.Unlock()
				}
			}()
		}
		for _, q := range todo {
			select {
			case next <- q:
			case <-ctx.Done():
			}
		}
		close(next)
		wg.Wait()
	}

	eval(append(slices.Clone(qtyGrid), p.Qty))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 在最好那一格和左右两个粗扫点之间细扫
	if bi := bestGridIndex(func(q int) (RefineQtyPoint, bool) { o, ok := results[q]; return o.pt, ok }); bi >= 0 {
		lo, hi := qtyGrid[max(bi-1, 0)], qtyGrid[min(bi+1, len(qtyGrid)-1)]
		var fine []int
		for i := 1; i <= qtyFine; i++ {
			fine = append(fine, lo+(qtyGrid[bi]-lo)*i/(qtyFine+1), qtyGrid[bi]+(hi-qtyGrid[bi])*i/(qtyFine+1))
		}
		eval(fine)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := &RefineQtyScan{Target: p.Target, City: p.City, Mode: mode}
	if vol.Known && vol.PerDay > 0 {
		v := vol.PerDay
		out.VolumePerDay = &v
	}
	qs := make([]int, 0, len(results))
	for q := range results {
		qs = append(qs, q)
	}
	slices.Sort(qs)
	for _, q := range qs {
		out.Points = append(out.Points, results[q].pt)
	}
	for i := range out.Points {
		pt := &out.Points[i]
		if pt.Qty == p.Qty {
			out.Current = pt
		}
		if pt.Filled && pt.Total > 0 && (out.Best == nil || pt.Total > out.Best.Total) {
			out.Best = pt
		}
	}
	if out.Best != nil && out.Best.Qty == refine.MaxQty && results[refine.MaxQty].unknown {
		out.Unbounded = true
	}
	return out, nil
}

// bestAtQty 是一次计算里,主出售方式下这一批利润最高、拿得满料的普通候选。
// 一条都拿不满料时给利润最高的那条、标 Filled=false
func bestAtQty(res *refine.Result, q int, mode string) RefineQtyPoint {
	pt := RefineQtyPoint{Qty: q}
	have := false
	for _, c := range res.Candidates {
		if c.Reference != "" {
			continue
		}
		pl := c.Profit.Post
		if mode == refine.SellTake {
			pl = c.Profit.Take
		}
		if pl == nil {
			continue
		}
		better := !have || (c.Filled && !pt.Filled) || (c.Filled == pt.Filled && pl.Total > pt.Total)
		if better {
			pt = RefineQtyPoint{Qty: q, Total: pl.Total, TotalPct: pl.TotalPct, Unit: pl.Unit, Label: c.Label, Filled: c.Filled}
			have = true
		}
	}
	return pt
}

// bestGridIndex 是粗扫里利润最高、拿得满料的那一格在 qtyGrid 里的下标,没有就是 −1
func bestGridIndex(at func(q int) (RefineQtyPoint, bool)) int {
	bi, best := -1, 0.0
	for i, q := range qtyGrid {
		pt, ok := at(q)
		if ok && pt.Filled && pt.Total > 0 && (bi < 0 || pt.Total > best) {
			bi, best = i, pt.Total
		}
	}
	return bi
}
