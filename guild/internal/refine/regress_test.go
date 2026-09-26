package refine

import (
	"strconv"
	"testing"
	"time"
)

// 真实抓包回归(设计 §4.7 第 4 条):断言 design-worked_example.md 里的数,误差 ±1 银。
//
// 数据是 2026-09-26 Thetford / Martlock 的抓包(testdata/local,不进仓库,没有就跳过);
// 实例的原型脚本是 scratchpad 的 rf_calc.py / rf_calc2.py / rf_ore_table.py / rf_n3.py。
// 口径:回收率开专注 117/217、不开 58/158;站费 500;转化实付 = 基础 × k + 产物 IV × 0.5625;
// FCE = 0;买料吃卖单。
//
// 和原型的一处差别要说明:Thetford 没有 T3 钢条的挂单,原型链底的 T3 条「借 Martlock 价」
// (rf_calc.py 第 136 行)。这里同样借:把 Martlock 的 T3 条盘口标成 borrowed 放进 Thetford。
// 不借的话 T4.4 条做不出来只能买,全链自动的成本会高 451 银(见 Test不借T3条价时T4点4只能买)。

const (
	// T8.4 钢条的几个方案(实例 §2)
	idA    = "mixed|make1|focus:8"        // 只顶层开专注,T7.4 条买现成
	idB    = "mixed|make2|focus:8"        // 顶层专注,T7.4 自炼不开专注,T6.4 买
	idC    = "mixed|make2|focus:8,7"      // 顶层和 T7.4 开专注,T6.4 买
	idD    = "mixed|auto|focus:8,7,6,5,4" // 全链开专注,自动比价
	idF    = "mixed|make1|focus:"         // 全不开专注,T7.4 条买
	idG    = "mixed|auto|focus:"          // 全不开专注,自动比价
	oreT84 = "T8_ORE_LEVEL4@4"
	barT84 = "T8_METALBAR_LEVEL4@4"
)

var scenario = map[string]strategy{
	idA: {raw: rawMixed, depth: 1, focus: setOf(8)},
	idB: {raw: rawMixed, depth: 2, focus: setOf(8)},
	idC: {raw: rawMixed, depth: 2, focus: setOf(8, 7)},
	idD: {raw: rawMixed, depth: 0, focus: setOf(8, 7, 6, 5, 4)},
	idF: {raw: rawMixed, depth: 1, focus: 0},
	idG: {raw: rawMixed, depth: 0, focus: 0},
}

// oreBooks 是抓包盘口,外加「借 Martlock 价」的 T3 钢条(见文件头)。
func oreBooks(d *captureDump) Books {
	books := d.books()
	if b, ok := books[Key{"T3_METALBAR", "Martlock"}]; ok {
		b.Asks.Source = "borrowed:Martlock"
		books[Key{"T3_METALBAR", "Thetford"}] = b
	}
	return books
}

// thetfordVolume 是实例 §3 的成交量:Thetford T8.4 钢条 AODP 14 天成交 65 根,8 天有数据。
var thetfordVolume = Volume{PerDay: 65.0 / 14, Days: 14, DaysWithData: 8, Source: "aodp", Known: true}

func captureEngine(t *testing.T, books Books, target string, k float64, mut func(*Params)) *engine {
	t.Helper()
	p := params(target)
	p.Fees.TransmuteFactor = k
	if mut != nil {
		mut(&p)
	}
	return mustEngine(t, Input{Params: p, Books: books, Volume: thetfordVolume})
}

