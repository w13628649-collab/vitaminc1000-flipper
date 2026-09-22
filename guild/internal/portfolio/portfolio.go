// Package portfolio 回答"这 1000 万该怎么分"。
//
// 一张按收益排序的列表回答不了这个问题,有两个原因:
//
//  1. 每条机会算出来的"可吃量"都假设**全部本金都投在它一个人身上**,
//     那一列加起来远超总资金,照着做会发现钱不够
//  2. 不同机会常常在抢**同一批流动性**。从 Fort Sterling 发往三个城的
//     三条路线,买的是同一个城市里同一批货;各算各的,一天要从那座城
//     买走三倍于模型自己允许的量,累计收益里三分之二是重复计数
//
// 这里做的是带容量账本的资金预算:按风险调整后的回报率从高到低
// 依次分配,每条只给它真正吃得下的那部分,并从它占用的流动性桶里扣掉。
// 输出是一条累计曲线——投入多少、拿回多少、边际收益衰减到哪里就该停。
package portfolio

import (
	"math"
	"sort"

	"albion-guild/internal/econ"
)

// Pool 是一份共享的流动性。同一个物品在同一个城市只有一份,
// 所有要动它的机会一起分。
type Pool struct {
	Key string
	// Capacity 是这个桶一天吃得下多少件(日成交量 × absorb_ratio)。
	Capacity float64
}

// Candidate 是待分配的一条机会。扫描器和套利引擎各自转成这个形状。
type Candidate struct {
	Key   string
	Label string
	Kind  string // flip(同城) | arb(跨城)

	// CostPerUnit 是一件货占用的本金。
	CostPerUnit   float64
	ProfitPerUnit float64
	// HoursPerRound 是这种执行方式跑完一轮要多久。决定一天能转几轮
	HoursPerRound float64
	// Pools 是这条机会要占用的流动性桶。同城两端在同一个城市,
	// 只有一个桶;跨城有产地和销地两个,取两者较小的剩余量
	Pools []Pool
	// Volatility 用来做风险调整,0 表示没有历史数据
	Volatility float64
	Payload    any
}

// Slice 是分到某条机会上的一份仓位。
type Slice struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  string `json:"kind"`

	// Qty 是一轮带多少,DailyQty 是一天总共搬多少。
	// 两者的比就是周转次数——这一点必须和机会榜同源,
	// 否则同一条路线在两个页面上差好几倍
	Qty         int64   `json:"qty"`
	DailyQty    float64 `json:"daily_qty"`
	TurnsPerDay float64 `json:"turns_per_day"`

	Capital     float64 `json:"capital"`
	DailyProfit float64 `json:"daily_profit"`
	// ROI 是这一份自己的日回报率(日收益 / 占用本金)。
	ROI float64 `json:"roi"`
	// RiskAdjROI 是排序真正用的那个数。累计曲线按它才是单调递减的,
	// 原始 ROI 不保证
	RiskAdjROI float64 `json:"risk_adj_roi"`
	Volatility float64 `json:"volatility"`

	// CumCapital/CumProfit 是加上这一份之后的累计值,前端直接拿来画曲线
	CumCapital float64 `json:"cum_capital"`
	CumProfit  float64 `json:"cum_profit"`
	CumROI     float64 `json:"cum_roi"`

	Payload any `json:"payload,omitempty"`
}

type Plan struct {
	Capital     int64   `json:"capital"`
	Deployed    float64 `json:"deployed"`
	Idle        float64 `json:"idle"`
	DailyProfit float64 `json:"daily_profit"`
	DailyROI    float64 `json:"daily_roi"`
	Slices      []Slice `json:"slices"`
	// Note 说明钱为什么没花完。闲置不是 bug,是市场深度不够——
	// 这件事必须说清楚,否则用户会以为工具算错了
	Note string `json:"note"`
}

// Options 控制怎么分。
type Options struct {
	Capital int64
	// MaxPerSlice 是单条机会最多占用总本金的比例。
	// 不设的话钱会全砸在第一条上,而"可吃量"本身只是个估计值——
	// 估错一次就全军覆没。默认 0.25,四条腿走路
	MaxPerSlice float64
	// RiskAversion 是风险厌恶系数。按 收益率/(1+λ×变异系数) 排序,
	// λ=0 就是纯按收益率排,λ 越大越偏向价格稳定的品种
	RiskAversion float64
	// MinSliceCapital 低于这个金额的仓位不值得单独开一条,跳过
	MinSliceCapital float64
}

func DefaultOptions(capital int64) Options {
	return Options{
		Capital:         capital,
		MaxPerSlice:     0.25,
		RiskAversion:    0.5,
		MinSliceCapital: 10_000,
	}
}

