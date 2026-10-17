package refine

import (
	"math"
	"sort"
	"time"
)

// 原料子问题(设计 §2.3 ①)。
//
// 转化边没有容量、单价固定,所以挂单簿上每一档 ℓ(在节点 v)供给目标 s 的等效单价
// = 挂单价 + dist(v→s)。只有一个目标时,把所有档按等效单价排序、取最便宜的 X 件
// 就是精确最优(每件货各自走自己最便宜的那条路,互不影响);结果天然是「卖一附近
// 那几件直接买 8.4,其余从 7.0 转上来」这种混合方案。
//
// 多个目标(8.4 矿和 7.4 矿)共用同一份挂单时,v1 从高 tier 往下依次分配、共享已用量。
// 这不是精确解:同一档便宜货对两个目标都便宜时,先到先得。source_test 里拿最小费用流
// 做对照,真实抓包上的偏差见那里。

// offerKey 标识挂单簿上的一档:物品 × 城市 × 第几档。bid 是「挂买单收料」那一档。
type offerKey struct {
	item, city string
	idx        int
	bid        bool
}

// offer 是一档能拿来用的货,以及它供给某个目标时的等效单价。
type offer struct {
	key offerKey
	// price 是成交价:吃卖单是挂单价,挂买单是 买一 + outbid
	price float64
	// mul 是买方系数:吃卖单 1.0,挂买单 1.025(F3)
	mul float64
	// transport 是每件运费(外城买料,Sources.TransportPct)
	transport float64
	// route 是每件从 key.item 转化到目标的实付;直接买目标时是 0
	route float64
	// eff = price × mul + transport + route,排序键
	eff float64
	// qty 是盘口上的件数;+Inf 表示深度未知或不限量
	qty float64
	// target 是这档货最后变成什么(转化目标;买现成的就是自己)
	target string

	seen         time.Time
	source       string
	truncated    bool
	lastVisible  bool // 是这一边可见阶梯的最后一档:吃到它就是吃穿了可见深度
	depthUnknown bool
}

// buyUnit 是每件买入的花费(不含转化)。
func (o offer) buyUnit() float64 { return o.price * o.mul }

func sortOffers(os []offer) {
	sort.SliceStable(os, func(i, j int) bool {
		a, b := os[i], os[j]
		if math.Abs(a.eff-b.eff) > costEps {
			return a.eff < b.eff
		}
		// 平局:按物品 id、城市、档位排,保证同一份输入永远得到同一个拆分
		if a.key.item != b.key.item {
			return a.key.item < b.key.item
		}
		if a.key.city != b.key.city {
			return a.key.city < b.key.city
		}
		if a.key.bid != b.key.bid {
			return !a.key.bid
		}
		return a.key.idx < b.key.idx
	})
}

// askOffers 是一个物品在可买城市的全部卖单档,每档等效单价 = 价 + 运费 + route。
// 过期或没有盘口的城市跳过。
func (e *engine) askOffers(item string, route float64, target string) []offer {
	var out []offer
	for _, city := range e.p.Sources.Cities {
		l, ok := e.side(item, city, true)
		if !ok {
			continue
		}
		transport := 0.0
		for i, lv := range l.levels {
			price := float64(lv.Price)
			if city != e.p.City {
				transport = price * e.p.Sources.TransportPct
			}
			qty := float64(lv.Qty)
			if l.depthUnknown {
				qty = math.Inf(1)
			}
			o := offer{
				key:   offerKey{item: item, city: city, idx: i},
				price: price, mul: e.mkt.TakeAsk, transport: transport, route: route,
				qty: qty, target: target,
				seen: l.seenAt(i), source: l.source, truncated: l.truncated,
				lastVisible: i == len(l.levels)-1, depthUnknown: l.depthUnknown,
			}
			o.eff = o.buyUnit() + o.transport + o.route
			out = append(out, o)
		}
	}
	return out
}

