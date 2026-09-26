package refine

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"albion-guild/internal/catalog"
	"albion-guild/internal/depth"
)

// 半成品链(设计 §2.3 ②③)。
//
// 顶层产物一定是自己炼的(买现成的再卖是倒货,不是精炼)。往下每一级 P(t,e) 需求 Q 件:
// 在「买 k 件 + 自炼 Q−k 件」里取最小。买的成本沿卖单阶梯是凸分段线性;自炼的成本
// = 原料曲线 + 下级料曲线 + 站费,也是凸分段线性(见 curve.go)。两条凸曲线合并、取最便宜的
// Q 件就是精确的拆分。
//
// 原料的分配从顶层往下做:先分顶层的原料,再定下一级的买 / 做,再分下一级的原料……
// 下一级的「自炼曲线」按当时剩下的挂单算,不计入它自己这一级马上要分走的原料
// (同一级的原料和更下一级的原料共用挂单时,这是近似);报出来的成本一律按实际分配算。

// engine 是一次 Plan 调用的上下文:参数、配方图、整理好的盘口和各种缓存。
// 只在一次调用里用,不跨调用共享,所以不需要锁。
type engine struct {
	p      Params
	line   Line
	g      *graph
	mkt    Market
	books  Books
	chain  []chainLevel
	volume Volume

	sides      map[sideKey]sideEntry
	stale      map[string]bool // 超过 MaxAgeHours、当作缺价的盘口(物品|城市|方向)
	rawCache   map[string][]offer
	buyCache   map[string][]offer
	routeCache map[string]map[string]route
	evalCache  map[string]*evalResult
}

type sideKey struct {
	item, city string
	asks       bool
}

type sideEntry struct {
	l  ladderSide
	ok bool
}

func newEngine(p Params, line Line, in Input) *engine {
	return &engine{
		p: p, line: line, g: newGraph(in.Recipes),
		mkt: MarketFees(p.Fees.economics()), books: in.Books, volume: in.Volume,
		sides: map[sideKey]sideEntry{}, stale: map[string]bool{},
		rawCache: map[string][]offer{}, buyCache: map[string][]offer{},
		routeCache: map[string]map[string]route{}, evalCache: map[string]*evalResult{},
	}
}

// side 取一个盘口边:排好序、去掉空档。比 MaxAgeHours 还旧的档不用(按每一档自己的
// 观测时刻,见 Ladder.LevelSeen);一档都不剩就当作没有,记进 stale。
func (e *engine) side(item, city string, asks bool) (ladderSide, bool) {
	k := sideKey{item, city, asks}
	if s, ok := e.sides[k]; ok {
		return s.l, s.ok
	}
	b, has := e.books[Key{ItemID: item, City: city}]
	var raw Ladder
	if asks {
		raw = b.Asks
	} else {
		raw = b.Bids
	}
	var ent sideEntry
	if has && !raw.empty() {
		l := normalizeLadder(raw, asks)
		fresh := l.keepFresh(func(t time.Time) bool {
			age, known := e.ageHours(t)
			return known && age > e.p.Sources.MaxAgeHours
		})
		switch {
		case len(fresh.levels) > 0:
			ent = sideEntry{l: fresh, ok: true}
		case len(l.levels) > 0:
			e.stale[sideLabel(item, city, asks)] = true
		}
	}
	e.sides[k] = ent
	return ent.l, ent.ok
}

func sideLabel(item, city string, asks bool) string {
	if asks {
		return item + "|" + city + "|卖单"
	}
	return item + "|" + city + "|买单"
}

// ageHours:观测时刻距 Now 多少小时。零值时刻表示调用方没给,年龄未知。
func (e *engine) ageHours(t time.Time) (float64, bool) {
	if t.IsZero() {
		return 0, false
	}
	return math.Max(0, e.p.Now.Sub(t).Hours()), true
}

// rrr 是这一级的回收率。专精加成看生产城市是不是这条线的专精城(C3);
// 个人岛时 City 是岛绑定的城市(C7)。
func (e *engine) rrr(focus bool) float64 {
	return ReturnRate(Bonus{
		Specialty: e.p.City == e.line.City, Focus: focus,
		Daily: e.p.RRR.DailyBonus, Island: e.p.RRR.Island,
	}.Total())
}

// itemKind 是配方材料的种类。
type itemKind int8

const (
	kindUnknown itemKind = iota
	kindRaw
	kindRefined
	kindHeart
)

