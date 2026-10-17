package refine

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"albion-guild/internal/depth"
)

// ── 输出 ────────────────────────────────────────────────────────────────

// 警告代码。界面按代码决定图标和颜色,Msg 是给人看的。
const (
	WarnDepthShort   = "depth_short"       // 盘口不够深,拿不到想要的量
	WarnTailShort    = "tail_short"        // 批末余料备不齐,开工本金是下界
	WarnMissing      = "missing"           // 缺价:需要的东西没有挂单
	WarnTruncated    = "truncated"         // 盘口没翻完、又吃到了可见的最后一档:成本只是下界(G1)
	WarnDepthUnknown = "depth_unknown"     // 只有 AODP 价,没有件数
	WarnUnverified   = "unverified"        // 用到了没核实的参数
	WarnThinVolume   = "thin_volume"       // 成交量太薄,一批要卖好几天
	WarnNoVolume     = "no_volume"         // 没有成交量数据(不当成 0)
	WarnBidRisk      = "bid_fill_risk"     // 挂买单收料,能不能收到、要等多久都不知道
	WarnOverFocus    = "over_focus_budget" // 这一批要的专注超过今日可用
	WarnStale        = "stale"             // 用到的价格超过 6 / 24 小时
	WarnSellCliff    = "sell_cliff"        // 吃买单卖不完这一批,后面是断崖
	WarnNoSellPrice  = "no_sell_price"     // 出售城市没有挂单,算不出利润
	WarnLocked       = "locked"            // 目标 tier 没解锁,只能买不能炼
	WarnQtyBound     = "qty_bound"         // 日化卡在这一批的件数:别的上限还允许做更多,但成本只按这一批算过
	WarnCapitalShort = "capital_short"     // 开工本金超过本金,这一批一次备不齐料
)

// Warning 是一条警告。
type Warning struct {
	Code string `json:"code"`
	Item string `json:"item,omitempty"`
	Msg  string `json:"msg"`
}

// StepInput 是精炼一步实际消耗的材料(稳态)。
type StepInput struct {
	Item string  `json:"item"`
	Qty  float64 `json:"qty"`
}

// Step 是步骤清单的一步:买什么、在哪城、多少件、价位、金额、观测时间,
// 转化几次,精炼几次、开没开专注(设计 §4.5)。
type Step struct {
	// Op:buy 吃卖单 | bid 挂买单 | transmute 转化 | refine 精炼 | sell 出售
	Op   string  `json:"op"`
	Item string  `json:"item"`
	City string  `json:"city,omitempty"`
	Qty  float64 `json:"qty"`
	Cost float64 `json:"cost"`

	// 买入
	For       string            `json:"for,omitempty"` // 买来转化成什么
	Levels    []depth.FracLevel `json:"levels,omitempty"`
	PriceFrom int64             `json:"price_from,omitempty"`
	PriceTo   int64             `json:"price_to,omitempty"`
	Transport float64           `json:"transport,omitempty"`
	SeenAt    *time.Time        `json:"seen_at,omitempty"`
	AgeH      *float64          `json:"age_h,omitempty"`
	Source    string            `json:"source,omitempty"`

	// 转化:From → To 做 Qty 次,每次 UnitFee
	From    string  `json:"from,omitempty"`
	To      string  `json:"to,omitempty"`
	UnitFee float64 `json:"unit_fee,omitempty"`

	// 精炼:做 Qty 次(每次 1 件)
	FocusOn    bool        `json:"focus_on,omitempty"`
	RRR        float64     `json:"rrr,omitempty"`
	StationFee float64     `json:"station_fee,omitempty"`
	FocusBase  float64     `json:"focus_base,omitempty"`
	FocusCost  float64     `json:"focus_cost,omitempty"`
	Variant    int         `json:"variant,omitempty"`
	Heart      bool        `json:"heart,omitempty"`
	Inputs     []StepInput `json:"inputs,omitempty"`

	// 出售
	Mode  string  `json:"mode,omitempty"`
	Price int64   `json:"price,omitempty"`
	Net   float64 `json:"net,omitempty"`
	// Unsold 是这一批做出来、按这种卖法卖不掉的件数(吃买单撞上断崖或盘口见底)。
	// 它们按成本留在手里,不进这一批的利润(ProfitLine.Total)
	Unsold float64 `json:"unsold,omitempty"`
}

// ProfitLine 是一种卖法下的利润。
type ProfitLine struct {
	Net  float64 `json:"net"`  // 每件到手(吃买单是卖掉的那几件的均价)
	Unit float64 `json:"unit"` // 每件利润 = Net − 单根成本,按卖掉的件算
	Pct  float64 `json:"pct"`  // 利润 ÷ 单根成本
	// Sold 是这一批按这种卖法卖得掉几件:挂卖按整批;吃买单只到断崖之前、盘口上够得着的那些
	Sold float64 `json:"sold"`
	// Total 是这一批的利润 = Sold × Unit。吃买单卖不完的那几件不在里面:
	// 它们按成本留在手里(和批末余料 carry 一个口径),不能拿断崖之前的均价去外推
	Total float64 `json:"total"`
	// TotalPct 是这一批的总利润率 = Total ÷ 开工本金(Candidate.UpfrontCapital,含批末余料)。
	// 和 Pct 的区别:Pct 的分母是稳态单根成本;这里是开工前真要掏的钱 ——
	// 批末余料、吃买单卖不完压着的料都在分母里。分母用 TotalCost 的话,全卖掉、余料结转时
	// 它就等于 Pct,没有新信息;这个数回答的是「回本快不快」
	TotalPct float64 `json:"total_pct"`
	// MaxQty 只有吃买单有:断崖之前最多卖几件
	MaxQty *float64 `json:"max_qty,omitempty"`
}

type Profits struct {
	Post *ProfitLine `json:"post"`
	Take *ProfitLine `json:"take"`
}