// Build 做资金分配。
//
// 贪心法:按风险调整后的本金效率排序,依次分配,同时维护一本
// 流动性账本。这不是严格最优(那是个带共享约束的背包问题),
// 但结果可解释——用户能看懂为什么是这个顺序、为什么这条只分到这么多。
func Build(candidates []Candidate, opt Options) Plan {
	if opt.Capital <= 0 {
		return Plan{Note: "本金为 0"}
	}
	perSliceCap := float64(opt.Capital)
	if opt.MaxPerSlice > 0 {
		perSliceCap = float64(opt.Capital) * opt.MaxPerSlice
	}

	// 流动性账本。同一个桶被多条机会引用时取最保守的那个容量——
	// 它们本该一致,不一致说明上游数据有出入,宁可少算
	remaining := map[string]float64{}
	for _, c := range candidates {
		for _, p := range c.Pools {
			if cur, ok := remaining[p.Key]; !ok || p.Capacity < cur {
				remaining[p.Key] = p.Capacity
			}
		}
	}

	ranked := append([]Candidate(nil), candidates...)
	sort.SliceStable(ranked, func(i, j int) bool {
		a, b := efficiency(ranked[i], opt.RiskAversion), efficiency(ranked[j], opt.RiskAversion)
		if a != b {
			return a > b
		}
		return ranked[i].Key < ranked[j].Key
	})

	plan := Plan{Capital: opt.Capital}
	left := float64(opt.Capital)
	budgetCapped, poolCapped := false, false

	for _, c := range ranked {
		if left < opt.MinSliceCapital || c.CostPerUnit <= 0 {
			continue
		}
		// 这条还能吃多少:受各个流动性桶的剩余量约束,取最紧的那个
		capacity := math.Inf(1)
		for _, p := range c.Pools {
			capacity = math.Min(capacity, remaining[p.Key])
		}
		if math.IsInf(capacity, 1) || capacity < 1 {
			if capacity < 1 {
				poolCapped = true
			}
			continue
		}

		budget := math.Min(left, perSliceCap)
		// 和机会榜走同一个 econ.Turnover,保证两个页面对同一条路线
		// 给出同样的数。以前组合页只算一轮,和榜单差 2–3 倍
		qty, turns, profit := econ.Turnover(
			econ.Unit{CostPerUnit: c.CostPerUnit, ProfitPerUnit: c.ProfitPerUnit},
			capacity, int64(budget), c.HoursPerRound)
		if qty < 1 {
			continue
		}
		capital := float64(qty) * c.CostPerUnit
		if capital < opt.MinSliceCapital {
			continue
		}
		if float64(qty) < capacity {
			budgetCapped = true
		}

		dailyQty := float64(qty) * turns
		left -= capital
		plan.Deployed += capital
		plan.DailyProfit += profit
		for _, p := range c.Pools {
			remaining[p.Key] -= dailyQty
		}

		s := Slice{
			Key: c.Key, Label: c.Label, Kind: c.Kind,
			Qty: qty, DailyQty: dailyQty, TurnsPerDay: turns,
			Capital: capital, DailyProfit: profit,
			Volatility: c.Volatility, Payload: c.Payload,
			CumCapital: plan.Deployed, CumProfit: plan.DailyProfit,
		}
		if capital > 0 {
			s.ROI = profit / capital
			s.RiskAdjROI = s.ROI / (1 + opt.RiskAversion*c.Volatility)
		}
		s.CumROI = plan.DailyProfit / plan.Deployed
		plan.Slices = append(plan.Slices, s)
	}

	plan.Idle = float64(opt.Capital) - plan.Deployed
	plan.DailyROI = plan.DailyProfit / float64(opt.Capital)
	plan.Note = explain(plan, budgetCapped, poolCapped)
	return plan
}

// efficiency 是排序用的风险调整本金效率:每投一银,一天赚多少。
//
// 用**回报率**排而不是绝对收益——资金有限时,该先占效率高的坑。
// 除以 (1+λ×变异系数) 把价格波动大的品种往后压:同样的账面回报,
// 价格飘的品种更可能在你挂单期间跑掉。
//
// 注意这里要算进周转:一轮赚得少但一天能转八轮的,效率可能更高。
func efficiency(c Candidate, lambda float64) float64 {
	if c.CostPerUnit <= 0 {
		return 0
	}
	turns := 1.0
	if c.HoursPerRound > 0 {
		turns = 24 / c.HoursPerRound
	}
	roi := c.ProfitPerUnit / c.CostPerUnit * turns
	return roi / (1 + lambda*c.Volatility)
}

func explain(p Plan, budgetCapped, poolCapped bool) string {
	switch {
	case len(p.Slices) == 0:
		return "没有可投的机会"
	case p.Idle > float64(p.Capital)*0.5:
		return "超过一半的钱没处放——市场深度不够,不是工具算错了。" +
			"要么扩大物品范围,要么接受资金闲置"
	case poolCapped:
		return "有机会因为同产地/同销地的流动性已经被前面的仓位吃掉而落选。" +
			"同一批货不能卖两遍,这是对的"
	case budgetCapped:
		return "部分机会受单条上限约束,没吃满。调高 max_per_slice 可以更集中," +
			"代价是更依赖单条估计的准确性"
	case p.Idle > 0:
		return "剩下的钱吃不下任何一条剩余机会的最小仓位"
	default:
		return "本金已全部部署"
	}
}