// 实例 §2 的表:各方案的单根成本,两种 k。
func Test真实抓包T8点4钢条各方案单根成本(t *testing.T) {
	d := loadCapture(t)
	books := oreBooks(d)
	want := map[float64]map[string]float64{
		1.0:   {idA: 2_640_928, idB: 2_719_399, idC: 2_585_444, idD: 2_564_096, idF: 3_626_238, idG: 3_626_238},
		1.154: {idA: 2_961_259, idB: 3_052_196, idC: 2_914_852, idD: 2_900_775, idF: 4_074_155, idG: 4_074_155},
	}
	// 挂卖利 / 根(实例 §2):A、D、F
	wantPost := map[float64]map[string]float64{
		1.0:   {idA: 407_170, idD: 484_003, idF: -578_140},
		1.154: {idA: 86_839, idD: 147_323, idF: -1_026_057},
	}
	for k, rows := range want {
		e := captureEngine(t, books, barT84, k, nil)
		si := e.sellInfo()
		assertNear(t, "挂卖到手", si.Post.Net, 3_048_098, 1)
		for id, w := range rows {
			ev := e.evaluate(scenario[id])
			if !ev.filled {
				t.Fatalf("k=%v %s 没拿满: %+v", k, id, ev.warnings)
			}
			assertNear(t, "k="+ftoa(k)+" "+id+" 单根成本", e.unitCost(ev), w, 1)
			if wp, ok := wantPost[k][id]; ok {
				c := e.candidate(ev, si, nil, "")
				assertNear(t, "k="+ftoa(k)+" "+id+" 挂卖利", c.Profit.Post.Unit, wp, 1)
			}
		}
		// 链上专注(基础值,10 根):A 47,140;C 59,555;D 63,911
		for id, w := range map[string]float64{idA: 47_140, idC: 59_555, idD: 63_911} {
			assertNear(t, id+" 链上专注", e.evaluate(scenario[id]).focusBase, w, 1)
		}
	}
}

// 实例 §1:8.4 矿的来路。需求 23.04 个(10 根 × 5 × (1 − 0.539))。
func Test真实抓包8点4矿来路(t *testing.T) {
	d := loadCapture(t)
	books := oreBooks(d)
	need := 10 * 5 * (1 - ReturnRate(117))
	assertNear(t, "需求量", need, 23.0415, 1e-4)
	pure := map[float64]map[string]float64{
		1.0: {oreT84: 1_166_917, "T8_ORE_LEVEL3@3": 1_017_363, "T8_ORE_LEVEL2@2": 976_463, "T8_ORE_LEVEL1@1": 969_461,
			"T8_ORE": 966_707, "T7_ORE": 965_164, "T7_ORE_LEVEL4@4": 1_059_851, "T7_ORE_LEVEL3@3": 993_315},
		1.154: {oreT84: 1_166_917, "T8_ORE_LEVEL3@3": 1_132_623, "T8_ORE_LEVEL2@2": 1_114_775, "T8_ORE_LEVEL1@1": 1_114_758,
			"T8_ORE": 1_114_222, "T7_ORE": 1_113_449, "T7_ORE_LEVEL4@4": 1_179_913, "T7_ORE_LEVEL3@3": 1_132_587},
	}
	for k, rows := range pure {
		e := captureEngine(t, books, barT84, k, nil)
		got := map[string]PureRoute{}
		for _, pr := range e.pureRoutes(oreT84, need) {
			got[pr.Start] = pr
		}
		for start, w := range rows {
			pr, ok := got[start]
			if !ok || pr.Unit == nil {
				t.Fatalf("k=%v 从 %s 纯转化:%+v", k, start, pr)
			}
			assertNear(t, "k="+ftoa(k)+" 纯路线 "+start, *pr.Unit, w, 1)
		}
	}

	// 混合最优:k=1.0 全部从 7.0 转;k=1.154 是 8.4 直接买 3 件 + 从 7.0 转 20.04 件
	mixed := func(k float64, x float64, cities ...string) (float64, map[string]float64) {
		e := captureEngine(t, books, barT84, k, func(p *Params) {
			if len(cities) > 0 {
				p.Sources.Cities = cities
			}
		})
		st := &state{e: e, used: map[offerKey]float64{}, res: &evalResult{missing: map[string]bool{}}}
		takes, total, got := st.allocate(e.rawOffers(oreT84), x)
		if got < x-qtyEps {
			t.Fatalf("k=%v 混合最优没拿满", k)
		}
		split := map[string]float64{}
		for _, tk := range takes {
			split[tk.o.key.city+"|"+tk.o.key.item] += tk.qty
		}
		return total / x, split
	}
	u, split := mixed(1.0, need)
	assertNear(t, "k=1.0 混合最优", u, 965_164, 1)
	assertNear(t, "k=1.0 从 7.0 转", split["Thetford|T7_ORE"], need, 1e-9)

	u, split = mixed(1.154, need)
	assertNear(t, "k=1.154 混合最优", u, 1_104_188, 1)
	if len(split) != 2 {
		t.Fatalf("k=1.154 拆分 %v", split)
	}
	assertNear(t, "直接买 8.4", split["Thetford|"+oreT84], 3, 1e-9)
	assertNear(t, "从 7.0 转", split["Thetford|T7_ORE"], 20.0415, 1e-4)

	// 需求量改变单价(k=1.154):5 件 1,070,771;50 件 1,109,182
	u, _ = mixed(1.154, 5)
	assertNear(t, "5 件混合", u, 1_070_771, 1)
	u, _ = mixed(1.154, 50)
	assertNear(t, "50 件混合", u, 1_109_182, 1)

	// 允许去 Martlock 买料(运费 0):最优是 Martlock 的 8.1 矿,均价 1,103,087,只省 0.1%
	u, split = mixed(1.154, need, "Thetford", "Martlock")
	assertNear(t, "跨城混合", u, 1_103_087, 1)
	assertNear(t, "Martlock 8.1", split["Martlock|T8_ORE_LEVEL1@1"], 20.0415, 1e-4)

	// 挂买单收 8.4 矿:(900,037 + 1) × 1.025 = 922,538.95
	e := captureEngine(t, books, barT84, 1.154, nil)
	o, ok := e.bidOffer(oreT84)
	if !ok {
		t.Fatal("8.4 矿没有买单")
	}
	assertNear(t, "挂买单收 8.4 矿", o.eff, 922_539, 0.5)
}