// FocusUse 是一条路线用的专注。
type FocusUse struct {
	// Tiers 是实际开了专注、也确实精炼了的 tier
	Tiers       []int   `json:"tiers"`
	Base        float64 `json:"base"`      // 这一批的基础专注合计
	Effective   float64 `json:"effective"` // 按 FCE 折算后(D2)
	PerUnit     float64 `json:"per_unit"`
	BasePerUnit float64 `json:"base_per_unit"`
	OverBudget  bool    `json:"over_budget"`
}

// DailyLimits 是日化的几个上限,每天能做几件。nil = 这一项不设限。
type DailyLimits struct {
	Focus  *float64 `json:"focus"`
	Volume *float64 `json:"volume"`
	// Capital = (本金 − 批末余料占着的钱) ÷ 稳态单根成本 ÷ 周转天数
	Capital *float64 `json:"capital"`
	// Qty = 这一批的件数 ÷ 周转天数。单根成本和利润只在这个量上沿阶梯算过,
	// 做得更多就要往阶梯深处吃、成本会变,不能拿这一批的单根利润外推。永远有值
	Qty *float64 `json:"qty"`
	// Take 只在主出售方式是吃买单时有:这一批断崖之前卖得掉的件数 ÷ 周转天数
	Take *float64 `json:"take"`
}

// Daily 是日化(设计 §2.5):每天件数 = min(今日专注 ÷ 每根专注, 成交份额 × 日成交量,
// 本金 ÷ 单根成本 ÷ 周转天数, 这一批的件数 ÷ 周转天数[, 吃买单卖得掉的件数 ÷ 周转天数]),
// 标出卡在哪一项。
type Daily struct {
	Units float64 `json:"units"`
	// Binding:focus | volume | capital | qty | take
	Binding string      `json:"binding"`
	Profit  float64     `json:"profit"`
	Limits  DailyLimits `json:"limits"`
	// ROC 是本金日回报率 = Profit ÷ 本金(daily.capital);没设本金时为 nil
	ROC *float64 `json:"roc"`
}

// Candidate 是一条候选路线。
type Candidate struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	// Reference 是固定参照行:market =「全部吃卖单、不转化、下级料买现成」;
	// bid =「挂买单收料」。空 = 普通候选
	Reference string `json:"reference,omitempty"`
	// Filled=false:深度不够或缺价,成本只按拿得到的那部分算,不能当真
	Filled bool `json:"filled"`

	UnitCost float64 `json:"unit_cost"`
	// TotalCost 是这一批的稳态成本(加上 bid / zero 估值下的余料损失)
	TotalCost float64 `json:"total_cost"`
	// UpfrontCapital 是开工前要备齐的钱:稳态消耗 + 批末余料(§2.4)
	UpfrontCapital float64 `json:"upfront_capital"`

	Profit Profits  `json:"profit"`
	Focus  FocusUse `json:"focus"`
	// SilverPerFocus = (开专注的利润 − max(0, 同目标不开专注的最好利润)) ÷ 实际专注(§2.5)
	SilverPerFocus *float64 `json:"silver_per_focus"`
	Daily          *Daily   `json:"daily"`

	OldestAgeH *float64 `json:"oldest_age_h"`
	// AgeLevel:ok | yellow(超过 6 小时)| red(超过 24 小时)
	AgeLevel string `json:"age_level"`

	// Breakeven 是每种买进的投入物的盈亏平衡价(路线固定、别的价不变,见 Breakeven),
	// 花钱多的在前;料不满或算不出主方式利润时是 []
	Breakeven []Breakeven `json:"breakeven"`
	// BreakevenAllPct:所有买的料(买价和运费,不含转化实付)一起涨这么多,这一批利润归零
	BreakevenAllPct *float64 `json:"breakeven_all_pct"`

	Steps    []Step     `json:"steps"`
	Leftover []Leftover `json:"leftover"`
	Warnings []Warning  `json:"warnings"`

	sig     string
	primary *ProfitLine // 主出售方式(Sell.Modes[0])的那一行,排序用
	ev      *evalResult
}

// ParetoPoint 是一种专注开法(链自动比价、原料混合最优)的成本和专注。
type ParetoPoint struct {
	Tiers        []int   `json:"tiers"`
	FocusTotal   float64 `json:"focus_total"`
	FocusPerUnit float64 `json:"focus_per_unit"`
	UnitCost     float64 `json:"unit_cost"`
	WithinBudget bool    `json:"within_budget"`
}

// FocusInfo 是专注开关组合的 Pareto 前沿(设计 §2.3 ③)。
type FocusInfo struct {
	// Tiers 是链上能开专注的 tier,从顶层往下
	Tiers  []int   `json:"tiers"`
	Budget float64 `json:"budget"`
	// Combos 是枚举了多少种开法(2^len(Tiers))
	Combos int `json:"combos"`
	// Pareto 按专注从少到多;每多花一点专注,成本都更低
	Pareto []ParetoPoint `json:"pareto"`
	// BudgetPick 是今日专注预算内成本最低的那种
	BudgetPick *ParetoPoint `json:"budget_pick"`
}

// DataInfo 汇总输出的候选路线用到的数据质量。
type DataInfo struct {
	OldestAgeH   *float64 `json:"oldest_age_h"`
	Missing      []string `json:"missing"`
	Truncated    []string `json:"truncated"`
	DepthUnknown []string `json:"depth_unknown"`
	// Stale 是比 max_age_hours 还旧、被当作缺价的盘口
	Stale []string `json:"stale"`
}

// ChainLevel 是链上的一级,给界面画配方树。
type ChainLevel struct {
	Item     string `json:"item"`
	Tier     int    `json:"tier"`
	Makeable bool   `json:"makeable"`
	Lower    string `json:"lower,omitempty"`
}

// ParamsUsed 是这次实际用的参数,以及其中没核实的那些。
type ParamsUsed struct {
	Params
	Now        time.Time        `json:"now"`
	Unverified []UnverifiedUsed `json:"unverified"`
}