func (e *engine) kindOf(id string) itemKind {
	if _, ok := heartOf(id); ok {
		return kindHeart
	}
	family, _, _, ok := ParseItemID(id)
	if !ok {
		return kindUnknown
	}
	switch family {
	case e.line.Raw:
		return kindRaw
	case e.line.Refined:
		return kindRefined
	}
	return kindUnknown
}

// factor 是每做 1 件要实际消耗多少件这种材料(稳态,设计 §2.4):
// 参与返还的按 count × (1 − r),不返还的按 count。
// 精炼的原料和下级料都返还(A6 ◐ 高);心返不返还由开关决定(A7 ?)。
func (e *engine) factor(in catalog.RecipeInput, r float64) float64 {
	returns := in.Returns()
	if e.kindOf(in.ItemID) == kindHeart {
		returns = returns && e.p.HeartReturns
	}
	if returns {
		return float64(in.Count) * (1 - r)
	}
	return float64(in.Count)
}

// orderedInputs:原料、心在前,下级料最后——下一级的买 / 做要在这一级的原料分完之后再定。
func (e *engine) orderedInputs(r catalog.Recipe) []catalog.RecipeInput {
	out := append([]catalog.RecipeInput(nil), r.Inputs...)
	sort.SliceStable(out, func(i, j int) bool {
		return e.kindOf(out[i].ItemID) != kindRefined && e.kindOf(out[j].ItemID) == kindRefined
	})
	return out
}

// chainLevel 是链上的一级半成品。
type chainLevel struct {
	item string
	tier int
	// makeable:能自己炼。T3 及以下只当链底的输入(设计 §2.1),没解锁的 tier 只能买(A10)
	makeable bool
	recipes  []catalog.Recipe
	lower    string
}

// buildChain 从目标往下找下级料,直到第一个不能自炼的那一级(只能买,链到此为止)。
func (e *engine) buildChain() {
	item := e.p.Target
	for len(e.chain) < 16 {
		_, t, _, _ := ParseItemID(item)
		rs := e.g.refineRecipes(item)
		lv := chainLevel{item: item, tier: t, recipes: rs}
		lv.makeable = t >= MinEnchantTier && t <= e.p.Focus.UnlockedTier && len(rs) > 0
		if len(rs) > 0 {
			for _, in := range rs[0].Inputs {
				if e.kindOf(in.ItemID) == kindRefined {
					lv.lower = in.ItemID
				}
			}
		}
		e.chain = append(e.chain, lv)
		if !lv.makeable || lv.lower == "" {
			return
		}
		item = lv.lower
	}
}

// focusTiers 是链上能开专注的 tier,从顶层往下。
func (e *engine) focusTiers() []int {
	var out []int
	for _, lv := range e.chain {
		if lv.makeable {
			out = append(out, lv.tier)
		}
	}
	return out
}

// tierSet 是开专注的 tier 集合,按位存(bit t)。
type tierSet uint16

func (s tierSet) has(t int) bool { return s&(1<<t) != 0 }

func setOf(tiers ...int) tierSet {
	var s tierSet
	for _, t := range tiers {
		s |= 1 << t
	}
	return s
}

// list 从高到低列出集合里的 tier。
func (s tierSet) list() []int {
	out := []int{}
	for t := MaxTier; t >= 0; t-- {
		if s.has(t) {
			out = append(out, t)
		}
	}
	return out
}

func (s tierSet) String() string {
	parts := make([]string, 0, 5)
	for _, t := range s.list() {
		parts = append(parts, strconv.Itoa(t))
	}
	return strings.Join(parts, ",")
}

// rawKind 是顶层原料的拿法(设计 §2.3 ⑤)。下面几级的原料一律用混合最优。
type rawKind int8

const (
	rawMixed rawKind = iota // 每档按等效价排序取最便宜的
	rawPure                 // 全部从一个起点买、再转化上来
	rawBid                  // 挂买单收
)

// strategy 是一条候选路线的开法。
type strategy struct {
	raw   rawKind
	start string // rawPure 的起点
	// depth:0 = 每一级自动比价;d ≥ 1 = 从顶层往下连续自炼 d 级,第 d+1 级买现成的
	depth int
	focus tierSet
}

func (s strategy) key() string {
	return fmt.Sprintf("%d:%s|%d|%d", s.raw, s.start, s.depth, s.focus)
}

// decision 是某一级的买 / 做。
type decision int8

const (
	decBuy decision = iota
	decMake
	decAuto
)

