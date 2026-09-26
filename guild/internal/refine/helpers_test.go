package refine

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"albion-guild/internal/depth"
)

// testNow 是测试里的「现在」:09-26 那份抓包之后半小时左右。
var testNow = time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)

// mustEngine 按 Plan 的前几步建好 engine,测试直接调内部的评估。
func mustEngine(t *testing.T, in Input) *engine {
	t.Helper()
	if in.Recipes == nil {
		in.Recipes = EmbeddedRecipes()
	}
	p := in.Params.normalized()
	line, _, _, err := p.validate()
	if err != nil {
		t.Fatal(err)
	}
	e := newEngine(p, line, in)
	e.buildChain()
	return e
}

// params 是测试用的默认参数:Now 固定。
func params(target string) Params {
	p := DefaultParams(target)
	p.Now = testNow
	return p
}

// lv 把 价, 件, 价, 件… 写成阶梯。
func lv(pairs ...int64) []depth.Level {
	out := make([]depth.Level, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, depth.Level{Price: pairs[i], Qty: pairs[i+1]})
	}
	return out
}

func askSide(pairs ...int64) Ladder {
	return Ladder{Levels: lv(pairs...), Seen: testNow.Add(-time.Hour), Source: "capture"}
}

func setAsks(b Books, item, city string, pairs ...int64) {
	x := b[Key{item, city}]
	x.Asks = askSide(pairs...)
	b[Key{item, city}] = x
}

func setBids(b Books, item, city string, pairs ...int64) {
	x := b[Key{item, city}]
	x.Bids = askSide(pairs...)
	b[Key{item, city}] = x
}

// synthBooks 给一条线 T3~T8 的原料和半成品在 city 生成确定的随机盘口。
// 价格大致按 tier / 附魔翻倍,乘 0.6~1.5 的随机系数,好让「买 / 做 / 转化」
// 各种情况都出现;每边 2~7 档、每档 1~40 件。
func synthBooks(l Line, city string, seed uint64) Books {
	rng := rand.New(rand.NewPCG(seed, seed*7+1))
	b := Books{}
	ladder := func(fair float64) ([]int64, []int64) {
		n := 2 + rng.IntN(6)
		var asks, bids []int64
		p := fair * (0.6 + 0.9*rng.Float64())
		for i := 0; i < n; i++ {
			p *= 1 + 0.01 + 0.08*rng.Float64()
			asks = append(asks, int64(math.Round(p)), int64(1+rng.IntN(40)))
		}
		q := fair * 0.55 * (0.6 + 0.9*rng.Float64())
		for i := 0; i < n; i++ {
			q *= 1 - 0.01 - 0.1*rng.Float64()
			bids = append(bids, int64(math.Max(1, math.Round(q))), int64(1+rng.IntN(40)))
		}
		return asks, bids
	}
	for tier := 3; tier <= MaxTier; tier++ {
		for _, e := range Enchants(tier) {
			raw := 40 * math.Pow(2.2, float64(tier-4)) * math.Pow(2.6, float64(e))
			ref := raw * 3.5
			a, bb := ladder(raw)
			setAsks(b, l.RawID(tier, e), city, a...)
			setBids(b, l.RawID(tier, e), city, bb...)
			a, bb = ladder(ref)
			setAsks(b, l.RefinedID(tier, e), city, a...)
			setBids(b, l.RefinedID(tier, e), city, bb...)
		}
	}
	return b
}

func assertNear(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > tol {
		t.Fatalf("%s = %.4f,想要 %.4f(容差 %g)", name, got, want, tol)
	}
}

// ── 最小费用流(暴力对照用)─────────────────────────────────────────────

// mcf 是逐次最短路(Bellman-Ford)的最小费用流,容量可以是小数。
// 只给测试当「精确解」对照,规模几百个点,不讲究效率。
type mcf struct{ g [][]mcfEdge }

type mcfEdge struct {
	to, rev   int
	cap, cost float64
}

const mcfInf = 1e18

func newMCF(n int) *mcf { return &mcf{g: make([][]mcfEdge, n)} }

func (f *mcf) add(u, v int, cap, cost float64) {
	f.g[u] = append(f.g[u], mcfEdge{to: v, rev: len(f.g[v]), cap: cap, cost: cost})
	f.g[v] = append(f.g[v], mcfEdge{to: u, rev: len(f.g[u]) - 1, cap: 0, cost: -cost})
}

// run 从 s 到 t 送 want 的流,返回实际送出的流量和总费用。
func (f *mcf) run(s, t int, want float64) (flow, cost float64) {
	n := len(f.g)
	for flow < want-1e-9 {
		dist := make([]float64, n)
		inq := make([]bool, n)
		pv, pe := make([]int, n), make([]int, n)
		for i := range dist {
			dist[i] = math.Inf(1)
		}
		dist[s] = 0
		queue := []int{s}
		inq[s] = true
		for len(queue) > 0 {
			u := queue[0]
			queue = queue[1:]
			inq[u] = false
			for i, e := range f.g[u] {
				if e.cap > 1e-12 && dist[u]+e.cost < dist[e.to]-1e-9 {
					dist[e.to] = dist[u] + e.cost
					pv[e.to], pe[e.to] = u, i
					if !inq[e.to] {
						inq[e.to] = true
						queue = append(queue, e.to)
					}
				}
			}
		}
		if math.IsInf(dist[t], 1) {
			return flow, cost
		}
		push := want - flow
		for v := t; v != s; v = pv[v] {
			push = math.Min(push, f.g[pv[v]][pe[v]].cap)
		}
		for v := t; v != s; v = pv[v] {
			e := &f.g[pv[v]][pe[v]]
			e.cap -= push
			f.g[v][e.rev].cap += push
		}
		flow += push
		cost += push * dist[t]
	}
	return flow, cost
}

// exactRawCost 用最小费用流算「几个原料目标共用同一份挂单」的精确最小成本:
// 源 → 每一档挂单(容量 = 件数,费用 = 买价 + 运费)→ 物品节点 → 沿转化边(不限量,费用 = 实付)
// → 目标 → 汇(容量 = 需求量)。
func exactRawCost(e *engine, demand map[string]float64) (cost float64, ok bool) {
	items := map[string]int{}
	var order []string
	for target := range demand {
		for v := range e.routes(target) {
			if _, seen := items[v]; !seen {
				items[v] = 0
				order = append(order, v)
			}
		}
	}
	for i, v := range order {
		items[v] = 2 + i
	}
	var offers []offer
	for _, v := range order {
		offers = append(offers, e.askOffers(v, 0, v)...)
	}
	n := 2 + len(order) + len(offers)
	f := newMCF(n)
	src, sink := 0, 1
	for i, o := range offers {
		node := 2 + len(order) + i
		c := o.qty
		if math.IsInf(c, 1) {
			c = mcfInf
		}
		f.add(src, node, c, o.buyUnit()+o.transport)
		f.add(node, items[o.key.item], mcfInf, 0)
	}
	for _, w := range order {
		for _, te := range e.g.transIn[w] {
			if from, ok := items[te.from]; ok {
				f.add(from, items[w], mcfInf, TransmuteCost(te.silver, e.p.Fees.TransmuteFactor, te.toIV, e.p.Fees.TransmuteStationPer100))
			}
		}
	}
	total := 0.0
	for target, d := range demand {
		f.add(items[target], sink, d, 0)
		total += d
	}
	flow, c := f.run(src, sink, total)
	return c, flow >= total-1e-7
}