// 实例 §1 的 k 断点:k 低于第一个断点时全部转化,两个断点之间卖一那 3 件直接买,
// 高于第二个断点时前 8 件直接买。
//
// 实例写的是「约 1.09」和「约 1.185」。rf_ore_table.py 是按 0.01 / 0.005 的步长扫、
// 而且只拿「7.0 一路转」和 8.4 的挂单比,精确值和它有出入:
//   - 第一个断点 = (1,042,319 − 2,276.4) / 962,888 = **1.0801**(不是 1.09:步长 0.01 扫到的第一个越过点)。
//     962,888 是 7.0 → 8.4 五步的基础价合计,2,276.4 是 7.0 矿 1,830 加五步主人费用 446.4
//   - k 超过 1.1744 以后,转化的那部分不再从 7.0 来,而是从 8.2 矿(77,985)转两步:
//     离目标近的起点基础价合计小(898,128),k 越大越占便宜
//   - 所以第二个断点是 8.4 第 4 件(1,142,320)顶掉 8.2 那条路最贵的一件的地方。要转的 20.04 件
//     吃到 8.2 矿 77,992 那一档(77,985×1、77,986×6、77,990×6、77,992×14):
//     (1,142,320 − 77,992 − 345.6) / 898,128 = **1.1847**。拿 7.0 那条算是 1.1840,
//     实例的「约 1.185」碰巧接近
func Test真实抓包k断点(t *testing.T) {
	d := loadCapture(t)
	books := oreBooks(d)
	need := 10 * 5 * (1 - ReturnRate(117))
	direct := func(k float64) float64 {
		e := captureEngine(t, books, barT84, k, nil)
		st := &state{e: e, used: map[offerKey]float64{}, res: &evalResult{missing: map[string]bool{}}}
		takes, _, _ := st.allocate(e.rawOffers(oreT84), need)
		n := 0.0
		for _, tk := range takes {
			if tk.o.key.item == oreT84 {
				n += tk.qty
			}
		}
		return n
	}
	if direct(1.0) != 0 || direct(1.154) != 3 || direct(1.2) != 8 {
		t.Fatalf("直接买件数 k=1.0:%v k=1.154:%v k=1.2:%v", direct(1.0), direct(1.154), direct(1.2))
	}
	bisect := func(lo, hi, below float64) float64 {
		for i := 0; i < 50; i++ {
			mid := (lo + hi) / 2
			if direct(mid) <= below {
				lo = mid
			} else {
				hi = mid
			}
		}
		return (lo + hi) / 2
	}
	k1 := bisect(1.0, 1.154, 0)
	k2 := bisect(1.154, 1.3, 3)
	assertNear(t, "第一个 k 断点", k1, (1_042_319-2_276.4)/962_888.0, 1e-6)
	assertNear(t, "第二个 k 断点", k2, (1_142_320-77_992-345.6)/898_128.0, 1e-7)
	assertNear(t, "第一个 k 断点(约)", k1, 1.0801, 1e-4)
	assertNear(t, "第二个 k 断点(约)", k2, 1.1847, 1e-4)

	// 转化的起点从 7.0 换成 8.2 的地方:(77,985 + 345.6 − 2,276.4) / (962,888 − 898,128) = 1.1744
	source := func(k float64) string {
		e := captureEngine(t, books, barT84, k, nil)
		st := &state{e: e, used: map[offerKey]float64{}, res: &evalResult{missing: map[string]bool{}}}
		takes, _, _ := st.allocate(e.rawOffers(oreT84), need)
		return takes[len(takes)-1].o.key.item
	}
	if source(1.17) != "T7_ORE" || source(1.18) != "T8_ORE_LEVEL2@2" {
		t.Fatalf("k=1.17 转化起点 %s,k=1.18 转化起点 %s", source(1.17), source(1.18))
	}
}