// ── 一次评估 ────────────────────────────────────────────────────────────

// evalResult 是按一种开法算下来的全部账。
type evalResult struct {
	s      strategy
	filled bool
	// total 是稳态总成本:买料 + 转化费 + 站费(不含批末余料)
	total     float64
	buys      []*buyRec
	trans     map[[2]string]*transRec
	refines   []refineRec
	focusBase float64
	focusEff  float64
	tails     []tailRec
	rawUses   []rawUse
	warnings  []Warning
	missing   map[string]bool

	// 批末余料:tailAcq 是备齐它们要多花的钱(算进开工本金),
	// tailLoss 是按估值方式要摊进成本的损失(carry 时为 0)
	leftovers []Leftover
	tailAcq   float64
	tailLoss  float64
}

type buyRec struct {
	item, city string
	bid        bool
	// forItem 是这批货最后变成什么:原料转化的目标,或者就是它自己
	forItem   string
	qty       float64
	cost      float64 // 成交价 × 买方系数 × 件数
	transport float64
	levels    map[int64]float64
	// seen 是吃到的各档里最旧的观测时刻
	seen   time.Time
	source string
	// hitLast:吃到了可见阶梯的最后一档
	hitLast      bool
	truncated    bool
	depthUnknown bool
}

type transRec struct {
	from, to string
	times    float64
	unitFee  float64
}

type refineRec struct {
	item       string
	tier       int
	crafts     float64
	focusOn    bool
	rrr        float64
	stationFee float64
	focusBase  float64
	focusEff   float64
	variant    int
	heart      bool
	inputs     []StepInput
}

// tailRec 是批末余料:每一级做完一批,总会剩约 count × r 件返还的材料,和批量大小无关(§2.4)。
type tailRec struct {
	item  string
	qty   float64
	level int
	kind  itemKind
}

type rawUse struct {
	target    string
	need, got float64
	takes     []take
}

// state 是一次评估的可变部分:这次计算里各档挂单已经分出去多少。
type state struct {
	e    *engine
	s    strategy
	used map[offerKey]float64
	res  *evalResult
	// forceBuy 只给测试用:第 i 级强制买这么多件(见 acquire)
	forceBuy map[int]float64
}

// evaluate 按一种开法把整条链算一遍。同一种开法只算一次。
func (e *engine) evaluate(s strategy) *evalResult {
	if r, ok := e.evalCache[s.key()]; ok {
		return r
	}
	r := e.run(s, nil)
	e.evalCache[s.key()] = r
	return r
}

// run 是不走缓存的一次评估;force 见 state.forceBuy。
func (e *engine) run(s strategy, force map[int]float64) *evalResult {
	st := &state{e: e, s: s, used: map[offerKey]float64{}, forceBuy: force, res: &evalResult{
		s: s, filled: true, trans: map[[2]string]*transRec{}, missing: map[string]bool{},
	}}
	st.make(0, float64(e.p.Qty))
	st.valueTails()
	return st.res
}

// Leftover 是一项批末余料。
type Leftover struct {
	Item string  `json:"item"`
	Qty  float64 `json:"qty"`
	// Cost 是备齐这些余料的取得成本:在这一批分完之后的阶梯上接着往下吃
	// (返还的料顶掉的就是下一件的购买,所以按边际价计,§2.4)
	Cost float64 `json:"cost"`
	// Value 是按 Mode 的估值:carry = Cost;bid = 砸买单到手;zero = 0
	Value float64 `json:"value"`
	Mode  string  `json:"mode"`
	// Loss = Cost − Value,IncludeTail 时摊进这一批的成本
	Loss float64 `json:"loss"`
}

