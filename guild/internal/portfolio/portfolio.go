// Package portfolio 回答"这 1000 万该怎么分"。
//
// 一张按收益排序的列表回答不了这个问题。每条机会算出来的"可吃量"
// 都假设**全部本金都投在它一个人身上**,所以那一列加起来远超总资金——
// 直接照着做会发现钱不够。
//
// 这里做的是资金预算:按本金回报率从高到低依次分配,每条只给它
// 真正吃得下的那部分,分完为止。输出是一条累计曲线——投入多少、
// 拿回多少、边际收益衰减到哪里就该停。
package portfolio

import (
	"math"
	"sort"
)

// Candidate 是待分配的一条机会。扫描器和套利引擎各自转成这个形状。
type Candidate struct {
	Key   string
	Label string
	Kind  string // flip(同城) | arb(跨城)

	// CostPerUnit 是一件货占用的本金,MaxQty 是市场吃得下的上限
	// (**与本金无关**,纯粹是流动性约束)
	CostPerUnit   float64
	MaxQty        int64
	ProfitPerUnit float64
	// Volatility 用来做风险调整,0 表示没有历史数据
	Volatility float64
	Payload    any
}

// Slice 是分到某条机会上的一份仓位。
type Slice struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Kind  string `json:"kind"`

	Qty         int64   `json:"qty"`
	Capital     float64 `json:"capital"`
	DailyProfit float64 `json:"daily_profit"`
	// ROI 是这一份自己的回报率,MarginalROI 是把它加进组合后
	// 组合整体回报率的增量——递减到不值得时就该停手
	ROI        float64 `json:"roi"`
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
	// RiskAversion 是风险厌恶系数。按 收益/(1+λ×变异系数) 排序,
	// λ=0 就是纯按收益排,λ 越大越偏向价格稳定的品种
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
// 贪心法:按风险调整后的本金效率排序,依次分配。
// 这不是严格最优(那是个背包问题),但在"每条机会的容量都远小于
// 总资金"这个前提下,贪心和最优的差距可以忽略,而且结果可解释——
// 用户能看懂为什么是这个顺序。
func Build(candidates []Candidate, opt Options) Plan {
	if opt.Capital <= 0 {
		return Plan{Note: "本金为 0"}
	}
	perSliceCap := float64(opt.Capital)
	if opt.MaxPerSlice > 0 {
		perSliceCap = float64(opt.Capital) * opt.MaxPerSlice
	}

	ranked := append([]Candidate(nil), candidates...)
	sort.SliceStable(ranked, func(i, j int) bool {
		return efficiency(ranked[i], opt.RiskAversion) > efficiency(ranked[j], opt.RiskAversion)
	})

	plan := Plan{Capital: opt.Capital}
	remaining := float64(opt.Capital)
	capped := false

	for _, c := range ranked {
		if remaining < opt.MinSliceCapital || c.CostPerUnit <= 0 || c.MaxQty < 1 {
			continue
		}
		budget := math.Min(remaining, perSliceCap)
		qty := int64(math.Floor(budget / c.CostPerUnit))
		if qty > c.MaxQty {
			qty = c.MaxQty
		} else if qty < c.MaxQty {
			capped = true // 这条是被预算卡住的,不是市场吃不下
		}
		if qty < 1 {
			continue
		}
		capital := float64(qty) * c.CostPerUnit
		if capital < opt.MinSliceCapital {
			continue
		}
		profit := float64(qty) * c.ProfitPerUnit

		remaining -= capital
		plan.Deployed += capital
		plan.DailyProfit += profit

		s := Slice{
			Key: c.Key, Label: c.Label, Kind: c.Kind,
			Qty: qty, Capital: capital, DailyProfit: profit,
			Volatility: c.Volatility, Payload: c.Payload,
			CumCapital: plan.Deployed, CumProfit: plan.DailyProfit,
		}
		if capital > 0 {
			s.ROI = profit / capital
		}
		s.CumROI = plan.DailyProfit / plan.Deployed
		plan.Slices = append(plan.Slices, s)
	}

	plan.Idle = float64(opt.Capital) - plan.Deployed
	plan.DailyROI = plan.DailyProfit / float64(opt.Capital)
	plan.Note = explain(plan, capped, len(plan.Slices))
	return plan
}

// efficiency 是排序用的风险调整本金效率:每投一银,一天赚多少。
//
// 用**回报率**排而不是绝对收益——资金有限时,该先占效率高的坑。
// 除以 (1+λ×变异系数) 把价格波动大的品种往后压:同样的账面回报,
// 价格飘的品种更可能在你挂单期间跑掉。
func efficiency(c Candidate, lambda float64) float64 {
	if c.CostPerUnit <= 0 {
		return 0
	}
	roi := c.ProfitPerUnit / c.CostPerUnit
	return roi / (1 + lambda*c.Volatility)
}

func explain(p Plan, capped bool, n int) string {
	switch {
	case n == 0:
		return "没有可投的机会"
	case p.Idle > float64(p.Capital)*0.5:
		return "超过一半的钱没处放——市场深度不够,不是工具算错了。" +
			"要么扩大物品范围,要么接受资金闲置"
	case capped:
		return "部分机会受单条上限约束,没吃满。调高 max_per_slice 可以更集中,代价是更依赖单条估计的准确性"
	case p.Idle > 0:
		return "剩下的钱吃不下任何一条剩余机会的最小仓位"
	default:
		return "本金已全部部署"
	}
}