// Result 是 Plan 的输出,形状对着 POST /api/refine/plan 的响应(设计 §4.4)。
type Result struct {
	Target string `json:"target"`
	Line   string `json:"line"`
	City   string `json:"city"`
	Qty    int    `json:"qty"`
	// MainRaw 是顶层配方的原料(主原料),界面和矩阵靠它从 breakeven 里挑主原料那一项
	MainRaw    string               `json:"main_raw"`
	ParamsUsed ParamsUsed           `json:"params_used"`
	Data       DataInfo             `json:"data"`
	Sell       SellInfo             `json:"sell"`
	Volume     Volume               `json:"volume"`
	Chain      []ChainLevel         `json:"chain"`
	RawSources map[string]RawSource `json:"raw_sources"`
	Focus      FocusInfo            `json:"focus"`
	// Candidates 按排序键排好,前 Top 条;最后跟着固定的参照行(Reference 非空)
	Candidates []Candidate `json:"candidates"`
	Warnings   []Warning   `json:"warnings"`
	// RecipeSource 是配方数据的来源(库 / 上游 / 内置快照),排查对不上时先看它
	RecipeSource string `json:"recipe_source"`
}

// ── 入口 ────────────────────────────────────────────────────────────────

// Plan 算一个目标的全部候选路线。
//
// 纯函数:不连库、不看钟(Now 由调用方给)。参数不合法返回 error;
// 缺价、深度不够、tier 没解锁都不是 error,而是结果里的警告。
func Plan(in Input) (*Result, error) {
	p := in.Params.normalized()
	line, tier, _, err := p.validate()
	if err != nil {
		return nil, err
	}
	if in.Recipes.Len() == 0 {
		return nil, errors.New("没有配方数据")
	}
	if err := CheckTarget(in.Recipes, p.Target); err != nil {
		return nil, err
	}
	e := newEngine(p, line, in)
	e.buildChain()
	tiers := e.focusTiers()

	res := &Result{
		Target: p.Target, Line: line.Key, City: p.City, Qty: p.Qty, MainRaw: e.topRaw(),
		ParamsUsed:   ParamsUsed{Params: p, Now: p.Now.UTC(), Unverified: p.unverifiedUsed(tiers)},
		Sell:         e.sellInfo(),
		Volume:       in.Volume,
		RawSources:   map[string]RawSource{},
		Candidates:   []Candidate{},
		Warnings:     []Warning{},
		RecipeSource: in.Recipes.Source(),
	}
	for _, lv := range e.chain {
		res.Chain = append(res.Chain, ChainLevel{Item: lv.item, Tier: lv.tier, Makeable: lv.makeable, Lower: lv.lower})
	}
	res.Warnings = append(res.Warnings, e.planWarnings(res.Sell)...)
	if !e.chain[0].makeable {
		res.Warnings = append(res.Warnings, Warning{Code: WarnLocked, Item: p.Target,
			Msg: fmt.Sprintf("T%d 精炼没解锁(已解锁到 T%d),只能买现成的,不能自炼", tier, p.Focus.UnlockedTier)})
		res.Data = e.dataInfo(nil, nil)
		return res, nil
	}

	var pick tierSet
	res.Focus, pick = e.focusFront(tiers)
	strats, refs := e.strategies(tiers, pick)

	evals := make([]*evalResult, 0, len(strats))
	for _, s := range strats {
		evals = append(evals, e.evaluate(s))
	}
	// 同目标不开专注的最好利润:每点专注值的基准(§2.5)。在算过的所有开法里找——
	// 专注模式固定成 all 时候选里没有不开专注的,但上面的 Pareto 枚举一定算过「全不开」
	bestNoFocus := e.bestNoFocusProfit(res.Sell)

	all := make([]Candidate, 0, len(evals))
	for _, ev := range evals {
		all = append(all, e.candidate(ev, res.Sell, bestNoFocus, ""))
	}
	sortCandidates(all, p.Sort)
	seen := map[string]bool{}
	var kept []*evalResult
	for _, c := range all {
		if seen[c.sig] {
			continue
		}
		seen[c.sig] = true
		res.Candidates = append(res.Candidates, c)
		kept = append(kept, c.ev)
		if len(res.Candidates) >= p.Top {
			break
		}
	}
	for _, r := range refs {
		c := e.candidate(e.evaluate(r.s), res.Sell, bestNoFocus, r.ref)
		res.Candidates = append(res.Candidates, c)
		kept = append(kept, c.ev)
	}

	// 「原料来路」表:按推荐那条的需求量列(设计 §4.5)
	if len(kept) > 0 {
		for _, u := range kept[0].rawUses {
			if _, dup := res.RawSources[u.target]; dup {
				continue
			}
			rs := RawSource{
				Need:  u.need,
				Pure:  e.pureRoutes(u.target, u.need),
				Mixed: mixedFromTakes(e.routes(u.target), u.takes, u.need),
			}
			// 缺价时两张表都是空的:给 [] 不给 null,界面不用分两种情况判
			if rs.Pure == nil {
				rs.Pure = []PureRoute{}
			}
			if rs.Mixed.Split == nil {
				rs.Mixed.Split = []MixedSplit{}
			}
			res.RawSources[u.target] = rs
		}
	}
	e.fillRawBreakevens(res)
	res.Data = e.dataInfo(res.Candidates, kept)
	return res, nil
}

// ── 专注组合 ────────────────────────────────────────────────────────────

