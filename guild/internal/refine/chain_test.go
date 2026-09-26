package refine

import (
	"math"
	"sort"
	"strings"
	"testing"
)

// splitBooks 是一个手工例子:T5.4 钢条下面那级 T4.4 条的最优拆分落在「自炼曲线」的拐点上,
// 不在卖单的断点上。
//
//   - T4.4 矿只有 3 件便宜的(1,000),再往上 5,000:自炼的边际成本先是 ≈1,441,做到约 2.37 根后跳到 ≈6,505
//   - T4.4 条卖单 3,000 × 1、4,000 × 10
//   - T5.4 矿直接买 2,000 管够(从 T4.4 矿转上去要 3 万多,不会去抢 T4.4 矿)
//
// 所以最优是「做 2.37 根 + 买 1 根 @3,000 + 其余 @4,000」,买的件数 ≈ 3.96,不是 0、1、Q 里的任何一个。
func splitBooks() Books {
	b := Books{}
	setAsks(b, "T5_ORE_LEVEL4@4", "Thetford", 2000, 1000)
	setAsks(b, "T4_ORE_LEVEL4@4", "Thetford", 1000, 3, 5000, 100)
	setAsks(b, "T4_METALBAR_LEVEL4@4", "Thetford", 3000, 1, 4000, 10)
	setAsks(b, "T3_METALBAR", "Thetford", 50, 10000)
	setAsks(b, "T5_METALBAR_LEVEL4@4", "Thetford", 60000, 10)
	return b
}

// 买/做拆分和逐 k 穷举一致(设计 §4.7 第 3 条)。穷举的 k 包括卖单各档的断点、
// 自炼曲线的拐点和一张细网格;引擎的拆分不能比穷举的最好结果差。
func Test买做拆分和逐k穷举一致(t *testing.T) {
	p := params("T5_METALBAR_LEVEL4@4")
	p.Qty = 10
	for _, focus := range []tierSet{0, setOf(5), setOf(4), setOf(5, 4)} {
		e := mustEngine(t, Input{Params: p, Books: splitBooks()})
		s := strategy{raw: rawMixed, depth: 0, focus: focus}
		ev := e.run(s, nil)
		if !ev.filled {
			t.Fatalf("专注 %s:没拿满", focus)
		}
		r5 := e.rrr(focus.has(5))
		q := 10 * (1 - r5) // T4.4 条的需求量
		r4 := e.rrr(focus.has(4))
		a := 2 * (1 - r4) // 每根 T4.4 条的矿
		ks := []float64{0, q, 1, 11, q - 3/a}
		for i := 0; i <= 400; i++ {
			ks = append(ks, q*float64(i)/400)
		}
		best, bestK := math.Inf(1), 0.0
		for _, k := range ks {
			if k < 0 || k > q {
				continue
			}
			x := e.run(s, map[int]float64{1: k})
			if x.filled && x.total < best {
				best, bestK = x.total, k
			}
		}
		if ev.total > best+1e-6 {
			t.Fatalf("专注 %s:引擎拆分 %.4f,穷举在 k=%.4f 得到 %.4f", focus, ev.total, bestK, best)
		}
		if ev.total < best-1e-6 {
			t.Fatalf("专注 %s:引擎 %.4f 比穷举 %.4f 还低,穷举漏了拐点", focus, ev.total, best)
		}
		bought := 0.0
		for _, b := range ev.buys {
			if b.item == "T4_METALBAR_LEVEL4@4" {
				bought += b.qty
			}
		}
		assertNear(t, "买的 T4.4 条件数", bought, q-3/a, 1e-6)

		// 只试「0、卖单断点、Q」(设计 §2.3 ② 原来的写法)在这个例子里找不到最优
		coarse := math.Inf(1)
		for _, k := range []float64{0, 1, q} {
			if x := e.run(s, map[int]float64{1: k}); x.filled {
				coarse = math.Min(coarse, x.total)
			}
		}
		if coarse <= best+1 {
			t.Fatalf("专注 %s:只试卖单断点也找到了最优(%.1f vs %.1f),这个例子没测到拐点", focus, coarse, best)
		}
	}
}

