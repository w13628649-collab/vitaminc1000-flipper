package portfolio

import (
	"math"
	"testing"
)

func cand(key string, cost, profit float64, maxQty int64, vol float64) Candidate {
	return Candidate{Key: key, Label: key, Kind: "flip",
		CostPerUnit: cost, ProfitPerUnit: profit, MaxQty: maxQty, Volatility: vol}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// 每条机会的"可吃量"都假设全部本金投在它身上,加起来远超总资金。
// 组合要做的就是把这件事摆平。
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

// 单条上限防的是"可吃量估错一次就全军覆没"。
func TestSingleSliceIsCapped(t *testing.T) {
	opt := DefaultOptions(10_000_000)
	opt.MaxPerSlice = 0.25
	plan := Build([]Candidate{
		cand("胖", 1000, 200, 1_000_000, 0), // 一条就能吃下全部本金
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
		// 单件赚得多,但一件要 10000 本金,回报率只有 2%
		cand("贵", 10_000, 200, 1_000_000, 0),
		// 单件只赚 50,但一件才 500 本金,回报率 10%
		cand("便宜", 500, 50, 1_000_000, 0),
	}, DefaultOptions(10_000_000))

	if plan.Slices[0].Key != "便宜" {
		t.Fatalf("排第一的是 %s,应该是回报率高的那个", plan.Slices[0].Key)
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
	// λ=0 就是纯按收益排,回到原顺序
	opt.RiskAversion = 0
	if Build([]Candidate{
		cand("飘", 1000, 100, 1_000_000, 0.8),
		cand("稳", 1000, 90, 1_000_000, 0.0),
	}, opt).Slices[0].Key != "飘" {
		t.Fatal("不考虑风险时应该纯按收益排")
	}
}

// 累计曲线要单调,前端直接拿它画图。
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
	// 边际回报递减:后面的仓位回报率不该比前面高
	for i := 1; i < len(plan.Slices); i++ {
		if plan.Slices[i].CumROI > plan.Slices[i-1].CumROI+1e-9 {
			t.Fatalf("第 %d 条把组合回报率拉高了,排序有问题", i)
		}
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