// focusFront 逐个枚举链上每一级专注的开 / 关(最多 2^5 = 32 种),
// 得到「成本–专注」的 Pareto 前沿,再取今日预算内成本最低的那种(设计 §2.3 ③)。
func (e *engine) focusFront(tiers []int) (FocusInfo, tierSet) {
	n := len(tiers)
	info := FocusInfo{Tiers: tiers, Budget: e.p.Focus.Budget, Combos: 1 << n, Pareto: []ParetoPoint{}}
	type pt struct {
		set tierSet
		pp  ParetoPoint
	}
	var pts []pt
	N := float64(e.p.Qty)
	for mask := 0; mask < 1<<n; mask++ {
		var set tierSet
		for j, t := range tiers {
			if mask&(1<<j) != 0 {
				set |= 1 << t
			}
		}
		ev := e.evaluate(strategy{raw: rawMixed, depth: 0, focus: set})
		if !ev.filled {
			continue
		}
		pts = append(pts, pt{set, ParetoPoint{
			Tiers: set.list(), FocusTotal: ev.focusEff, FocusPerUnit: ev.focusEff / N,
			UnitCost:     e.unitCost(ev),
			WithinBudget: ev.focusEff <= e.p.Focus.Budget+costEps,
		}})
	}
	// 平局:专注少的优先,再按开的级数少、集合小的优先,保证确定
	sort.SliceStable(pts, func(i, j int) bool {
		a, b := pts[i], pts[j]
		if math.Abs(a.pp.FocusTotal-b.pp.FocusTotal) > costEps {
			return a.pp.FocusTotal < b.pp.FocusTotal
		}
		if math.Abs(a.pp.UnitCost-b.pp.UnitCost) > costEps {
			return a.pp.UnitCost < b.pp.UnitCost
		}
		if len(a.pp.Tiers) != len(b.pp.Tiers) {
			return len(a.pp.Tiers) < len(b.pp.Tiers)
		}
		return a.set < b.set
	})
	best := math.Inf(1)
	for _, x := range pts {
		if x.pp.UnitCost < best-costEps {
			info.Pareto = append(info.Pareto, x.pp)
			best = x.pp.UnitCost
		}
	}
	var pick tierSet
	var pickPt *ParetoPoint
	for i := range pts {
		x := pts[i]
		if !x.pp.WithinBudget {
			continue
		}
		if pickPt == nil || x.pp.UnitCost < pickPt.UnitCost-costEps {
			pp := x.pp
			pickPt, pick = &pp, x.set
		}
	}
	info.BudgetPick = pickPt
	return info, pick
}

// ── 候选路线 ────────────────────────────────────────────────────────────

type refStrategy struct {
	s   strategy
	ref string
}

// strategies 生成候选路线的开法(设计 §2.3 ⑤):
// 原料 {混合最优 | 从前 K 个起点纯转化 | 挂买单(可选)} × 链 {自动 | 下级料全买 | 自炼到第 d 级}
// × 专注 {预算内最优 | 全开 | 顶层 + 下一级 | 只顶层 | 不开}。出售方式不单独成候选:
// 每条候选两种卖法的利润都算。另外固定两条参照行。
func (e *engine) strategies(tiers []int, pick tierSet) ([]strategy, []refStrategy) {
	p := e.p
	top := setOf(tiers[0])
	topNext := setOf(tiers[:min(2, len(tiers))]...)
	all := setOf(tiers...)
	var sets []tierSet
	var ref tierSet
	switch p.Focus.Mode {
	case FocusOff:
		sets, ref = []tierSet{0}, 0
	case FocusTop:
		sets, ref = []tierSet{top}, top
	case FocusAll:
		sets, ref = []tierSet{all}, all
	case FocusCustom:
		c := setOf(p.Focus.CustomTiers...)
		sets, ref = []tierSet{c}, c
	default:
		sets, ref = []tierSet{pick, all, topNext, top, 0}, top
	}
	sets = uniqueSets(sets)

	topRaw := e.topRaw()
	raws := []strategy{{raw: rawMixed}}
	if topRaw != "" && p.PureStarts > 0 {
		need := float64(p.Qty) * e.topRawFactor(ref.has(tiers[0]))
		n := 0
		for _, pr := range e.pureRoutes(topRaw, need) {
			if !pr.Filled || n >= p.PureStarts {
				continue
			}
			raws = append(raws, strategy{raw: rawPure, start: pr.Start})
			n++
		}
	}
	_, hasBid := e.bidOffer(topRaw)
	if p.Sources.BidOrders && hasBid {
		raws = append(raws, strategy{raw: rawBid})
	}

	var out []strategy
	for _, r := range raws {
		for d := 0; d <= len(tiers); d++ {
			for _, f := range sets {
				out = append(out, strategy{raw: r.raw, start: r.start, depth: d, focus: f})
			}
		}
	}
	refs := []refStrategy{{strategy{raw: rawPure, start: topRaw, depth: 1, focus: ref}, "market"}}
	if hasBid {
		refs = append(refs, refStrategy{strategy{raw: rawBid, depth: 1, focus: ref}, "bid"})
	}
	return out, refs
}

