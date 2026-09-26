package refine

import (
	"math"
	"math/rand/v2"
	"testing"
)

// 单个目标:「每档按等效价排序、取最便宜的 X 件」和最小费用流的精确解相同(设计 §2.3 ①)。
// 转化边不限量、单价固定,每件货各走各的最便宜路线,所以排序取件就是最优。
func Test单目标排序取件等于最小费用流(t *testing.T) {
	line := Lines[0]
	targets := []string{"T8_ORE_LEVEL4@4", "T8_ORE_LEVEL2@2", "T7_ORE_LEVEL3@3", "T6_ORE", "T5_ORE_LEVEL4@4"}
	for seed := uint64(1); seed <= 12; seed++ {
		books := synthBooks(line, "Thetford", seed)
		p := params("T8_METALBAR_LEVEL4@4")
		p.Fees.TransmuteFactor = 1 + 0.02*float64(seed%10)
		e := mustEngine(t, Input{Params: p, Books: books})
		rng := rand.New(rand.NewPCG(seed, 99))
		for _, target := range targets {
			need := 0.3 + 80*rng.Float64()
			st := &state{e: e, used: map[offerKey]float64{}, res: &evalResult{missing: map[string]bool{}}}
			_, greedy, got := st.allocate(e.rawOffers(target), need)
			exact, ok := exactRawCost(e, map[string]float64{target: need})
			if (got >= need-1e-7) != ok {
				t.Fatalf("seed %d %s:排序取件拿到 %.3f / %.3f,最小费用流可行=%v", seed, target, got, need, ok)
			}
			if ok && math.Abs(greedy-exact) > 1e-6*exact {
				t.Fatalf("seed %d %s ×%.3f:排序取件 %.4f,最小费用流 %.4f", seed, target, need, greedy, exact)
			}
		}
	}
}

// 多个目标共用挂单:v1 从高 tier 往下依次分配(设计 §2.3 ①)。不是精确解,
// 这里量一下它和最小费用流差多少:永远不会比精确解便宜;合成数据上把偏差打出来。
// 真实抓包上的偏差见 regress_test 的 Test真实抓包多目标分配和最小费用流对照。
func Test多目标从高往下分配不比最小费用流便宜(t *testing.T) {
	line := Lines[0]
	worst := 0.0
	for seed := uint64(1); seed <= 12; seed++ {
		books := synthBooks(line, "Thetford", seed)
		p := params("T8_METALBAR_LEVEL4@4")
		e := mustEngine(t, Input{Params: p, Books: books})
		rng := rand.New(rand.NewPCG(seed, 7))
		demand := map[string]float64{}
		order := []string{"T8_ORE_LEVEL4@4", "T7_ORE_LEVEL4@4", "T6_ORE_LEVEL4@4", "T5_ORE_LEVEL4@4"}
		st := &state{e: e, used: map[offerKey]float64{}, res: &evalResult{missing: map[string]bool{}}}
		greedy, feasible := 0.0, true
		for _, target := range order {
			d := 0.5 + 20*rng.Float64()
			demand[target] = d
			_, c, got := st.allocate(e.rawOffers(target), d)
			greedy += c
			feasible = feasible && got >= d-1e-7
		}
		exact, ok := exactRawCost(e, demand)
		if !ok || !feasible {
			continue
		}
		if greedy < exact-1e-6*exact {
			t.Fatalf("seed %d:依次分配 %.2f 比最小费用流 %.2f 还便宜,最小费用流写错了", seed, greedy, exact)
		}
		worst = math.Max(worst, greedy/exact-1)
	}
	t.Logf("合成盘口上「从高往下分配」相对最小费用流的最大偏差 %.4f%%", worst*100)
}

