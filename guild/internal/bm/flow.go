package bm

import "math"

// 最小费用流:逐次最短路(SPFA,边上有负费用),最短路的费用不再是负数时停——
// 再多送一件不赚钱了。一开始的图是有向无环的,没有负环;每次沿最短路增广不会造出负环。
// 规模是一个物品的几十档 × 几档,用不着更快的算法。

type flowEdge struct {
	to, rev int
	cap     int64
	cost    int64
}

// edgeRef 指向一条正向边,cap 是它最初的容量,用来读出流量
type edgeRef struct {
	from, idx int
	cap       int64
}

type flow struct{ g [][]flowEdge }

func newFlow(n int) *flow { return &flow{g: make([][]flowEdge, n)} }

func (f *flow) add(u, v int, capacity, cost int64) edgeRef {
	f.g[u] = append(f.g[u], flowEdge{to: v, rev: len(f.g[v]), cap: capacity, cost: cost})
	f.g[v] = append(f.g[v], flowEdge{to: u, rev: len(f.g[u]) - 1, cost: -cost})
	return edgeRef{from: u, idx: len(f.g[u]) - 1, cap: capacity}
}

func (f *flow) flowOn(r edgeRef) int64 { return r.cap - f.g[r.from][r.idx].cap }

func (f *flow) run(s, t int) {
	n := len(f.g)
	dist := make([]int64, n)
	inQ := make([]bool, n)
	prevV, prevE := make([]int, n), make([]int, n)
	for {
		for i := range dist {
			dist[i] = math.MaxInt64
		}
		dist[s] = 0
		queue := []int{s}
		inQ[s] = true
		for len(queue) > 0 {
			u := queue[0]
			queue = queue[1:]
			inQ[u] = false
			for i, e := range f.g[u] {
				if e.cap > 0 && dist[u]+e.cost < dist[e.to] {
					dist[e.to] = dist[u] + e.cost
					prevV[e.to], prevE[e.to] = u, i
					if !inQ[e.to] {
						inQ[e.to] = true
						queue = append(queue, e.to)
					}
				}
			}
		}
		if dist[t] >= 0 { // 到不了(MaxInt64)或者不赚钱
			return
		}
		push := int64(math.MaxInt64)
		for v := t; v != s; v = prevV[v] {
			push = min(push, f.g[prevV[v]][prevE[v]].cap)
		}
		for v := t; v != s; v = prevV[v] {
			e := &f.g[prevV[v]][prevE[v]]
			e.cap -= push
			f.g[v][e.rev].cap += push
		}
	}
}