// 实例 §2、§3:出售、每点专注、数量效应、本金、日化、挂买单参照行。
func Test真实抓包出售专注本金日化(t *testing.T) {
	d := loadCapture(t)
	books := oreBooks(d)

	// 吃买单:卖到第 8 根后断崖到 105,565;前 6 根到手均价 2,848,000
	e := captureEngine(t, books, barT84, 1.154, nil)
	si := e.sellInfo()
	if si.Take == nil || si.Take.Cliff == nil || si.Take.Cliff.Price != 105_565 || *si.Take.MaxQty != 8 {
		t.Fatalf("断崖: %+v", si.Take)
	}
	assertNear(t, "10 根只能出", si.Take.Fill.Got, 8, 1e-9)
	e6 := captureEngine(t, books, barT84, 1.154, func(p *Params) { p.Qty = 6 })
	assertNear(t, "前 6 根吃买单到手均价", e6.sellInfo().Take.Fill.NetAvg, 2_848_000, 1)

	// 每点专注(不开专注本身就亏,所以就是 利润 ÷ 专注):
	// k=1.154 A 18.4、D 23.1;k=1.0 A 86.4、D 75.7
	for k, rows := range map[float64]map[string]float64{1.154: {idA: 18.4, idD: 23.1}, 1.0: {idA: 86.4, idD: 75.7}} {
		e := captureEngine(t, books, barT84, k, nil)
		si := e.sellInfo()
		for _, s := range scenario {
			e.evaluate(s)
		}
		best := e.bestNoFocusProfit(si)
		if best == nil || *best > 0 {
			t.Fatalf("k=%v 不开专注的最好利润应当是负的: %v", k, best)
		}
		for id, w := range rows {
			c := e.candidate(e.evaluate(scenario[id]), si, best, "")
			assertNear(t, "k="+ftoa(k)+" "+id+" 银/专注点", *c.SilverPerFocus, w, 0.05)
		}
	}

	// 数量效应(k=1.154,方案 A):1 根 2,818,703;3 根 2,911,467;10 根 2,961,259
	for n, w := range map[int]float64{1: 2_818_703, 3: 2_911_467, 10: 2_961_259} {
		e := captureEngine(t, books, barT84, 1.154, func(p *Params) { p.Qty = n })
		assertNear(t, "A 做 "+itoa(n)+" 根", e.unitCost(e.evaluate(scenario[idA])), w, 1)
	}
	// N=1 全链专注(spreadsheet_diff):2,758,220
	e1 := captureEngine(t, books, barT84, 1.154, func(p *Params) { p.Qty = 1 })
	assertNear(t, "D 做 1 根", e1.unitCost(e1.evaluate(scenario[idD])), 2_758_220, 1)

	// 本金(实例 §3):一批 3 根要先备 9.61 个 8.4 矿和 1.92 根 T7.4,约 1,222 万;
	// 批末余 2.70 个矿 + 0.54 根条。余料马上砸买单每批要多亏约 70~90 万
	for _, mode := range []string{LeftoverCarry, LeftoverBid} {
		e3 := captureEngine(t, books, barT84, 1.154, func(p *Params) { p.Qty = 3; p.Leftover = mode })
		ev := e3.run(scenario[idA], nil)
		c := e3.candidate(ev, e3.sellInfo(), nil, "")
		r := ReturnRate(117)
		assertNear(t, "余矿", c.Leftover[0].Qty, 5*r, 1e-9)
		assertNear(t, "余条", c.Leftover[1].Qty, r, 1e-9)
		var oreBuy, barBuy float64
		for _, b := range ev.buys {
			switch b.forItem {
			case oreT84:
				oreBuy += b.qty
			case "T7_METALBAR_LEVEL4@4":
				barBuy += b.qty
			}
		}
		assertNear(t, "开工要备的矿", oreBuy+c.Leftover[0].Qty, 9.61, 0.005)
		assertNear(t, "开工要备的条", barBuy+c.Leftover[1].Qty, 1.92, 0.005)
		if c.UpfrontCapital < 12_200_000 || c.UpfrontCapital > 12_250_000 {
			t.Fatalf("%s:开工本金 %.0f,实例说约 1,222 万", mode, c.UpfrontCapital)
		}
		if mode == LeftoverBid && (ev.tailLoss < 700_000 || ev.tailLoss > 900_000) {
			t.Fatalf("余料砸买单每批多亏 %.0f,实例说约 70~90 万", ev.tailLoss)
		}
	}

	// 日化(实例 §3):成交量卡住,方案 D 每天约 1.4 根 × 14.7 万 ≈ 20.5 万;
	// 专注上限:FCE=0 每天 10,000 专注,A 能做 2.12 根、D 1.56 根
	cD := e.candidate(e.evaluate(scenario[idD]), si, nil, "")
	if cD.Daily == nil || cD.Daily.Binding != "volume" {
		t.Fatalf("D 的日化: %+v", cD.Daily)
	}
	assertNear(t, "D 每天件数", cD.Daily.Units, 0.3*65/14, 1e-9)
	assertNear(t, "D 日化", cD.Daily.Profit, 205_200, 1)
	assertNear(t, "D 专注上限", *cD.Daily.Limits.Focus, 1.5647, 1e-4)
	cA := e.candidate(e.evaluate(scenario[idA]), si, nil, "")
	assertNear(t, "A 专注上限", *cA.Daily.Limits.Focus, 10000.0/4714, 1e-9)

	// 挂买单参照行(实例 A′):8.4 矿按 922,538.95 收,k 不影响。
	// 实例写 2,542,715,是手算时的舍入;逐项精确算是 (23.0415 × 922,538.95 + 4,147,432.5 + 23,040) / 10
	// = 2,542,712.7
	for _, k := range []float64{1.0, 1.154} {
		e := captureEngine(t, books, barT84, k, nil)
		ev := e.evaluate(strategy{raw: rawBid, depth: 1, focus: setOf(8)})
		assertNear(t, "挂买单参照行", e.unitCost(ev), 2_542_712.7, 0.5)
	}
}