// 专注开关的 Pareto 前沿和预算内最优,与 2^5 种开法逐一暴力算出来的一致(设计 §4.7 第 3 条)。
// 暴力那边每种开法单独走一遍 Plan(custom 模式),和引擎内部的枚举不共用缓存。
func Test专注组合与2的5次方暴力一致(t *testing.T) {
	l := Lines[0]
	for _, seed := range []uint64{3, 11} {
		books := synthBooks(l, l.City, seed)
		base := params(l.RefinedID(8, 4))
		base.Qty = 3
		base.PureStarts = 0
		base.Top = 1000
		for tier := 4; tier <= 8; tier++ {
			base.Focus.FCE[tier] = 40000 // 满专精:专注小,预算才卡得出区别
		}
		base.Focus.Budget = 1200
		res, err := Plan(Input{Params: base, Books: books, Recipes: EmbeddedRecipes()})
		if err != nil {
			t.Fatal(err)
		}
		if res.Focus.Combos != 32 || len(res.Focus.Tiers) != 5 {
			t.Fatalf("专注组合 %d 种、tier %v", res.Focus.Combos, res.Focus.Tiers)
		}
		type pt struct {
			tiers       []int
			focus, cost float64
		}
		var all []pt
		for mask := 0; mask < 32; mask++ {
			var tiers []int
			for j, tier := range []int{8, 7, 6, 5, 4} {
				if mask&(1<<j) != 0 {
					tiers = append(tiers, tier)
				}
			}
			p := base
			p.Focus.Mode = FocusCustom
			p.Focus.CustomTiers = tiers
			r, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
			if err != nil {
				t.Fatal(err)
			}
			id := "mixed|auto|focus:" + setOf(tiers...).String()
			var c *Candidate
			for i := range r.Candidates {
				if r.Candidates[i].ID == id {
					c = &r.Candidates[i]
				}
			}
			if c == nil {
				t.Fatalf("custom %v 的结果里没有 %s", tiers, id)
			}
			if c.Filled {
				all = append(all, pt{setOf(tiers...).list(), c.Focus.Effective, c.UnitCost})
			}
		}
		// 暴力的前沿:按专注从少到多,成本严格下降才留
		sort.SliceStable(all, func(i, j int) bool {
			if math.Abs(all[i].focus-all[j].focus) > costEps {
				return all[i].focus < all[j].focus
			}
			if math.Abs(all[i].cost-all[j].cost) > costEps {
				return all[i].cost < all[j].cost
			}
			return len(all[i].tiers) < len(all[j].tiers)
		})
		var front []pt
		bestCost := math.Inf(1)
		for _, x := range all {
			if x.cost < bestCost-costEps {
				front = append(front, x)
				bestCost = x.cost
			}
		}
		if len(front) != len(res.Focus.Pareto) {
			t.Fatalf("seed %d:暴力前沿 %d 个点,引擎 %d 个", seed, len(front), len(res.Focus.Pareto))
		}
		for i, x := range front {
			got := res.Focus.Pareto[i]
			assertNear(t, "前沿专注", got.FocusTotal, x.focus, 1e-6)
			assertNear(t, "前沿成本", got.UnitCost, x.cost, 1e-6)
		}
		// 预算内最优:几档预算,从卡得很紧到不卡
		for _, budget := range []float64{0, 150, 400, 900, 1100, 1e9} {
			pb := base
			pb.Focus.Budget = budget
			r, err := Plan(Input{Params: pb, Books: books, Recipes: EmbeddedRecipes()})
			if err != nil {
				t.Fatal(err)
			}
			pick := math.Inf(1)
			for _, x := range all {
				if x.focus <= budget+costEps {
					pick = math.Min(pick, x.cost)
				}
			}
			if r.Focus.BudgetPick == nil {
				t.Fatalf("预算 %.0f:没有预算内的开法(不开专注总是在预算内)", budget)
			}
			assertNear(t, "预算内最优成本", r.Focus.BudgetPick.UnitCost, pick, 1e-6)
			if r.Focus.BudgetPick.FocusTotal > budget+costEps {
				t.Fatalf("预算 %.0f:预算内最优超了预算: %+v", budget, r.Focus.BudgetPick)
			}
			// 预算内最优那种开法一定在候选里(auto 模式的第一种专注集合)
			findCandidate(t, r, "mixed|auto|focus:"+setOf(r.Focus.BudgetPick.Tiers...).String())
			t.Logf("seed %d:前沿 %d 个点,预算 %.0f 内最优 %v(专注 %.0f)", seed, len(front), budget,
				r.Focus.BudgetPick.Tiers, r.Focus.BudgetPick.FocusTotal)
		}
	}
}