// valueTails 给批末余料估值(设计 §2.4)。
//
// 稳态消耗已经按「返还的料再投入下一轮」算了,但一批开工前总得先备齐一整份料,
// 做完剩下约 count × r 件,和批量大小无关(T8.4 钢条是 2.70 个矿 + 0.54 根 T7.4)。
// 不照抄表格的「尾常数 +(n−1)」:那样会把启动缓冲重复记成成本(spreadsheet_diff D4)。
// 取得成本按分完这一批之后的边际价算,不再占用挂单:几级的余料加起来只有几件,
// 为这点量去改前面的分配不值得。
func (st *state) valueTails() {
	e := st.e
	byItem := map[string]int{}
	for _, t := range st.res.tails {
		var c curve
		switch t.kind {
		case kindRaw:
			// 和这一级的原料同一个来路:顶层按开法(纯转化 / 挂买单),下面几级混合最优
			c = st.offerCurve(st.rawSource(t.item, t.level == 0), t.qty)
		case kindRefined:
			c = st.nodeCurve(t.level+1, t.qty)
		case kindHeart:
			c = st.buyCurve(t.item, t.qty)
		}
		acq, got, _ := c.cost(t.qty)
		if got < t.qty-qtyEps {
			st.warn(Warning{Code: WarnTailShort, Item: t.item,
				Msg: fmt.Sprintf("批末余料要先备 %s 件 %s,盘口只够 %s 件,开工本金是下界", fmtQty(t.qty), t.item, fmtQty(got))})
		}
		lo := Leftover{Item: t.item, Qty: t.qty, Cost: acq, Mode: e.p.Leftover}
		switch e.p.Leftover {
		case LeftoverCarry:
			lo.Value = acq
		case LeftoverBid:
			lo.Value = st.bidValue(t.item, t.qty)
		}
		lo.Loss = lo.Cost - lo.Value
		if i, ok := byItem[t.item]; ok {
			x := &st.res.leftovers[i]
			x.Qty += lo.Qty
			x.Cost += lo.Cost
			x.Value += lo.Value
			x.Loss += lo.Loss
		} else {
			byItem[t.item] = len(st.res.leftovers)
			st.res.leftovers = append(st.res.leftovers, lo)
		}
		st.res.tailAcq += lo.Cost
		st.res.tailLoss += lo.Loss
	}
}

// bidValue 是马上把 qty 件砸到生产城市的买单上能拿回多少(× 0.96)。没有买单就是 0。
func (st *state) bidValue(item string, qty float64) float64 {
	l, ok := st.e.side(item, st.e.p.City, false)
	if !ok {
		st.warn(Warning{Code: WarnMissing, Item: item, Msg: item + " 没有买单,余料按「砸买单」估值只能算 0"})
		return 0
	}
	if l.depthUnknown {
		return float64(l.levels[0].Price) * qty * st.e.mkt.TakeBid
	}
	f := depth.WalkFrac(l.levels, qty)
	return f.Total * st.e.mkt.TakeBid
}

func (st *state) decision(i int) decision {
	lv := st.e.chain[i]
	switch {
	case !lv.makeable:
		return decBuy
	case i == 0:
		return decMake
	case st.s.depth == 0:
		return decAuto
	case i < st.s.depth:
		return decMake
	}
	return decBuy
}

// ── 成本曲线 ────────────────────────────────────────────────────────────

func (st *state) rawCurve(target string, capQ float64) curve {
	os := st.e.rawOffers(target)
	if len(os) == 0 {
		st.res.missing[target] = true
	}
	return st.offerCurve(os, capQ)
}

func (st *state) buyCurve(item string, capQ float64) curve {
	os := st.e.buyOffers(item)
	if len(os) == 0 {
		st.res.missing[item] = true
	}
	return st.offerCurve(os, capQ)
}

// nodeCurve 是第 i 级半成品「要 q 件最少花多少」,q ≤ capQ。
func (st *state) nodeCurve(i int, capQ float64) curve {
	if i >= len(st.e.chain) {
		return nil
	}
	item := st.e.chain[i].item
	switch st.decision(i) {
	case decBuy:
		return st.buyCurve(item, capQ).markBuy()
	case decMake:
		c, _ := st.makeCurve(i, capQ)
		return c
	}
	b := st.buyCurve(item, capQ).markBuy()
	m, _ := st.makeCurve(i, capQ)
	return mergeCurves(b, m).truncate(capQ)
}

// makeCurve 是第 i 级自炼 m 件(m ≤ capM)的成本曲线,以及选中的配方(下标,-1 = 做不了)。
//
// 有心配方的档(.0~.3)两条配方各算一条曲线,按做 capM 件的成本取便宜的:
// 能做满的优先,平局取不用心的那条。心在抓包库里没有价格(A4),实际上几乎总是普通配方。
func (st *state) makeCurve(i int, capM float64) (curve, int) {
	lv := st.e.chain[i]
	var best curve
	bestV, bestCost, bestGot := -1, math.Inf(1), -1.0
	for v, rcp := range lv.recipes {
		c := st.variantCurve(i, rcp, capM)
		total, got, _ := c.cost(capM)
		better := bestV < 0 || got > bestGot+qtyEps ||
			(math.Abs(got-bestGot) <= qtyEps && total < bestCost-costEps)
		if better {
			best, bestV, bestCost, bestGot = c, v, total, got
		}
	}
	if bestGot <= qtyEps {
		return nil, bestV
	}
	return best, bestV
}

