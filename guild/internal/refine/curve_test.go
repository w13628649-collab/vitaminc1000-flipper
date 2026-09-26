package refine

import (
	"math"
	"math/rand/v2"
	"sort"
	"testing"
)

// randCurve 生成一条凸曲线(单价升序),最后一段有一定概率是不限量。
func randCurve(rng *rand.Rand, n int) curve {
	var c curve
	for i := 0; i < n; i++ {
		c = append(c, seg{qty: 0.5 + 10*rng.Float64(), unit: 1 + 1000*rng.Float64()})
	}
	sort.Slice(c, func(i, j int) bool { return c[i].unit < c[j].unit })
	if rng.IntN(3) == 0 {
		c[len(c)-1].qty = math.Inf(1)
	}
	return c
}

// 下确卷积:合并后要 q 件的成本 = 在两条曲线之间怎么分 q 都不会更便宜。
// 逐 k 穷举(包含两边所有断点)对照。
func Test曲线合并等于逐k穷举(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 300; trial++ {
		a, b := randCurve(rng, 1+rng.IntN(5)), randCurve(rng, 1+rng.IntN(5))
		q := 0.1 + 30*rng.Float64()
		got, filled, _ := mergeCurves(a.markBuy(), b).cost(q)
		// 两边的断点都是候选
		ks := []float64{0, q}
		acc := 0.0
		for _, s := range a {
			acc += s.qty
			ks = append(ks, acc)
		}
		acc = 0
		for _, s := range b {
			acc += s.qty
			ks = append(ks, q-acc)
		}
		best := math.Inf(1)
		for _, k := range ks {
			if k < 0 || k > q {
				continue
			}
			ca, ga, _ := a.cost(k)
			cb, gb, _ := b.cost(q - k)
			if ga < k-qtyEps || gb < q-k-qtyEps {
				continue
			}
			best = math.Min(best, ca+cb)
		}
		if math.IsInf(best, 1) {
			if filled >= q-qtyEps {
				t.Fatalf("第 %d 次:穷举说凑不满,合并曲线却凑满了", trial)
			}
			continue
		}
		if math.Abs(got-best) > 1e-6*math.Max(1, best) {
			t.Fatalf("第 %d 次:合并 %.6f,穷举 %.6f", trial, got, best)
		}
	}
}

// 逐点相加:做 m 件的成本 = 各项成本之和;缩放:C.scale(a)(m) = C(a·m)。
func Test曲线相加和缩放(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for trial := 0; trial < 300; trial++ {
		a, b := randCurve(rng, 1+rng.IntN(5)), randCurve(rng, 1+rng.IntN(5))
		fa, fb := 0.2+3*rng.Float64(), 0.2+3*rng.Float64()
		sum := sumCurves(a.scale(fa), b.scale(fb)).plus(7)
		m := 0.05 + 10*rng.Float64()
		got, gm, _ := sum.cost(m)
		ca, ga, _ := a.cost(m * fa)
		cb, gb, _ := b.cost(m * fb)
		wantFilled := ga >= m*fa-1e-7 && gb >= m*fb-1e-7
		if (gm >= m-1e-7) != wantFilled {
			t.Fatalf("第 %d 次:相加后能否做满 %v,分项 %v", trial, gm >= m-1e-7, wantFilled)
		}
		if wantFilled && math.Abs(got-(ca+cb+7*m)) > 1e-6*math.Max(1, got) {
			t.Fatalf("第 %d 次:相加 %.6f,分项之和 %.6f", trial, got, ca+cb+7*m)
		}
	}
	// 不限量的段:两条都到头不了时,和也不限量
	inf := curve{{qty: math.Inf(1), unit: 3}}
	s := sumCurves(inf, inf.scale(2))
	if !math.IsInf(s.capacity(), 1) || s[0].unit != 9 {
		t.Fatalf("两条不限量曲线相加: %+v", s)
	}
	if c, _, _ := s.cost(4); c != 36 {
		t.Fatalf("4 件应当 36,得到 %v", c)
	}
	// 边际:到头之后是 +Inf
	c := curve{{qty: 2, unit: 5}, {qty: 1, unit: 8}}
	if c.marginal(0) != 5 || c.marginal(2) != 8 || !math.IsInf(c.marginal(3), 1) {
		t.Fatalf("边际 %v %v %v", c.marginal(0), c.marginal(2), c.marginal(3))
	}
}