func uniqueSets(sets []tierSet) []tierSet {
	var out []tierSet
	seen := map[tierSet]bool{}
	for _, s := range sets {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// topRaw 是顶层配方(普通那条)的原料。
func (e *engine) topRaw() string {
	lv := e.chain[0]
	if len(lv.recipes) == 0 {
		return ""
	}
	for _, in := range lv.recipes[0].Inputs {
		if e.kindOf(in.ItemID) == kindRaw {
			return in.ItemID
		}
	}
	return ""
}

// topRawFactor 是顶层每做 1 件要多少原料(稳态)。
func (e *engine) topRawFactor(focus bool) float64 {
	lv := e.chain[0]
	r := e.rrr(focus)
	for _, in := range lv.recipes[0].Inputs {
		if e.kindOf(in.ItemID) == kindRaw {
			return e.factor(in, r)
		}
	}
	return 0
}

func (e *engine) unitCost(ev *evalResult) float64 {
	total := ev.total
	if e.p.IncludeTail {
		total += ev.tailLoss
	}
	return total / float64(e.p.Qty)
}

// primaryProfit 是主出售方式(Sell.Modes[0])下的每件利润;缺价时 ok=false。
func (e *engine) primaryProfit(si SellInfo, unitCost float64) (float64, bool) {
	switch e.p.Sell.Modes[0] {
	case SellTake:
		if si.Take == nil || si.Take.Fill.Got <= 0 {
			return 0, false
		}
		return si.Take.Fill.NetAvg - unitCost, true
	default:
		if si.Post == nil {
			return 0, false
		}
		return si.Post.Net - unitCost, true
	}
}

// bestNoFocusProfit 是所有算过的、不用专注又拿得满料的开法里,主出售方式下最好的每件利润。
func (e *engine) bestNoFocusProfit(si SellInfo) *float64 {
	var best *float64
	keys := make([]string, 0, len(e.evalCache))
	for k := range e.evalCache {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		ev := e.evalCache[k]
		if !ev.filled || ev.focusEff > 0 || ev.s.raw == rawBid {
			continue // 挂买单能不能收到不知道,不拿它当基准
		}
		if pr, ok := e.primaryProfit(si, e.unitCost(ev)); ok && (best == nil || pr > *best) {
			v := pr
			best = &v
		}
	}
	return best
}

// candidate 把一次评估整理成候选路线:指标、步骤清单、警告。
func (e *engine) candidate(ev *evalResult, si SellInfo, bestNoFocus *float64, ref string) Candidate {
	p := e.p
	N := float64(p.Qty)
	c := Candidate{ID: ev.s.id(), Label: e.label(ev.s), Reference: ref, Filled: ev.filled,
		Warnings: []Warning{}, Leftover: []Leftover{}, ev: ev}
	c.TotalCost = ev.total
	c.UpfrontCapital = ev.total
	if p.IncludeTail {
		c.TotalCost += ev.tailLoss
		c.UpfrontCapital += ev.tailAcq
		c.Leftover = append(c.Leftover, ev.leftovers...)
	}
	c.UnitCost = c.TotalCost / N

	if si.Post != nil {
		c.Profit.Post = &ProfitLine{Net: si.Post.Net, Unit: si.Post.Net - c.UnitCost, Sold: N}
		c.Profit.Post.Pct = pct(c.Profit.Post.Unit, c.UnitCost)
		c.Profit.Post.Total = c.Profit.Post.Sold * c.Profit.Post.Unit
		c.Profit.Post.TotalPct = pct(c.Profit.Post.Total, c.UpfrontCapital)
	}
	if si.Take != nil && si.Take.Fill.Got > 0 {
		// 每件利润按卖掉的那几件的均价算;断崖之后的不外推(见 ProfitLine.Total)
		c.Profit.Take = &ProfitLine{Net: si.Take.Fill.NetAvg, Unit: si.Take.Fill.NetAvg - c.UnitCost,
			Sold: si.Take.Fill.Got, MaxQty: si.Take.MaxQty}
		c.Profit.Take.Pct = pct(c.Profit.Take.Unit, c.UnitCost)
		c.Profit.Take.Total = c.Profit.Take.Sold * c.Profit.Take.Unit
		c.Profit.Take.TotalPct = pct(c.Profit.Take.Total, c.UpfrontCapital)
	}
	if p.Sell.Modes[0] == SellTake {
		c.primary = c.Profit.Take
	} else {
		c.primary = c.Profit.Post
	}

	// 专注
	var focused []int
	for _, r := range ev.refines {
		if r.focusOn && r.crafts > qtyEps {
			focused = append(focused, r.tier)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(focused)))
	c.Focus = FocusUse{Tiers: focused, Base: ev.focusBase, Effective: ev.focusEff,
		PerUnit: ev.focusEff / N, BasePerUnit: ev.focusBase / N,
		OverBudget: ev.focusEff > p.Focus.Budget+costEps}
	if c.Focus.Tiers == nil {
		c.Focus.Tiers = []int{}
	}

	primary, hasProfit := e.primaryProfit(si, c.UnitCost)
	if hasProfit && ev.focusEff > 0 {
		base := 0.0
		if bestNoFocus != nil && *bestNoFocus > 0 {
			base = *bestNoFocus
		}
		v := (primary - base) / c.Focus.PerUnit
		c.SilverPerFocus = &v
	}
	if hasProfit {
		c.Daily = e.daily(c, ev, primary)
	}
	c.Breakeven, c.BreakevenAllPct = e.breakevens(&c, ev)

	c.Steps = e.steps(ev, si)
	c.sig = signature(c.Steps)
	e.ageAndWarnings(&c, ev, si)
	e.dailyWarnings(&c)
	return c
}

func pct(profit, cost float64) float64 {
	if cost <= 0 {
		return 0
	}
	return profit / cost
}

// daily 是日化(设计 §2.5)。profit 是主出售方式下的每件利润。
//
// 几个上限取最小,平局时先登记的那一项算「卡住」的:
//   - focus:今日专注 ÷ 每根专注
//   - volume:成交份额 × 日成交量(没有成交数据时不设限,不当成 0)
//   - capital:(本金 − 批末余料占着的钱) ÷ 稳态单根成本 ÷ 周转天数。批末余料(§2.4)开工前
//     就得备齐、做完还留在手里结转,这部分本金周转不起来;不扣的话开工本金明明超过本金,
//     日化照样按整份本金报件数(审查 finding 3)。按稳态成本除,是因为本金要备的是
//     买料的钱,余料的估值损失(bid / zero)不占本金
//   - qty:这一批的件数 ÷ 周转天数。单根成本是按这一批沿阶梯吃出来的,做得更多就吃得更深、
//     更贵;不封顶的话低 tier 不开专注的格子按成交量能外推出一天上万件,日化又是默认排序键
//     (审查 finding 1)。要看更大的量,把 qty 调大重算
//   - take:主出售方式是吃买单时,这一批断崖之前卖得掉的件数 ÷ 周转天数(finding 4)
func (e *engine) daily(c Candidate, ev *evalResult, profit float64) *Daily {
	p := e.p
	d := &Daily{}
	units := math.Inf(1)
	consider := func(v float64, name string, slot **float64) {
		vv := math.Max(0, v)
		*slot = &vv
		if vv < units {
			units, d.Binding = vv, name
		}
	}
	if c.Focus.PerUnit > 0 {
		consider(p.Focus.Budget/c.Focus.PerUnit, "focus", &d.Limits.Focus)
	}
	if e.volume.Known {
		consider(p.Daily.VolumeShare*e.volume.PerDay, "volume", &d.Limits.Volume)
	}
	if steady := ev.total / float64(p.Qty); p.Daily.Capital > 0 && steady > 0 {
		tied := c.UpfrontCapital - ev.total // 批末余料的取得成本;不算余料时是 0
		consider((p.Daily.Capital-tied)/steady/p.Daily.TurnoverDays, "capital", &d.Limits.Capital)
	}
	consider(float64(p.Qty)/p.Daily.TurnoverDays, "qty", &d.Limits.Qty)
	if p.Sell.Modes[0] == SellTake && c.Profit.Take != nil {
		consider(c.Profit.Take.Sold/p.Daily.TurnoverDays, "take", &d.Limits.Take)
	}
	d.Units = units
	d.Profit = units * profit
	if p.Daily.Capital > 0 {
		roc := d.Profit / p.Daily.Capital
		d.ROC = &roc
	}
	return d
}

// dailyWarnings:日化卡在批量上、开工本金超过本金,这两种要让人知道。
func (e *engine) dailyWarnings(c *Candidate) {
	p := e.p
	if p.Daily.Capital > 0 && c.UpfrontCapital > p.Daily.Capital+costEps {
		c.Warnings = append(c.Warnings, Warning{Code: WarnCapitalShort,
			Msg: fmt.Sprintf("开工要先备齐 %.0f 银的料(含批末余料),超过本金 %.0f:这一批一次备不齐,日化按扣掉余料占用之后的本金算",
				c.UpfrontCapital, p.Daily.Capital)})
	}
	d := c.Daily
	if d == nil || d.Binding != "qty" {
		return
	}
	// 别的上限里最小的那个:比批量大才值得提示
	other := math.Inf(1)
	for _, v := range []*float64{d.Limits.Focus, d.Limits.Volume, d.Limits.Capital, d.Limits.Take} {
		if v != nil && *v < other {
			other = *v
		}
	}
	if other > d.Units+qtyEps {
		room := "专注、成交量、本金都没封顶"
		if !math.IsInf(other, 1) {
			room = "专注、成交量、本金还允许每天做 " + fmtQty(other) + " 件"
		}
		c.Warnings = append(c.Warnings, Warning{Code: WarnQtyBound,
			Msg: fmt.Sprintf("日化只算到这一批的 %s 件/天(%s),但单根成本只按这一批的量沿阶梯算过;要看更大的量,把件数调大重算",
				fmtQty(d.Units), room)})
	}
}

// ── 步骤清单 ────────────────────────────────────────────────────────────

func (e *engine) steps(ev *evalResult, si SellInfo) []Step {
	var out []Step
	for _, b := range ev.buys {
		st := Step{Op: "buy", Item: b.item, City: b.city, Qty: b.qty, Cost: b.cost + b.transport,
			Transport: b.transport, Source: b.source}
		if b.bid {
			st.Op = "bid"
		}
		if b.forItem != b.item {
			st.For = b.forItem
		}
		prices := make([]int64, 0, len(b.levels))
		for pr := range b.levels {
			prices = append(prices, pr)
		}
		sort.Slice(prices, func(i, j int) bool { return prices[i] < prices[j] })
		for _, pr := range prices {
			st.Levels = append(st.Levels, depth.FracLevel{Price: pr, Qty: b.levels[pr]})
		}
		if len(prices) > 0 {
			st.PriceFrom, st.PriceTo = prices[0], prices[len(prices)-1]
		}
		st.SeenAt, st.AgeH = e.seenAge(b.seen)
		out = append(out, st)
	}
	trans := make([]*transRec, 0, len(ev.trans))
	for _, t := range ev.trans {
		trans = append(trans, t)
	}
	sort.Slice(trans, func(i, j int) bool {
		si, sj := levelSum(trans[i].from), levelSum(trans[j].from)
		if si != sj {
			return si < sj
		}
		if trans[i].from != trans[j].from {
			return trans[i].from < trans[j].from
		}
		return trans[i].to < trans[j].to
	})
	for _, t := range trans {
		out = append(out, Step{Op: "transmute", Item: t.to, From: t.from, To: t.to, Qty: t.times,
			UnitFee: t.unitFee, Cost: t.times * t.unitFee, City: e.p.City})
	}
	refines := append([]refineRec(nil), ev.refines...)
	sort.SliceStable(refines, func(i, j int) bool { return refines[i].tier < refines[j].tier })
	for _, r := range refines {
		out = append(out, Step{Op: "refine", Item: r.item, City: e.p.City, Qty: r.crafts, Cost: r.stationFee,
			FocusOn: r.focusOn, RRR: r.rrr, StationFee: r.stationFee, FocusBase: r.focusBase, FocusCost: r.focusEff,
			Variant: r.variant, Heart: r.heart, Inputs: r.inputs})
	}
	sell := Step{Op: "sell", Item: e.p.Target, City: si.City, Qty: float64(e.p.Qty), Mode: e.p.Sell.Modes[0]}
	switch {
	case sell.Mode == SellPost && si.Post != nil:
		sell.Price, sell.Net = si.Post.Price, si.Post.Net
		sell.SeenAt, sell.AgeH, sell.Source = si.Post.SeenAt, si.Post.AgeH, si.Post.Source
	case sell.Mode == SellTake && si.Take != nil:
		sell.Price, sell.Net = int64(math.Round(si.Take.Fill.VWAP)), si.Take.Fill.NetAvg
		sell.Qty = si.Take.Fill.Got
		if u := float64(e.p.Qty) - si.Take.Fill.Got; u > qtyEps {
			sell.Unsold = u // 做了 Qty 件、只卖得掉 Got 件:剩下的按成本留在手里
		}
		sell.SeenAt, sell.AgeH, sell.Source = si.Take.SeenAt, si.Take.AgeH, si.Take.Source
	}
	out = append(out, sell)
	return out
}

// signature 是步骤清单的签名:两种开法算出同样的步骤(比如「自动比价」恰好就是
// 「下级料全买」),只留一条。件数取到 1e-6。
func signature(steps []Step) string {
	var b strings.Builder
	for _, s := range steps {
		if s.Op == "sell" {
			continue
		}
		fmt.Fprintf(&b, "%s|%s|%s|%s|%s|%.6f|%t|%d;", s.Op, s.Item, s.City, s.From, s.To, s.Qty, s.FocusOn, s.Variant)
	}
	return b.String()
}

// ── 新鲜度和警告 ────────────────────────────────────────────────────────

func (e *engine) ageAndWarnings(c *Candidate, ev *evalResult, si SellInfo) {
	p := e.p
	c.Warnings = append(c.Warnings, ev.warnings...)
	oldest, known := 0.0, false
	note := func(t time.Time) {
		if age, ok := e.ageHours(t); ok {
			if !known || age > oldest {
				oldest = age
			}
			known = true
		}
	}
	for _, b := range ev.buys {
		note(b.seen)
		if b.hitLast && b.truncated {
			c.Warnings = append(c.Warnings, Warning{Code: WarnTruncated, Item: b.item,
				Msg: fmt.Sprintf("%s @ %s 的盘口没翻完,又吃到了可见的最后一档:成本只是下界", b.item, b.city)})
		}
		if b.depthUnknown {
			c.Warnings = append(c.Warnings, Warning{Code: WarnDepthUnknown, Item: b.item,
				Msg: fmt.Sprintf("%s @ %s 只有价格、没有件数(%s),按不限量算", b.item, b.city, b.source)})
		}
		if b.bid {
			c.Warnings = append(c.Warnings, Warning{Code: WarnBidRisk, Item: b.item,
				Msg: fmt.Sprintf("%s 按挂买单收 %s 件:能不能收到、要等多久都不知道,创建费下单就扣", b.item, fmtQty(b.qty))})
		}
	}
	switch p.Sell.Modes[0] {
	case SellTake:
		if si.Take != nil && si.Take.SeenAt != nil {
			note(*si.Take.SeenAt)
		}
	default:
		if si.Post != nil && si.Post.SeenAt != nil {
			note(*si.Post.SeenAt)
		}
	}
	c.AgeLevel = "ok"
	if known {
		o := math.Round(oldest*1000) / 1000
		c.OldestAgeH = &o
		switch {
		case oldest > AgeRedHours:
			c.AgeLevel = "red"
		case oldest > AgeYellowHours:
			c.AgeLevel = "yellow"
		}
		if c.AgeLevel != "ok" {
			c.Warnings = append(c.Warnings, Warning{Code: WarnStale,
				Msg: fmt.Sprintf("用到的价格最旧的已经 %.1f 小时", oldest)})
		}
	}
	if c.Focus.OverBudget {
		c.Warnings = append(c.Warnings, Warning{Code: WarnOverFocus,
			Msg: fmt.Sprintf("这一批要 %.0f 专注,今日可用 %.0f", c.Focus.Effective, p.Focus.Budget)})
	}
	// 值等于默认值、用户又没说核对过(Params.Confirmed)的才算「没核实」
	var keys []string
	if len(ev.trans) > 0 {
		if p.Fees.TransmuteFactor == DefaultTransmuteFactor && !p.confirmed("fees.transmute_factor") {
			keys = append(keys, fmt.Sprintf("转化系数 k=%g", p.Fees.TransmuteFactor))
		}
		if p.Fees.TransmuteStationPer100 == DefaultTransmuteStationFeePer100 && !p.confirmed("fees.transmute_station_per100") {
			keys = append(keys, fmt.Sprintf("转化主人费率 %g/100 营养", p.Fees.TransmuteStationPer100))
		}
	}
	if len(ev.refines) > 0 && p.Fees.StationPer100 == DefaultStationFeePer100 && !p.confirmed("fees.station_per100") {
		keys = append(keys, fmt.Sprintf("精炼站费 %g/100 营养", p.Fees.StationPer100))
	}
	if ev.focusEff > 0 && !p.confirmed("focus.fce") {
		zero := true
		for _, t := range c.Focus.Tiers {
			if p.Focus.FCE[t] != DefaultFCE {
				zero = false
			}
		}
		if zero {
			keys = append(keys, "专精 FCE=0")
		}
	}
	for _, r := range ev.refines {
		if r.heart && p.HeartReturns == DefaultHeartReturns && !p.confirmed("heart_returns") {
			keys = append(keys, "心按返还算")
			break
		}
	}
	if len(keys) > 0 {
		c.Warnings = append(c.Warnings, Warning{Code: WarnUnverified,
			Msg: "用到了没核实的参数:" + strings.Join(keys, "、") + "(见 params_used.unverified)"})
	}
}

// planWarnings 是和具体路线无关的警告:卖价、断崖、成交量。
func (e *engine) planWarnings(si SellInfo) []Warning {
	p := e.p
	var out []Warning
	for _, m := range p.Sell.Modes {
		if m == SellPost && si.Post == nil {
			out = append(out, Warning{Code: WarnNoSellPrice, Item: p.Target,
				Msg: fmt.Sprintf("%s @ %s 没有卖单,算不出挂卖价", p.Target, si.City)})
		}
		if m == SellTake && (si.Take == nil || si.Take.Fill.Got <= 0) {
			out = append(out, Warning{Code: WarnNoSellPrice, Item: p.Target,
				Msg: fmt.Sprintf("%s @ %s 没有买单,不能吃买单出货", p.Target, si.City)})
		}
	}
	if si.Take != nil && si.Take.MaxQty != nil && *si.Take.MaxQty < float64(p.Qty)-qtyEps {
		msg := fmt.Sprintf("吃买单只能出 %s 件", fmtQty(*si.Take.MaxQty))
		if si.Take.Cliff != nil {
			msg += fmt.Sprintf(",再往下买单掉到 %d(断崖,比买一低 %.0f%%)", si.Take.Cliff.Price, si.Take.Cliff.Drop*100)
		}
		out = append(out, Warning{Code: WarnSellCliff, Item: p.Target, Msg: msg})
	}
	switch {
	case !e.volume.Known:
		out = append(out, Warning{Code: WarnNoVolume, Item: p.Target,
			Msg: "没有成交量数据:日化不按成交量封顶(不是按 0 算)"})
	case p.Daily.VolumeShare*e.volume.PerDay < float64(p.Qty):
		out = append(out, Warning{Code: WarnThinVolume, Item: p.Target,
			Msg: fmt.Sprintf("日均成交 %.1f 件,按吃到 %.0f%% 算每天约 %.1f 件,这一批 %d 件要卖好几天",
				e.volume.PerDay, p.Daily.VolumeShare*100, p.Daily.VolumeShare*e.volume.PerDay, p.Qty)})
	}
	return out
}

func (e *engine) dataInfo(cands []Candidate, evs []*evalResult) DataInfo {
	d := DataInfo{Missing: []string{}, Truncated: []string{}, DepthUnknown: []string{}, Stale: []string{}}
	missing, trunc, unknown := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, ev := range evs {
		for m := range ev.missing {
			missing[m] = true
		}
		for _, b := range ev.buys {
			if b.hitLast && b.truncated {
				trunc[b.item+"|"+b.city] = true
			}
			if b.depthUnknown {
				unknown[b.item+"|"+b.city] = true
			}
		}
	}
	for _, c := range cands {
		if c.OldestAgeH != nil && (d.OldestAgeH == nil || *c.OldestAgeH > *d.OldestAgeH) {
			v := *c.OldestAgeH
			d.OldestAgeH = &v
		}
	}
	d.Missing = sortedKeys(missing)
	d.Truncated = sortedKeys(trunc)
	d.DepthUnknown = sortedKeys(unknown)
	d.Stale = sortedKeys(e.stale)
	return d
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── 排序和命名 ──────────────────────────────────────────────────────────

// sortValue 是排序键的值;算不出来(缺卖价、不用专注时的 per_focus)时 ok=false,排在后面。
func sortValue(c Candidate, by string) (float64, bool) {
	switch by {
	case SortPerFocus:
		if c.SilverPerFocus == nil {
			return 0, false
		}
		return *c.SilverPerFocus, true
	case SortUnit, SortPct:
		if c.primary == nil {
			return 0, false
		}
		if by == SortPct {
			return c.primary.Pct, true
		}
		return c.primary.Unit, true
	default:
		if c.Daily == nil {
			return 0, false
		}
		return c.Daily.Profit, true
	}
}

// sortCandidates:拿得满料的在前;再按排序键从大到小;平局按单根成本从低到高、再按 id。
func sortCandidates(cs []Candidate, by string) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.Filled != b.Filled {
			return a.Filled
		}
		va, oka := sortValue(a, by)
		vb, okb := sortValue(b, by)
		if oka != okb {
			return oka
		}
		if oka && math.Abs(va-vb) > costEps {
			return va > vb
		}
		if math.Abs(a.UnitCost-b.UnitCost) > costEps {
			return a.UnitCost < b.UnitCost
		}
		return a.ID < b.ID
	})
}