// 实例 §4:Martlock 皮革线(T8.4 皮革,挂卖到手 3,319,240)。
func Test真实抓包Martlock皮革(t *testing.T) {
	d := loadCapture(t)
	books := d.books()
	target := "T8_LEATHER_LEVEL4@4"
	want := map[float64]map[string]float64{
		1.0:   {idA: 2_681_627, idB: 2_744_928, idC: 2_605_076, idD: 2_569_340, idF: 3_682_134},
		1.154: {idA: 3_023_509, idB: 3_146_893, idC: 2_990_580, idD: 2_962_121, idF: 4_151_682},
	}
	wantPost := map[float64]map[string]float64{
		1.0:   {idA: 637_613, idD: 749_900, idF: -362_894},
		1.154: {idA: 295_730, idD: 357_119, idF: -832_442},
	}
	for k, rows := range want {
		e := captureEngine(t, books, target, k, nil)
		if e.p.City != "Martlock" {
			t.Fatalf("皮革默认生产城应当是 Martlock,得到 %s", e.p.City)
		}
		si := e.sellInfo()
		assertNear(t, "皮革挂卖到手", si.Post.Net, 3_319_240, 1)
		for id, w := range rows {
			ev := e.evaluate(scenario[id])
			if !ev.filled {
				t.Fatalf("k=%v %s 没拿满: %+v", k, id, ev.warnings)
			}
			assertNear(t, "皮革 k="+ftoa(k)+" "+id, e.unitCost(ev), w, 1)
			if wp, ok := wantPost[k][id]; ok {
				assertNear(t, "皮革 k="+ftoa(k)+" "+id+" 挂卖利", e.candidate(ev, si, nil, "").Profit.Post.Unit, wp, 1)
			}
		}
		// 8.4 皮:两种 k 下都是转化更便宜(实例 §4:966,832 / 1,115,209)
		need := 10 * 5 * (1 - ReturnRate(117))
		st := &state{e: e, used: map[offerKey]float64{}, res: &evalResult{missing: map[string]bool{}}}
		_, total, _ := st.allocate(e.rawOffers("T8_HIDE_LEVEL4@4"), need)
		assertNear(t, "8.4 皮混合", total/need, map[float64]float64{1.0: 966_832, 1.154: 1_115_209}[k], 1)
	}
}