// bidOffer 是「挂买单收料」:(买一 + outbid) × 1.025,件数是用户设的上限(0 = 不限)。
// 只在生产城市挂。能不能收到、要等多久都不知道(设计 §2.2),所以它只进单独的候选。
func (e *engine) bidOffer(item string) (offer, bool) {
	l, ok := e.side(item, e.p.City, false)
	if !ok {
		return offer{}, false
	}
	qty := e.p.Sources.BidMaxQty
	if qty <= 0 {
		qty = math.Inf(1)
	}
	o := offer{
		key:   offerKey{item: item, city: e.p.City, bid: true},
		price: float64(l.levels[0].Price + e.p.Fees.Outbid), mul: e.mkt.PostBid,
		qty: qty, target: item, seen: l.seenAt(0), source: l.source,
	}
	o.eff = o.buyUnit()
	return o, true
}

// rawOffers 是目标原料的全部来路(混合最优用),按等效单价排好。按目标缓存:
// 等效单价只取决于配方和费率,和已用量无关。
func (e *engine) rawOffers(target string) []offer {
	if os, ok := e.rawCache[target]; ok {
		return os
	}
	routes := e.routes(target)
	starts := make([]string, 0, len(routes))
	for v := range routes {
		starts = append(starts, v)
	}
	sort.Strings(starts)
	var out []offer
	for _, v := range starts {
		out = append(out, e.askOffers(v, routes[v].cost, target)...)
	}
	sortOffers(out)
	e.rawCache[target] = out
	return out
}

// pureOffers 是「只从 start 买、再转到 target」这一条纯路线的货。
func (e *engine) pureOffers(target, start string) []offer {
	r, ok := e.routes(target)[start]
	if !ok {
		return nil
	}
	os := e.askOffers(start, r.cost, target)
	sortOffers(os)
	return os
}

// buyOffers 是一个物品直接买现成的货(半成品、心)。按物品缓存。
func (e *engine) buyOffers(item string) []offer {
	if os, ok := e.buyCache[item]; ok {
		return os
	}
	os := e.askOffers(item, 0, item)
	sortOffers(os)
	e.buyCache[item] = os
	return os
}

// routes 按目标缓存 routesTo。
func (e *engine) routes(target string) map[string]route {
	if r, ok := e.routeCache[target]; ok {
		return r
	}
	r := e.g.routesTo(target, e.p.Fees.TransmuteFactor, e.p.Fees.TransmuteStationPer100)
	e.routeCache[target] = r
	return r
}

// take 是从一档货里拿了多少。
type take struct {
	o   offer
	qty float64
}

// residual 是这一档还剩多少(减去这次计算里已经分出去的)。
func (st *state) residual(o offer) float64 { return o.qty - st.used[o.key] }

// offerCurve 把剩余的货变成成本曲线,只取前 capQ 件。
func (st *state) offerCurve(os []offer, capQ float64) curve {
	var c curve
	acc := 0.0
	for _, o := range os {
		if acc >= capQ-qtyEps {
			break
		}
		rem := st.residual(o)
		if rem <= qtyEps {
			continue
		}
		c = append(c, seg{qty: rem, unit: o.eff})
		acc += rem
	}
	return c.truncate(capQ)
}

// allocate 按等效单价从便宜到贵拿 q 件,记进已用量。
func (st *state) allocate(os []offer, q float64) (takes []take, total, got float64) {
	remaining := q
	for _, o := range os {
		if remaining <= qtyEps {
			break
		}
		rem := st.residual(o)
		if rem <= qtyEps {
			continue
		}
		t := math.Min(rem, remaining)
		st.used[o.key] += t
		takes = append(takes, take{o: o, qty: t})
		total += t * o.eff
		got += t
		remaining -= t
	}
	return takes, total, got
}

// PureRoute 是「原料来路」表的一行:X 件全部从一个起点买、再转化到目标。
type PureRoute struct {
	Start string   `json:"start"`
	Path  []string `json:"path"`
	// Unit 是每件目标原料的均价(买价沿阶梯吃深度 + 转化费 + 运费);深度不够时为 nil
	Unit *float64 `json:"unit"`
	// Available 是这个起点在可买城市的可见件数合计;深度未知时为 nil
	Available *float64 `json:"available"`
	Filled    bool     `json:"filled"`
	// Route 是每件从这个起点转化到目标的实付;直接买为 0
	Route float64 `json:"route"`
	// MaxPrice 是在生产城吃卖单的买价上限 = (推荐路线的到手均价上限 − Route) ÷ 买方系数:
	// 起点买价涨过它,照推荐路线做这一批就不赚钱。没算盈亏平衡价时为 nil
	MaxPrice *float64 `json:"max_price"`
}