// 批末余料三种估值(设计 §2.4):carry 不改成本、只进本金;bid 按砸买单到手估值,
// 差额摊进成本;zero 整个取得成本摊进成本。余料件数 = count × r,和批量无关。
func Test余料三种估值(t *testing.T) {
	books := Books{}
	setAsks(books, "T5_ORE_LEVEL4@4", "Thetford", 1000, 3, 1200, 100)
	setBids(books, "T5_ORE_LEVEL4@4", "Thetford", 800, 1, 700, 50)
	setAsks(books, "T4_METALBAR_LEVEL4@4", "Thetford", 5000, 100)
	setBids(books, "T4_METALBAR_LEVEL4@4", "Thetford", 4000, 100)
	setAsks(books, "T5_METALBAR_LEVEL4@4", "Thetford", 30000, 5)
	s := strategy{raw: rawMixed, depth: 1, focus: setOf(5)}
	var carry *evalResult
	for _, mode := range []string{LeftoverCarry, LeftoverBid, LeftoverZero} {
		p := params("T5_METALBAR_LEVEL4@4")
		p.Qty = 2
		p.Leftover = mode
		e := mustEngine(t, Input{Params: p, Books: books})
		ev := e.run(s, nil)
		r := e.rrr(true)
		if len(ev.leftovers) != 2 {
			t.Fatalf("%s:余料 %+v", mode, ev.leftovers)
		}
		ore, bar := ev.leftovers[0], ev.leftovers[1]
		if ore.Item != "T5_ORE_LEVEL4@4" || bar.Item != "T4_METALBAR_LEVEL4@4" {
			t.Fatalf("%s:余料 %+v", mode, ev.leftovers)
		}
		assertNear(t, "余矿件数", ore.Qty, 3*r, 1e-9)
		assertNear(t, "余条件数", bar.Qty, r, 1e-9)
		// 稳态用掉 2×3×(1−r) = 2.77 个矿:1000 那档还剩 0.23 件,余料接着往 1200 吃
		used := 2 * 3 * (1 - r)
		wantOreCost := (3-used)*1000 + (ore.Qty-(3-used))*1200
		assertNear(t, "余矿取得成本", ore.Cost, wantOreCost, 1e-6)
		assertNear(t, "余条取得成本", bar.Cost, bar.Qty*5000, 1e-6)
		switch mode {
		case LeftoverCarry:
			carry = ev
			assertNear(t, "carry 损失", ev.tailLoss, 0, 1e-9)
		case LeftoverBid:
			wantOre := (800*1 + (ore.Qty-1)*700) * 0.96
			assertNear(t, "余矿砸买单", ore.Value, wantOre, 1e-6)
			assertNear(t, "余条砸买单", bar.Value, bar.Qty*4000*0.96, 1e-6)
			assertNear(t, "bid 损失", ev.tailLoss, ore.Cost+bar.Cost-ore.Value-bar.Value, 1e-6)
		case LeftoverZero:
			assertNear(t, "zero 损失", ev.tailLoss, ore.Cost+bar.Cost, 1e-6)
		}
		// 稳态成本和估值方式无关
		assertNear(t, "稳态成本", ev.total, carry.total, 1e-6)
		c := e.candidate(ev, e.sellInfo(), nil, "")
		assertNear(t, "单根成本", c.UnitCost, (ev.total+ev.tailLoss)/2, 1e-6)
		assertNear(t, "开工本金", c.UpfrontCapital, ev.total+ore.Cost+bar.Cost, 1e-6)

		// include_tail=false:余料不列、不进本金、不进成本
		p.IncludeTail = false
		e2 := mustEngine(t, Input{Params: p, Books: books})
		c2 := e2.candidate(e2.run(s, nil), e2.sellInfo(), nil, "")
		if len(c2.Leftover) != 0 || math.Abs(c2.UpfrontCapital-carry.total) > 1e-6 || math.Abs(c2.UnitCost-carry.total/2) > 1e-6 {
			t.Fatalf("%s,include_tail=false:%+v", mode, c2)
		}
	}
}

// 深度不够时 Filled=false 并带警告;完全没有挂单时是「缺价」。
func Test深度不够和缺价(t *testing.T) {
	books := Books{}
	setAsks(books, "T5_ORE_LEVEL4@4", "Thetford", 1000, 2) // 做 5 根要 ≈6.9 个
	setAsks(books, "T4_METALBAR_LEVEL4@4", "Thetford", 5000, 100)
	setAsks(books, "T5_METALBAR_LEVEL4@4", "Thetford", 30000, 5)
	p := params("T5_METALBAR_LEVEL4@4")
	p.Qty = 5
	p.Top = 100
	res, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Candidates {
		if c.Filled {
			t.Fatalf("%s 不该拿满", c.ID)
		}
		if !hasWarning(c.Warnings, WarnDepthShort, "T5_ORE_LEVEL4@4") {
			t.Fatalf("%s 缺深度不够的警告: %+v", c.ID, c.Warnings)
		}
	}

	// 下级料一件都没有,也做不出来(T3 条缺价):缺价
	delete(books, Key{"T4_METALBAR_LEVEL4@4", "Thetford"})
	setAsks(books, "T5_ORE_LEVEL4@4", "Thetford", 1000, 200)
	res, err = Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	c := findCandidate(t, res, "mixed|make1|focus:5")
	if c.Filled || !hasWarning(c.Warnings, WarnMissing, "T4_METALBAR_LEVEL4@4") {
		t.Fatalf("下级料缺价: filled=%v %+v", c.Filled, c.Warnings)
	}
	if !contains(res.Data.Missing, "T4_METALBAR_LEVEL4@4") || !contains(res.Data.Missing, "T3_METALBAR") {
		t.Fatalf("data.missing = %v", res.Data.Missing)
	}
}