// 真实抓包上的多目标分配:全链开专注时 8.4 / 7.4 / 6.4 / 5.4 / 4.4 五种矿共用 Thetford 的挂单,
// 「从高往下依次分配」和最小费用流的精确解比,偏差不超过 0.1%(设计 §2.3 ① 的换算门槛)。
func Test真实抓包多目标分配和最小费用流对照(t *testing.T) {
	d := loadCapture(t)
	for _, c := range []struct {
		books        Books
		target, city string
	}{
		{oreBooks(d), barT84, "Thetford"},
		{d.books(), "T8_LEATHER_LEVEL4@4", "Martlock"},
		// 纤维、木的专精城没抓包,借 Thetford / Martlock 的盘口只测分配
		{d.books(), "T8_CLOTH_LEVEL2@2", "Thetford"},
		{d.books(), "T7_PLANKS_LEVEL3@3", "Martlock"},
	} {
		for _, k := range []float64{1.0, 1.154} {
			e := captureEngine(t, c.books, c.target, k, func(p *Params) {
				p.City, p.Sell.City, p.Sources.Cities = c.city, c.city, []string{c.city}
			})
			all := setOf(e.focusTiers()...)
			for _, s := range []strategy{{raw: rawMixed, depth: 0, focus: all}, {raw: rawMixed, depth: 0, focus: 0}} {
				ev := e.run(s, nil)
				demand := map[string]float64{}
				greedy := 0.0
				filled := true
				for _, u := range ev.rawUses {
					demand[u.target] += u.need
					for _, tk := range u.takes {
						greedy += tk.qty * tk.o.eff
					}
					filled = filled && u.got >= u.need-qtyEps
				}
				if !filled || len(demand) == 0 {
					continue
				}
				exact, ok := exactRawCost(e, demand)
				if !ok {
					t.Fatalf("%s k=%v:最小费用流不可行", c.target, k)
				}
				gap := greedy/exact - 1
				if gap < -1e-9 || gap > 0.001 {
					t.Fatalf("%s k=%v %s:依次分配 %.1f,精确 %.1f,偏差 %.4f%%", c.target, k, s.id(), greedy, exact, gap*100)
				}
				t.Logf("%s k=%v %s:%d 种原料,偏差 %.6f%%", c.target, k, s.id(), len(demand), gap*100)
			}
		}
	}
}