// MixedSplit 是混合最优里的一段:从某城某起点买多少件、价位区间、走哪条路。
type MixedSplit struct {
	City      string   `json:"city"`
	Start     string   `json:"start"`
	Qty       float64  `json:"qty"`
	PriceFrom int64    `json:"price_from"`
	PriceTo   int64    `json:"price_to"`
	Path      []string `json:"path"`
	Bid       bool     `json:"bid,omitempty"`
}

// MixedSource 是混合最优:单价和拆分。
type MixedSource struct {
	Unit   *float64     `json:"unit"`
	Got    float64      `json:"got"`
	Filled bool         `json:"filled"`
	Split  []MixedSplit `json:"split"`
}

// RawSource 回答「这种原料买现成的、还是从哪一级转上来最便宜」(设计 §4.5「原料来路」表)。
type RawSource struct {
	// Need 是推荐路线里这种原料的稳态需求量
	Need  float64     `json:"need"`
	Pure  []PureRoute `json:"pure"`
	Mixed MixedSource `json:"mixed"`
	// Breakeven 是推荐路线里这种原料的到手均价上限(见 Breakeven);推荐路线没算出来时为 nil
	Breakeven *Breakeven `json:"breakeven"`
}

// pureRoutes 按需求量 need 算每个起点的纯路线,从便宜到贵;深度不够的排在最后。
// 用满盘口算(不减这次计算的已用量):这张表回答的是「单独这样买要多少钱」。
func (e *engine) pureRoutes(target string, need float64) []PureRoute {
	routes := e.routes(target)
	starts := make([]string, 0, len(routes))
	for v := range routes {
		starts = append(starts, v)
	}
	sort.Strings(starts)
	var out []PureRoute
	for _, v := range starts {
		os := e.pureOffers(target, v)
		if len(os) == 0 {
			continue
		}
		pr := PureRoute{Start: v, Path: pathOf(routes, v), Route: routes[v].cost}
		avail, unknown := 0.0, false
		for _, o := range os {
			if math.IsInf(o.qty, 1) {
				unknown = true
			} else {
				avail += o.qty
			}
		}
		if !unknown {
			pr.Available = &avail
		}
		fresh := &state{e: e, used: map[offerKey]float64{}}
		_, total, got := fresh.allocate(os, need)
		pr.Filled = got >= need-qtyEps
		if pr.Filled && need > 0 {
			u := total / need
			pr.Unit = &u
		}
		out = append(out, pr)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Unit == nil) != (b.Unit == nil) {
			return a.Unit != nil
		}
		if a.Unit != nil && math.Abs(*a.Unit-*b.Unit) > costEps {
			return *a.Unit < *b.Unit
		}
		return a.Start < b.Start
	})
	return out
}

// mixedFromTakes 把一次分配整理成拆分表:按 城市 × 起点 × 是否挂买单 合并。
func mixedFromTakes(routes map[string]route, takes []take, need float64) MixedSource {
	type k struct {
		city, start string
		bid         bool
	}
	idx := map[k]int{}
	var ms MixedSource
	total := 0.0
	for _, t := range takes {
		key := k{t.o.key.city, t.o.key.item, t.o.key.bid}
		i, ok := idx[key]
		if !ok {
			i = len(ms.Split)
			idx[key] = i
			ms.Split = append(ms.Split, MixedSplit{
				City: key.city, Start: key.start, Bid: key.bid,
				PriceFrom: int64(math.Round(t.o.price)), Path: pathOf(routes, key.start),
			})
		}
		ms.Split[i].Qty += t.qty
		ms.Split[i].PriceTo = int64(math.Round(t.o.price))
		ms.Got += t.qty
		total += t.qty * t.o.eff
	}
	ms.Filled = ms.Got >= need-qtyEps
	if ms.Got > 0 {
		u := total / ms.Got
		ms.Unit = &u
	}
	sort.SliceStable(ms.Split, func(i, j int) bool {
		if math.Abs(ms.Split[i].Qty-ms.Split[j].Qty) > qtyEps {
			return ms.Split[i].Qty > ms.Split[j].Qty
		}
		return ms.Split[i].Start < ms.Split[j].Start
	})
	return ms
}