// id 是开法的稳定标识,界面和测试靠它找某一条。
func (s strategy) id() string {
	var raw string
	switch s.raw {
	case rawPure:
		raw = "pure:" + s.start
	case rawBid:
		raw = "bid"
	default:
		raw = "mixed"
	}
	chain := "auto"
	if s.depth > 0 {
		chain = "make" + strconv.Itoa(s.depth)
	}
	return raw + "|" + chain + "|focus:" + s.focus.String()
}

// label 是给人看的名字。
func (e *engine) label(s strategy) string {
	var parts []string
	switch s.raw {
	case rawPure:
		if s.start == e.topRaw() {
			parts = append(parts, "原料直接买")
		} else {
			parts = append(parts, "原料全从 "+shortName(s.start)+" 转化")
		}
	case rawBid:
		parts = append(parts, "原料挂买单收")
	default:
		parts = append(parts, "原料混合最优")
	}
	switch {
	case s.depth == 0:
		parts = append(parts, "下级料自动比价")
	case s.depth == 1:
		parts = append(parts, "下级料买现成")
	case s.depth >= len(e.chain)-1 || !e.chain[s.depth].makeable:
		parts = append(parts, "全链自炼")
	default:
		parts = append(parts, fmt.Sprintf("自炼到 %s、%s 买现成",
			shortName(e.chain[s.depth-1].item), shortName(e.chain[s.depth].item)))
	}
	tiers := e.focusTiers()
	switch {
	case s.focus == 0:
		parts = append(parts, "不开专注")
	case s.focus == setOf(tiers...):
		parts = append(parts, "全链专注")
	case s.focus == setOf(tiers[0]):
		parts = append(parts, "只顶层专注")
	default:
		ts := make([]string, 0, 5)
		for _, t := range s.focus.list() {
			ts = append(ts, "T"+strconv.Itoa(t))
		}
		parts = append(parts, "专注 "+strings.Join(ts, "+"))
	}
	return strings.Join(parts, " · ")
}

// shortName:T8_ORE_LEVEL4@4 → 8.4 矿式的短名 T8.4;无附魔的 T3_METALBAR → T3。
func shortName(id string) string {
	_, t, en, ok := ParseItemID(id)
	if !ok {
		return id
	}
	if t < MinEnchantTier {
		return "T" + strconv.Itoa(t)
	}
	return "T" + strconv.Itoa(t) + "." + strconv.Itoa(en)
}