// 没解锁的 tier 只能买(A10):目标本身没解锁就没有候选,只有警告。
// 解锁是一级一级往上的,所以不会出现「顶层解锁了、下面某级没解锁」。
func Test未解锁的tier只能买(t *testing.T) {
	l := Lines[0]
	books := synthBooks(l, l.City, 5)
	p := params(l.RefinedID(8, 4))
	p.Focus.UnlockedTier = 7
	res, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 0 || !hasWarning(res.Warnings, WarnLocked, l.RefinedID(8, 4)) {
		t.Fatalf("T8 没解锁:候选 %d 条,警告 %+v", len(res.Candidates), res.Warnings)
	}
	if len(res.Chain) != 1 || res.Chain[0].Makeable {
		t.Fatalf("链 %+v", res.Chain)
	}

	// 解锁到 T7 时做 T7.4:链 T7.4 → T6.4 → T5.4 → T4.4 都能做,T3 条只能买
	p = params(l.RefinedID(7, 4))
	p.Focus.UnlockedTier = 7
	e := mustEngine(t, Input{Params: p, Books: books})
	if got := e.focusTiers(); len(got) != 4 || got[0] != 7 || got[3] != 4 {
		t.Fatalf("能开专注的 tier %v", got)
	}
	if last := e.chain[len(e.chain)-1]; last.item != l.RefinedID(3, 0) || last.makeable {
		t.Fatalf("链底 %+v", last)
	}
}

// 有心配方的档(.0~.3):心没有价格时走普通配方;心便宜到比原料划算时走心配方,
// 心按开关返还。
func Test心配方(t *testing.T) {
	l := Lines[0]
	target := l.RefinedID(5, 2)
	books := Books{}
	setAsks(books, l.RawID(5, 2), "Thetford", 1000, 500)
	setAsks(books, l.RefinedID(4, 2), "Thetford", 2000, 500)
	setAsks(books, target, "Thetford", 9000, 5)
	p := params(target)
	p.Qty = 4
	e := mustEngine(t, Input{Params: p, Books: books})
	ev := e.run(strategy{raw: rawMixed, depth: 1, focus: setOf(5)}, nil)
	if ev.refines[0].heart {
		t.Fatal("心没有价格时不该选心配方")
	}
	// 心 10 银:3 个原料 → 2 个原料 + 1 个心,每次省约 1000 × (1−r)
	setAsks(books, l.Heart, "Thetford", 10, 500)
	for _, returns := range []bool{true, false} {
		p.HeartReturns = returns
		e = mustEngine(t, Input{Params: p, Books: books})
		ev = e.run(strategy{raw: rawMixed, depth: 1, focus: setOf(5)}, nil)
		rf := ev.refines[0]
		if !rf.heart {
			t.Fatalf("心便宜时应当选心配方: %+v", rf)
		}
		r := e.rrr(true)
		want := 4 * (1 - r)
		if !returns {
			want = 4
		}
		var heartQty float64
		for _, in := range rf.inputs {
			if in.Item == l.Heart {
				heartQty = in.Qty
			}
		}
		assertNear(t, "心的消耗", heartQty, want, 1e-9)
		c := e.candidate(ev, e.sellInfo(), nil, "")
		if returns != hasLeftover(c, l.Heart) {
			t.Fatalf("心返还=%v 时余料里有没有心: %+v", returns, c.Leftover)
		}
	}
}

func hasWarning(ws []Warning, code, item string) bool {
	for _, w := range ws {
		if w.Code == code && (item == "" || w.Item == item) {
			return true
		}
	}
	return false
}

func hasLeftover(c Candidate, item string) bool {
	for _, lo := range c.Leftover {
		if lo.Item == item {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x || strings.HasPrefix(v, x+"|") {
			return true
		}
	}
	return false
}

func findCandidate(t *testing.T, res *Result, id string) Candidate {
	t.Helper()
	for _, c := range res.Candidates {
		if c.ID == id {
			return c
		}
	}
	var ids []string
	for _, c := range res.Candidates {
		ids = append(ids, c.ID)
	}
	t.Fatalf("没有候选 %s,有 %v", id, ids)
	return Candidate{}
}