func (st *state) variantCurve(i int, rcp catalog.Recipe, capM float64) curve {
	lv := st.e.chain[i]
	r := st.e.rrr(st.s.focus.has(lv.tier))
	var parts []curve
	for _, in := range st.e.orderedInputs(rcp) {
		f := st.e.factor(in, r)
		need := capM * f
		var c curve
		switch st.e.kindOf(in.ItemID) {
		case kindRaw:
			c = st.rawCurve(in.ItemID, need)
		case kindHeart:
			c = st.buyCurve(in.ItemID, need)
		case kindRefined:
			if i+1 >= len(st.e.chain) || st.e.chain[i+1].item != in.ItemID {
				return nil
			}
			c = st.nodeCurve(i+1, need)
		default:
			return nil // 不认识的材料:宁可说做不了,也不少算一项
		}
		parts = append(parts, c.scale(f))
	}
	fee := StationFee(rcp.ItemValue, lv.tier, st.e.p.Fees.StationPer100)
	return sumCurves(parts...).plus(fee).truncate(capM)
}

// ── 实际分配 ────────────────────────────────────────────────────────────

// acquire 拿到第 i 级半成品 q 件:买、做,或者按两条曲线精确拆分。
func (st *state) acquire(i int, q float64) {
	if q <= qtyEps {
		return
	}
	item := st.e.chain[i].item
	if k, ok := st.forceBuy[i]; ok {
		// 测试用:强制这一级买 k 件、做其余的,和精确拆分逐 k 对照
		k = math.Min(k, q)
		if k > qtyEps {
			st.buy(item, k)
		}
		if q-k > qtyEps {
			st.make(i, q-k)
		}
		return
	}
	switch st.decision(i) {
	case decBuy:
		st.buy(item, q)
		return
	case decMake:
		st.make(i, q)
		return
	}
	b := st.buyCurve(item, q).markBuy()
	m, _ := st.makeCurve(i, q)
	merged := mergeCurves(b, m)
	_, got, kb := merged.cost(q)
	if got < q-qtyEps {
		// 买和做加起来都不够:能买的先买满,剩下的去做,做不出来的由 make 报深度不够
		kb = b.capacity()
	}
	if kb > qtyEps {
		st.buy(item, math.Min(kb, q))
	}
	if q-kb > qtyEps {
		st.make(i, q-kb)
	}
}

// make 自炼第 i 级 m 件:先分这一级的原料,再拿下级料。
func (st *state) make(i int, m float64) {
	if m <= qtyEps {
		return
	}
	e := st.e
	lv := e.chain[i]
	if len(lv.recipes) == 0 {
		st.warn(Warning{Code: WarnMissing, Item: lv.item, Msg: lv.item + " 没有精炼配方"})
		st.res.filled = false
		return
	}
	v := 0
	if len(lv.recipes) > 1 {
		if _, best := st.makeCurve(i, m); best >= 0 {
			v = best
		}
	}
	rcp := lv.recipes[v]
	focusOn := st.s.focus.has(lv.tier)
	r := e.rrr(focusOn)
	rec := refineRec{
		item: lv.item, tier: lv.tier, crafts: m, focusOn: focusOn, rrr: r,
		stationFee: m * StationFee(rcp.ItemValue, lv.tier, e.p.Fees.StationPer100),
		variant:    rcp.Variant, heart: usesHeart(rcp),
	}
	if focusOn {
		rec.focusBase = m * float64(rcp.Focus)
		rec.focusEff = m * FocusCost(float64(rcp.Focus), e.p.Focus.FCE[lv.tier])
	}
	for _, in := range e.orderedInputs(rcp) {
		f := e.factor(in, r)
		need := m * f
		rec.inputs = append(rec.inputs, StepInput{Item: in.ItemID, Qty: need})
		kind := e.kindOf(in.ItemID)
		switch kind {
		case kindRaw:
			st.raw(in.ItemID, need, i == 0)
		case kindHeart:
			st.buy(in.ItemID, need)
		case kindRefined:
			st.acquire(i+1, need)
		default:
			st.warn(Warning{Code: WarnMissing, Item: in.ItemID, Msg: "配方里有不认识的材料 " + in.ItemID})
			st.res.filled = false
		}
		// 批末余料:参与返还的材料,一批做完总会剩约 count × r 件(§2.4)
		if f < float64(in.Count) {
			st.res.tails = append(st.res.tails, tailRec{item: in.ItemID, qty: float64(in.Count) * r, level: i, kind: kind})
		}
	}
	st.res.total += rec.stationFee
	st.res.focusBase += rec.focusBase
	st.res.focusEff += rec.focusEff
	st.res.refines = append(st.res.refines, rec)
}

