package portfolio

import (
	"math"
	"testing"
)

// cand 造一条机会。pool 为空时给它一个独占的桶,
// 这样默认情况下各条互不抢流动性。
func cand(key string, cost, profit float64, capacity float64, vol float64, pools ...string) Candidate {
	if len(pools) == 0 {
		pools = []string{"pool:" + key}
	}
	ps := make([]Pool, len(pools))
	for i, p := range pools {
		ps[i] = Pool{Key: p, Capacity: capacity}
	}
	return Candidate{
		Key: key, Label: key, Kind: "flip",
		CostPerUnit: cost, ProfitPerUnit: profit,
		HoursPerRound: 24, // 默认一天一轮,和老口径一致,便于算预期值
		Pools:         ps, Volatility: vol,
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// 每条机会的"可吃量"都假设全部本金投在它身上,加起来远超总资金。
func TestDeployedNeverExceedsCapital(t *testing.T) {
	plan := Build([]Candidate{
		cand("a", 1000, 100, 100_000, 0),
		cand("b", 500, 40, 100_000, 0),
		cand("c", 2000, 150, 100_000, 0),
	}, DefaultOptions(10_000_000))

	if plan.Deployed > float64(plan.Capital) {
		t.Fatalf("部署了 %v,超过本金 %d", plan.Deployed, plan.Capital)
	}
	var sum float64
	for _, s := range plan.Slices {
		sum += s.Capital
	}
	if !near(sum, plan.Deployed) {
		t.Fatalf("各仓位加起来 %v,和总部署 %v 对不上", sum, plan.Deployed)
	}
}

// 这是最要命的一条:从同一座城发往三个方向的三条路线,买的是
// 同一批货。各算各的,一天要从那座城买走三倍于模型自己允许的量,
// 累计收益里三分之二是重复计数。
func TestSharedPoolIsNotCountedTwice(t *testing.T) {
	const capacity = 1000 // 产地一天只吃得下 1000 件
	src := "T5_METALBAR|Fort Sterling"
	plan := Build([]Candidate{
		cand("→Caerleon", 1000, 200, capacity, 0, src, "T5_METALBAR|Caerleon"),
		cand("→Lymhurst", 1000, 190, capacity, 0, src, "T5_METALBAR|Lymhurst"),
		cand("→Martlock", 1000, 180, capacity, 0, src, "T5_METALBAR|Martlock"),
	}, DefaultOptions(10_000_000))

	var moved float64
	for _, s := range plan.Slices {
		moved += s.DailyQty
	}
	if moved > capacity+1e-6 {
		t.Fatalf("一天从产地搬走 %.0f 件,超过它的容量 %d", moved, capacity)
	}
	// 收益也不能超过"那 1000 件按最赚的那条路走"
	if limit := capacity * 200.0; plan.DailyProfit > limit+1e-6 {
		t.Fatalf("日收益 %v 超过同一批货能赚的上限 %v", plan.DailyProfit, limit)
	}
	t.Logf("三条同产地路线共搬 %.0f 件(容量 %d),日收益 %.0f", moved, capacity, plan.DailyProfit)
}

// 挂单簿(Ladder)按"已经被吃掉多少"记账:各条机会的 Capacity 是自己限价以内的件数,
// 本来就不同。深的限价以内 1000 件、浅的 100 件,同一份卖单簿:
//   - 深的先分:吃掉最便宜的 1000 件,浅的限价以内一件不剩
//   - 浅的先分:吃掉最便宜的 100 件,深的限价以内还剩 900
//
// 以前按普通桶取最小容量,深的那条先分也只能分到 100 件
func TestLadderPoolTracksWhatIsEaten(t *testing.T) {
	ladder := func(c Candidate, capacity float64) Candidate {
		c.Pools = append(c.Pools, Pool{Key: "T5_CLOTH|Lymhurst|ask", Capacity: capacity, Ladder: true})
		return c
	}
	total := func(p Plan) (sum float64, byKey map[string]float64) {
		byKey = map[string]float64{}
		for _, s := range p.Slices {
			sum += s.DailyQty
			byKey[s.Key] = s.DailyQty
		}
		return sum, byKey
	}
	opt := DefaultOptions(1_000_000_000) // 本金不是瓶颈

	// 深的更赚钱,先分
	deepFirst := Build([]Candidate{
		ladder(cand("deep", 1000, 60, 100_000, 0), 1000),
		ladder(cand("shallow", 1000, 50, 100_000, 0), 100),
	}, opt)
	sum, got := total(deepFirst)
	if !near(got["deep"], 1000) || got["shallow"] != 0 || sum > 1000+1e-6 {
		t.Fatalf("深的先分应吃满 1000、浅的一件不剩,得到 %v", got)
	}

	// 浅的更赚钱,先分
	shallowFirst := Build([]Candidate{
		ladder(cand("deep", 1000, 50, 100_000, 0), 1000),
		ladder(cand("shallow", 1000, 60, 100_000, 0), 100),
	}, opt)
	sum, got = total(shallowFirst)
	if !near(got["shallow"], 100) || !near(got["deep"], 900) || !near(sum, 1000) {
		t.Fatalf("浅的先分吃 100、深的吃剩下的 900,得到 %v", got)
	}

	// 非 Ladder 的普通桶口径不变:同一个桶取最保守的容量
	plain := Build([]Candidate{
		cand("a", 1000, 60, 1000, 0, "same"),
		cand("b", 1000, 50, 100, 0, "same"),
	}, opt)
	if _, got := total(plain); !near(got["a"], 100) || got["b"] != 0 {
		t.Fatalf("普通桶仍取最小容量 100,得到 %v", got)
	}
}

// 组合页和机会榜必须对同一条路线给出同样的数。
// 以前组合页只算一轮,榜单按周转算,两边差 2–3 倍。
func TestSliceProfitMatchesTurnoverModel(t *testing.T) {
	c := cand("x", 1000, 100, 100_000, 0)
	c.HoursPerRound = 8 // 一天最多 3 轮
	plan := Build([]Candidate{c}, DefaultOptions(10_000_000))

	s := plan.Slices[0]
	if s.TurnsPerDay <= 1.0 {
		t.Fatalf("一轮 8 小时应该能转 3 轮,得到 %v", s.TurnsPerDay)
	}
	if !near(s.DailyProfit, float64(s.Qty)*s.TurnsPerDay*100) {
		t.Fatalf("日收益 %v 和 件数×轮数×单件利润 %v 对不上",
			s.DailyProfit, float64(s.Qty)*s.TurnsPerDay*100)
	}
	if !near(s.DailyQty, float64(s.Qty)*s.TurnsPerDay) {
		t.Fatalf("日搬运量 %v 对不上", s.DailyQty)
	}
}

// 单条上限防的是"可吃量估错一次就全军覆没"。
func TestSingleSliceIsCapped(t *testing.T) {
	opt := DefaultOptions(10_000_000)
	opt.MaxPerSlice = 0.25
	plan := Build([]Candidate{
		cand("胖", 1000, 200, 1_000_000, 0),
		cand("瘦", 1000, 10, 1_000_000, 0),
	}, opt)

	if plan.Slices[0].Capital > float64(plan.Capital)*0.25+1 {
		t.Fatalf("单条占用 %v,超过 25%% 上限", plan.Slices[0].Capital)
	}
	if len(plan.Slices) < 2 {
		t.Fatal("有上限就该分散到多条")
	}
}

// 按回报率排,不是按绝对收益——资金有限时该先占效率高的坑。
func TestRanksByReturnRateNotAbsoluteProfit(t *testing.T) {
	plan := Build([]Candidate{
		cand("贵", 10_000, 200, 1_000_000, 0), // 回报率 2%
		cand("便宜", 500, 50, 1_000_000, 0),    // 回报率 10%
	}, DefaultOptions(10_000_000))

	if plan.Slices[0].Key != "便宜" {
		t.Fatalf("排第一的是 %s,应该是回报率高的那个", plan.Slices[0].Key)
	}
}

// 周转要算进排序:一轮赚得少但一天转八轮的,效率可能更高。
func TestRankingAccountsForTurnover(t *testing.T) {
	slow := cand("慢", 1000, 100, 1_000_000, 0) // 回报率 10%/轮
	slow.HoursPerRound = 24                    // 一天 1 轮 → 10%/天
	fast := cand("快", 1000, 40, 1_000_000, 0)  // 回报率 4%/轮
	fast.HoursPerRound = 3                     // 一天 8 轮 → 32%/天

	plan := Build([]Candidate{slow, fast}, DefaultOptions(10_000_000))
	if plan.Slices[0].Key != "快" {
		t.Fatalf("排第一的是 %s,按日回报率应该是转得快的那个", plan.Slices[0].Key)
	}
}

// 同样的账面回报,价格飘的品种要往后压。
func TestVolatilityPushesCandidatesDown(t *testing.T) {
	opt := DefaultOptions(10_000_000)
	opt.RiskAversion = 1.0
	plan := Build([]Candidate{
		cand("飘", 1000, 100, 1_000_000, 0.8),
		cand("稳", 1000, 90, 1_000_000, 0.0),
	}, opt)
	if plan.Slices[0].Key != "稳" {
		t.Fatalf("排第一的是 %s,风险调整后稳的那个应该在前", plan.Slices[0].Key)
	}

	opt.RiskAversion = 0 // 不考虑风险时纯按收益排
	plan = Build([]Candidate{
		cand("飘", 1000, 100, 1_000_000, 0.8),
		cand("稳", 1000, 90, 1_000_000, 0.0),
	}, opt)
	if plan.Slices[0].Key != "飘" {
		t.Fatal("λ=0 时应该纯按收益排")
	}
}

// 累计曲线要单调。注意**凹性只对风险调整后的回报率成立**——
// 排序用的是它,原始回报率不保证单调,界面上的说明也照这个写。
func TestCumulativeCurveIsMonotonic(t *testing.T) {
	plan := Build([]Candidate{
		cand("a", 1000, 100, 500, 0),
		cand("b", 1000, 80, 500, 0),
		cand("c", 1000, 60, 500, 0),
	}, DefaultOptions(10_000_000))

	var lastCap, lastProfit float64
	for i, s := range plan.Slices {
		if s.CumCapital < lastCap || s.CumProfit < lastProfit {
			t.Fatalf("第 %d 条累计值回退了:%+v", i, s)
		}
		lastCap, lastProfit = s.CumCapital, s.CumProfit
	}
	for i := 1; i < len(plan.Slices); i++ {
		if plan.Slices[i].RiskAdjROI > plan.Slices[i-1].RiskAdjROI+1e-9 {
			t.Fatalf("第 %d 条的风险调整回报率比前一条高,排序有问题", i)
		}
	}
}

// λ>0 时原始回报率可以是递增的。这不是 bug,是风险调整的必然结果——
// 文档和界面说明必须照这个写,不能说"曲线一定是凹的"。
func TestRawROICanRiseWhenRiskAdjusted(t *testing.T) {
	opt := DefaultOptions(10_000_000)
	opt.RiskAversion = 0.5
	plan := Build([]Candidate{
		cand("稳", 1000, 100, 2000, 0),   // 原始 10%,风险调整后 10%
		cand("飘", 1000, 130, 2000, 0.9), // 原始 13%,风险调整后 8.97%
	}, opt)

	if plan.Slices[0].Key != "稳" {
		t.Fatalf("排序应按风险调整后:%s", plan.Slices[0].Key)
	}
	if !(plan.Slices[1].ROI > plan.Slices[0].ROI) {
		t.Skip("这个场景没构造出原始回报率递增,换个数再说")
	}
	// 风险调整后仍必须递减
	if plan.Slices[1].RiskAdjROI > plan.Slices[0].RiskAdjROI {
		t.Fatal("风险调整后的回报率必须单调递减")
	}
}

// 钱花不完不是 bug,是市场深度不够。必须说清楚。
func TestIdleCapitalIsExplained(t *testing.T) {
	plan := Build([]Candidate{cand("小", 1000, 100, 10, 0)}, DefaultOptions(10_000_000))
	if plan.Idle <= 0 {
		t.Fatal("市场这么小,钱应该花不完")
	}
	if plan.Note == "" {
		t.Fatal("闲置资金必须给出解释")
	}
	t.Log(plan.Note)
}

func TestEmptyCandidatesGiveEmptyPlan(t *testing.T) {
	plan := Build(nil, DefaultOptions(10_000_000))
	if len(plan.Slices) != 0 || plan.DailyProfit != 0 {
		t.Fatalf("空输入应该给空计划,得到 %+v", plan)
	}
	if plan.Note == "" {
		t.Fatal("空计划也要说明原因")
	}
}