// 不借 Martlock 的 T3 条价:Thetford 没有 T3 条,T4.4 条做不出来只能买,
// 全链自动比 worked example 高 451 银(= 0.451 根 T4.4 条 × (买 43,310 − 自炼 33,302))。
func Test不借T3条价时T4点4只能买(t *testing.T) {
	d := loadCapture(t)
	e := captureEngine(t, d.books(), barT84, 1.154, nil)
	ev := e.run(scenario[idD], nil)
	if !ev.filled {
		t.Fatalf("没拿满: %+v", ev.warnings)
	}
	assertNear(t, "全链自动(不借 T3)", e.unitCost(ev), 2_901_226, 1)
	if !ev.missing["T3_METALBAR"] {
		t.Fatal("应当记下 T3 条缺价")
	}
	for _, r := range ev.refines {
		if r.tier == 4 {
			t.Fatal("没有 T3 条时不该精炼 T4.4")
		}
	}
}

// 整个 Plan 在真实抓包上的形状:推荐、参照行、原料来路、警告、新鲜度。
func Test真实抓包Plan输出(t *testing.T) {
	d := loadCapture(t)
	p := params(barT84)
	res, err := Plan(Input{Params: p, Books: oreBooks(d), Recipes: EmbeddedRecipes(), Volume: thetfordVolume})
	if err != nil {
		t.Fatal(err)
	}
	if res.Candidates[0].ID != idD {
		t.Fatalf("按日化排第一的应当是全链专注自动比价,得到 %s", res.Candidates[0].ID)
	}
	assertNear(t, "推荐单根成本", res.Candidates[0].UnitCost, 2_900_775, 1)
	var refs []string
	for _, c := range res.Candidates {
		if c.Reference != "" {
			refs = append(refs, c.Reference+":"+c.ID)
		}
	}
	if len(refs) != 2 || refs[0] != "market:pure:"+oreT84+"|make1|focus:8" || refs[1] != "bid:bid|make1|focus:8" {
		t.Fatalf("参照行 %v", refs)
	}
	rs, ok := res.RawSources[oreT84]
	if !ok || rs.Mixed.Unit == nil {
		t.Fatalf("原料来路里没有 8.4 矿: %+v", res.RawSources)
	}
	assertNear(t, "原料来路:8.4 矿需求", rs.Need, 23.0415, 1e-4)
	assertNear(t, "原料来路:混合最优", *rs.Mixed.Unit, 1_104_188, 1)
	if rs.Pure[0].Start != "T7_ORE" || len(rs.Mixed.Split) != 2 {
		t.Fatalf("原料来路: %+v", rs)
	}
	if !hasWarning(res.Warnings, WarnSellCliff, barT84) || !hasWarning(res.Warnings, WarnThinVolume, barT84) {
		t.Fatalf("计划级警告: %+v", res.Warnings)
	}
	if res.Data.OldestAgeH == nil || *res.Data.OldestAgeH > 1 || res.Candidates[0].AgeLevel != "ok" {
		t.Fatalf("新鲜度: %+v %s", res.Data.OldestAgeH, res.Candidates[0].AgeLevel)
	}
	if !hasWarning(res.Candidates[0].Warnings, WarnUnverified, "") {
		t.Fatalf("推荐路线用到了没核实的 k 和站费,应当有警告: %+v", res.Candidates[0].Warnings)
	}
	// 六小时后再算:价格年龄跟着 Now 走,超过 6 小时标黄
	p.Now = testNow.Add(6 * time.Hour)
	res, err = Plan(Input{Params: p, Books: oreBooks(d), Recipes: EmbeddedRecipes(), Volume: thetfordVolume})
	if err != nil {
		t.Fatal(err)
	}
	if res.Candidates[0].AgeLevel != "yellow" || !hasWarning(res.Candidates[0].Warnings, WarnStale, "") {
		t.Fatalf("6 小时后: %s %+v", res.Candidates[0].AgeLevel, res.Candidates[0].Warnings)
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func itoa(n int) string { return strconv.Itoa(n) }