// 已知的局限,留证据:一档便宜货对两个目标都便宜、而低 tier 那个没有别的便宜来路时,
// 先到先得会把它分给高 tier。真实抓包上没出现(见 regress_test),出现了再换最小费用流。
func Test多目标先到先得的反例(t *testing.T) {
	books := Books{}
	// 7.4 矿只有 1 件、价 100;8.4 矿直接买比「7.4 转上来」只贵 1 银
	route := TransmuteCost(779_625, DefaultTransmuteFactor, 409.6, DefaultTransmuteStationFeePer100)
	setAsks(books, "T7_ORE_LEVEL4@4", "Thetford", 100, 1)
	setAsks(books, "T8_ORE_LEVEL4@4", "Thetford", int64(math.Ceil(100+route))+1, 10)
	// 7.4 矿的另一条来路:7.3 升附魔,贵得多
	setAsks(books, "T7_ORE_LEVEL3@3", "Thetford", 50_000, 10)
	e := mustEngine(t, Input{Params: params("T8_METALBAR_LEVEL4@4"), Books: books})
	demand := map[string]float64{"T8_ORE_LEVEL4@4": 1, "T7_ORE_LEVEL4@4": 1}
	st := &state{e: e, used: map[offerKey]float64{}, res: &evalResult{missing: map[string]bool{}}}
	_, c8, _ := st.allocate(e.rawOffers("T8_ORE_LEVEL4@4"), 1)
	_, c7, _ := st.allocate(e.rawOffers("T7_ORE_LEVEL4@4"), 1)
	exact, ok := exactRawCost(e, demand)
	if !ok {
		t.Fatal("最小费用流不可行")
	}
	gap := c8 + c7 - exact
	if gap < 0 {
		t.Fatalf("依次分配 %.1f 比精确解 %.1f 还便宜", c8+c7, exact)
	}
	t.Logf("反例:依次分配 %.0f,精确解 %.0f,多花 %.0f(%.1f%%)", c8+c7, exact, gap, gap/exact*100)
}

// 混合最优 ≤ 任何一条纯路线(同样的需求量、同样的盘口)。
func Test混合最优不贵于任一纯路线(t *testing.T) {
	for seed := uint64(1); seed <= 8; seed++ {
		for _, l := range Lines {
			books := synthBooks(l, l.City, seed)
			e := mustEngine(t, Input{Params: params(l.RefinedID(8, 4)), Books: books})
			for _, target := range []string{l.RawID(8, 4), l.RawID(7, 2), l.RawID(6, 0)} {
				for _, need := range []float64{1, 7.5, 40} {
					st := &state{e: e, used: map[offerKey]float64{}, res: &evalResult{missing: map[string]bool{}}}
					_, mixed, got := st.allocate(e.rawOffers(target), need)
					if got < need-qtyEps {
						continue
					}
					for _, pr := range e.pureRoutes(target, need) {
						if pr.Filled && mixed/need > *pr.Unit+1e-6 {
							t.Fatalf("%s ×%v:混合 %.2f 贵于从 %s 纯转化 %.2f", target, need, mixed/need, pr.Start, *pr.Unit)
						}
					}
				}
			}
		}
	}
}

// 平局按 物品 id → 城市 → 档位 排,结果确定:两个起点等效价完全相同时,先拿 id 小的。
func Test等效价平局按物品id排(t *testing.T) {
	p := params("T8_METALBAR_LEVEL4@4")
	p.Fees.TransmuteFactor = 1
	p.Fees.TransmuteStationPer100 = 0
	// 转到 T5.1 矿:从 T5.0 升附魔 2000,从 T4.1 升 tier 1563
	books := Books{}
	setAsks(books, "T5_ORE", "Thetford", 1000, 5)
	setAsks(books, "T4_ORE_LEVEL1@1", "Thetford", 1437, 5) // 1437 + 1563 = 1000 + 2000
	setAsks(books, "T5_ORE_LEVEL1@1", "Thetford", 3000, 5) // 直接买也是 3000
	e := mustEngine(t, Input{Params: p, Books: books})
	for run := 0; run < 5; run++ {
		st := &state{e: e, used: map[offerKey]float64{}, res: &evalResult{missing: map[string]bool{}}}
		takes, _, _ := st.allocate(e.rawOffers("T5_ORE_LEVEL1@1"), 7)
		var seq []string
		for _, tk := range takes {
			seq = append(seq, tk.o.key.item)
		}
		want := []string{"T4_ORE_LEVEL1@1", "T5_ORE"}
		if len(seq) != 2 || seq[0] != want[0] || seq[1] != want[1] || takes[1].qty != 2 {
			t.Fatalf("第 %d 次:拿货顺序 %v(%+v),想要 %v 且第二个拿 2 件", run, seq, takes, want)
		}
	}
}