// raw 分原料。顶层的原料按开法(混合 / 纯转化 / 挂买单),下面几级一律混合最优。
func (st *state) raw(target string, need float64, top bool) {
	os := st.rawSource(target, top)
	takes, _, got := st.allocate(os, need)
	st.record(takes)
	st.res.rawUses = append(st.res.rawUses, rawUse{target: target, need: need, got: got, takes: takes})
	st.checkFill(target, need, got, len(os) == 0)
}

// rawSource 是一种原料可用的货:顶层的按开法,下面几级一律混合最优。
func (st *state) rawSource(target string, top bool) []offer {
	e := st.e
	switch {
	case !top || st.s.raw == rawMixed:
		return e.rawOffers(target)
	case st.s.raw == rawPure:
		return e.pureOffers(target, st.s.start)
	case st.s.raw == rawBid:
		if o, ok := e.bidOffer(target); ok {
			return []offer{o}
		}
	}
	return nil
}

// buy 直接买现成的(半成品、心)。
func (st *state) buy(item string, q float64) {
	os := st.e.buyOffers(item)
	takes, _, got := st.allocate(os, q)
	st.record(takes)
	st.checkFill(item, q, got, len(os) == 0)
}

func (st *state) checkFill(item string, want, got float64, none bool) {
	if got >= want-qtyEps {
		return
	}
	st.res.filled = false
	if none {
		st.res.missing[item] = true
		st.warn(Warning{Code: WarnMissing, Item: item, Msg: fmt.Sprintf("%s 缺价:可买城市里没有它的挂单", item)})
		return
	}
	st.warn(Warning{Code: WarnDepthShort, Item: item,
		Msg: fmt.Sprintf("%s 要 %s 件,盘口只够 %s 件", item, fmtQty(want), fmtQty(got))})
}

// record 把一次分配记成买入和转化。
func (st *state) record(takes []take) {
	res := st.res
	for _, t := range takes {
		o := t.o
		var br *buyRec
		for _, b := range res.buys {
			if b.item == o.key.item && b.city == o.key.city && b.bid == o.key.bid && b.forItem == o.target {
				br = b
				break
			}
		}
		if br == nil {
			br = &buyRec{item: o.key.item, city: o.key.city, bid: o.key.bid, forItem: o.target,
				levels: map[int64]float64{}, seen: o.seen, source: o.source,
				truncated: o.truncated, depthUnknown: o.depthUnknown}
			res.buys = append(res.buys, br)
		}
		// 一笔买入吃了几档,新鲜度按其中最旧的那一档报(审查 finding 2)
		if !o.seen.IsZero() && (br.seen.IsZero() || o.seen.Before(br.seen)) {
			br.seen = o.seen
		}
		br.qty += t.qty
		br.cost += t.qty * o.buyUnit()
		br.transport += t.qty * o.transport
		br.levels[int64(math.Round(o.price))] += t.qty
		if o.lastVisible {
			br.hitLast = true
		}
		res.total += t.qty * o.eff
		if o.route > 0 {
			routes := st.e.routes(o.target)
			for cur := o.key.item; cur != o.target; {
				r := routes[cur]
				if r.next == "" {
					break
				}
				k := [2]string{cur, r.next}
				tr := res.trans[k]
				if tr == nil {
					tr = &transRec{from: cur, to: r.next, unitFee: r.edge}
					res.trans[k] = tr
				}
				tr.times += t.qty
				cur = r.next
			}
		}
	}
}

func (st *state) warn(w Warning) {
	for _, x := range st.res.warnings {
		if x.Code == w.Code && x.Item == w.Item {
			return
		}
	}
	st.res.warnings = append(st.res.warnings, w)
}

// fmtQty 把件数写成最多两位小数。
func fmtQty(q float64) string {
	if math.IsInf(q, 1) {
		return "不限"
	}
	return strconv.FormatFloat(math.Round(q*100)/100, 'f', -1, 64)
}
