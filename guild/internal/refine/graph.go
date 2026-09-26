package refine

import (
	"math"
	"sort"

	"albion-guild/internal/catalog"
)

// graph 是配方数据建出来的图:原料之间的转化边,半成品的精炼配方。
//
// 配方一律来自数据(catalog 同步进库的那份,或内置快照),这里不按规则生成任何数字
// (设计 §4.2:转化银币不规整、会随版本改,数据文件才是权威)。
type graph struct {
	recipes *catalog.RecipeBook
	// transIn[w] 是所有转化成 w 的边。转化是 1 换 1、不返还、不用专注(B3)
	transIn map[string][]transEdge
}

// transEdge 是一条转化边:1 个 from 换 1 个 to。
type transEdge struct {
	from, to string
	// silver 是基础银币(B4),实付还要乘 k、加主人费用(B5、B6)
	silver int64
	// toIV 是产物的物品价值,主人费用按它算(B6)
	toIV float64
}

func newGraph(b *catalog.RecipeBook) *graph {
	g := &graph{recipes: b, transIn: map[string][]transEdge{}}
	for _, r := range b.All() {
		if r.Kind != catalog.RecipeTransmute || !InScope(r) || len(r.Inputs) != 1 {
			continue
		}
		in := r.Inputs[0]
		if in.Count != 1 || r.Amount != 1 {
			// 数据里转化都是 1 换 1(B3,recipes_test 断言过);哪天不是了,
			// 这里宁可不认这条边,也不按 1 换 1 算错账
			continue
		}
		g.transIn[r.ItemID] = append(g.transIn[r.ItemID],
			transEdge{from: in.ItemID, to: r.ItemID, silver: r.Silver, toIV: r.ItemValue})
	}
	for k := range g.transIn {
		edges := g.transIn[k]
		sort.Slice(edges, func(i, j int) bool { return edges[i].from < edges[j].from })
	}
	return g
}

// refineRecipes 是一个半成品的精炼配方:不用心的排在前面,再按 variant。
// 平局时选前面那条(见 chain.go)。
func (g *graph) refineRecipes(item string) []catalog.Recipe {
	rs := g.recipes.OfKind(item, catalog.RecipeRefine)
	out := append([]catalog.Recipe(nil), rs...)
	sort.SliceStable(out, func(i, j int) bool {
		hi, hj := usesHeart(out[i]), usesHeart(out[j])
		if hi != hj {
			return !hi
		}
		return out[i].Variant < out[j].Variant
	})
	return out
}

func usesHeart(r catalog.Recipe) bool {
	for _, in := range r.Inputs {
		if _, ok := heartOf(in.ItemID); ok {
			return true
		}
	}
	return false
}

// heartOf:这个 id 是不是某条线的心。
func heartOf(id string) (Line, bool) {
	for _, l := range Lines {
		if l.Heart == id {
			return l, true
		}
	}
	return Line{}, false
}

// levelSum 是 tier + 附魔。每条转化边都让它恰好 +1(同 tier +1 附魔,或同附魔 +1 tier,B3),
// 所以按它从大到小处理节点就是转化图的逆拓扑序。
func levelSum(id string) int {
	_, t, e, ok := ParseItemID(id)
	if !ok {
		return -1
	}
	return t + e
}

// route 是从某个原料转化到目标原料的最便宜走法。
type route struct {
	// cost 是每件从这里转到目标的实付合计
	cost float64
	// next 是下一跳;自己就是目标时为空
	next string
	// edge 是走到 next 那一步的单次实付
	edge float64
}

// routesTo 算每个原料到 target 的最便宜转化路线(设计 §2.3 ①)。
//
// 转化边没有容量、单价固定,是 DAG(tier 或附魔只增不减),按逆拓扑序做一遍 DP 就是最短路。
// 返回的 map 包含 target 自己(cost 0)和所有能转到它的原料。
// 平局取下一跳 id 小的那条,保证结果确定。
func (g *graph) routesTo(target string, k, feePer100 float64) map[string]route {
	out := map[string]route{target: {}}
	// 先收齐所有祖先,再按 levelSum 从大到小松弛
	seen := map[string]bool{target: true}
	stack := []string{target}
	var nodes []string
	for len(stack) > 0 {
		w := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		nodes = append(nodes, w)
		for _, e := range g.transIn[w] {
			if !seen[e.from] {
				seen[e.from] = true
				stack = append(stack, e.from)
			}
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		si, sj := levelSum(nodes[i]), levelSum(nodes[j])
		if si != sj {
			return si > sj
		}
		return nodes[i] < nodes[j]
	})
	for _, w := range nodes {
		rw, ok := out[w]
		if !ok {
			continue // 到不了 target(不会发生:nodes 都是从 target 反向找到的)
		}
		for _, e := range g.transIn[w] {
			edge := TransmuteCost(e.silver, k, e.toIV, feePer100)
			c := rw.cost + edge
			old, has := out[e.from]
			if !has || c < old.cost-costEps || (math.Abs(c-old.cost) <= costEps && w < old.next) {
				out[e.from] = route{cost: c, next: w, edge: edge}
			}
		}
	}
	return out
}

// path 是从 start 沿 next 走到目标的节点序列(含两端)。
func pathOf(routes map[string]route, start string) []string {
	p := []string{start}
	for cur := start; ; {
		r := routes[cur]
		if r.next == "" {
			return p
		}
		cur = r.next
		p = append(p, cur)
		if len(p) > 64 {
			return p // 防御:数据坏了成环也不会死循环
		}
	}
}

// costEps 是银币金额的比较容差。
const costEps = 1e-6
