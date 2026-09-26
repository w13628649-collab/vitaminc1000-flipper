package refine

import (
	"testing"
)

// 日化的几个上限(设计 §2.5,审查 finding 1 / 3 / 4)。盘口都是合成的。

// dailyBooks:T5.4 钢条 @ Thetford。T4.4 条买现成(做不出来:没有 T4.4 矿和 T3 条),
// 所以不开专注时只有一条路线;卖单 20,000 × 5,买单前三件像样、之后断崖到 100。
func dailyBooks() Books {
	b := Books{}
	setAsks(b, "T5_ORE_LEVEL4@4", "Thetford", 1000, 100, 1100, 100)
	setAsks(b, "T4_METALBAR_LEVEL4@4", "Thetford", 5000, 100)
	setAsks(b, "T5_METALBAR_LEVEL4@4", "Thetford", 20000, 5)
	setBids(b, "T5_METALBAR_LEVEL4@4", "Thetford", 15000, 2, 14800, 1, 100, 500)
	// 批末余料按「砸买单」估值时要用到原料和下级料的买单
	setBids(b, "T5_ORE_LEVEL4@4", "Thetford", 600, 100)
	setBids(b, "T4_METALBAR_LEVEL4@4", "Thetford", 3000, 100)
	return b
}

func dailyPlan(t *testing.T, mut func(*Params), vol Volume) (*Result, Candidate) {
	t.Helper()
	p := params("T5_METALBAR_LEVEL4@4")
	p.Focus.Mode = FocusOff
	if mut != nil {
		mut(&p)
	}
	res, err := Plan(Input{Params: p, Books: dailyBooks(), Recipes: EmbeddedRecipes(), Volume: vol})
	if err != nil {
		t.Fatal(err)
	}
	c := findCandidate(t, res, "mixed|auto|focus:")
	if !c.Filled || c.Daily == nil {
		t.Fatalf("候选应当拿满、算得出日化: %+v", c)
	}
	return res, c
}

// 成交量、本金都允许做很多时,日化也只算到这一批的件数:单根成本只按这一批的量沿阶梯吃过,
// 不能拿它外推到一天几千件(finding 1:低 tier 不开专注的格子曾被外推到一天上万件,
// 排在开专注的前面)。
func Test日化件数不超过这一批(t *testing.T) {
	_, c := dailyPlan(t, nil, Volume{PerDay: 10000, Days: 14, DaysWithData: 14, Known: true})
	d := c.Daily
	if d.Binding != "qty" || d.Units != 10 || d.Limits.Qty == nil || *d.Limits.Qty != 10 {
		t.Fatalf("应当卡在这一批的 10 件: %+v", d)
	}
	if d.Limits.Volume == nil || *d.Limits.Volume != 3000 {
		t.Fatalf("成交量上限照样列出来: %+v", d.Limits)
	}
	assertNear(t, "日化 = 10 × 每件利润", d.Profit, 10*c.Profit.Post.Unit, 1e-6)
	if !hasWarning(c.Warnings, WarnQtyBound, "") {
		t.Fatalf("卡在批量上要提示把件数调大: %+v", c.Warnings)
	}

	// 周转要 2 天:一批 10 件,每天 5 件
	_, c = dailyPlan(t, func(p *Params) { p.Daily.TurnoverDays = 2 }, Volume{PerDay: 10000, Days: 14, Known: true})
	if c.Daily.Binding != "qty" || c.Daily.Units != 5 {
		t.Fatalf("周转 2 天时每天 5 件: %+v", c.Daily)
	}

	// 成交量比批量小:卡在成交量,不提示批量
	_, c = dailyPlan(t, nil, Volume{PerDay: 10, Days: 14, Known: true})
	if c.Daily.Binding != "volume" || c.Daily.Units != 3 || hasWarning(c.Warnings, WarnQtyBound, "") {
		t.Fatalf("成交量卡住: %+v %+v", c.Daily, c.Warnings)
	}

	// 专注、成交量、本金都不设限:照样有批量这一道上限,binding 不会是空的
	_, c = dailyPlan(t, nil, Volume{})
	if c.Daily.Binding != "qty" || c.Daily.Units != 10 || !hasWarning(c.Warnings, WarnQtyBound, "") {
		t.Fatalf("什么都不设限时按一批算: %+v", c.Daily)
	}
}

// 本金上限要先扣掉批末余料占着的钱(finding 3):余料开工前就得备齐、做完还留在手里,
// 这部分本金周转不起来。开工本金超过本金时要警告。
func Test日化本金扣掉批末余料(t *testing.T) {
	for _, mode := range []string{LeftoverCarry, LeftoverBid} {
		// 先不设本金,拿到开工本金和余料
		_, c0 := dailyPlan(t, func(p *Params) { p.Leftover = mode }, Volume{})
		tied, loss := 0.0, 0.0
		for _, lo := range c0.Leftover {
			tied += lo.Cost
			loss += lo.Loss
		}
		if tied <= 0 {
			t.Fatalf("%s:应当有批末余料: %+v", mode, c0.Leftover)
		}
		steady := (c0.TotalCost - loss) / 10 // 稳态单根成本:不含余料的估值损失
		assertNear(t, mode+" 开工本金 = 稳态 + 余料", c0.UpfrontCapital, steady*10+tied, 1e-6)

		// 本金只比开工本金少一点:一批备不齐,按扣掉余料后的本金算
		capital := c0.UpfrontCapital - 1
		_, c := dailyPlan(t, func(p *Params) { p.Leftover = mode; p.Daily.Capital = capital }, Volume{})
		want := (capital - tied) / steady
		if c.Daily.Limits.Capital == nil {
			t.Fatalf("%s:应当有本金上限", mode)
		}
		assertNear(t, mode+" 本金上限", *c.Daily.Limits.Capital, want, 1e-9)
		if c.Daily.Binding != "capital" || !hasWarning(c.Warnings, WarnCapitalShort, "") {
			t.Fatalf("%s:开工本金超过本金,应当卡在本金并警告: %+v %+v", mode, c.Daily, c.Warnings)
		}
		// 不扣余料的旧算法会报 capital / 单根成本,比这里多
		if old := capital / c.UnitCost; !(old > want) {
			t.Fatalf("%s:扣掉余料应当更少: 旧 %.3f 新 %.3f", mode, old, want)
		}

		// 本金还不够备余料:一件都做不了
		_, c = dailyPlan(t, func(p *Params) { p.Leftover = mode; p.Daily.Capital = tied / 2 }, Volume{})
		if *c.Daily.Limits.Capital != 0 || c.Daily.Units != 0 || c.Daily.Profit != 0 {
			t.Fatalf("%s:本金不够备余料时日化是 0: %+v", mode, c.Daily)
		}

		// 本金充足:不警告,也不卡在本金
		_, c = dailyPlan(t, func(p *Params) { p.Leftover = mode; p.Daily.Capital = 1e9 }, Volume{})
		if c.Daily.Binding == "capital" || hasWarning(c.Warnings, WarnCapitalShort, "") {
			t.Fatalf("%s:本金充足: %+v %+v", mode, c.Daily, c.Warnings)
		}
	}
}

// 吃买单只卖得掉断崖之前那几件(finding 4):每件利润按卖掉的那几件算,这一批的利润只算它们,
// 卖不掉的按成本留在手里;主出售方式是吃买单时,日化也以它封顶。
func Test吃买单按卖得掉的件数入账(t *testing.T) {
	res, c := dailyPlan(t, func(p *Params) { p.Sell.Modes = []string{SellTake, SellPost} }, Volume{})
	tk := c.Profit.Take
	if tk == nil || res.Sell.Take.MaxQty == nil || *res.Sell.Take.MaxQty != 3 {
		t.Fatalf("断崖之前 3 件: %+v %+v", tk, res.Sell.Take)
	}
	net := (2*15000.0 + 14800) / 3 * 0.96
	assertNear(t, "吃买单到手均价", tk.Net, net, 1e-6)
	assertNear(t, "每件利润", tk.Unit, net-c.UnitCost, 1e-6)
	if tk.Sold != 3 {
		t.Fatalf("只卖得掉 3 件: %+v", tk)
	}
	assertNear(t, "这一批的利润只算卖掉的", tk.Total, 3*tk.Unit, 1e-6)
	// 挂卖按整批
	if c.Profit.Post == nil || c.Profit.Post.Sold != 10 {
		t.Fatalf("挂卖按整批 10 件: %+v", c.Profit.Post)
	}
	assertNear(t, "挂卖这一批的利润", c.Profit.Post.Total, 10*c.Profit.Post.Unit, 1e-6)

	var sell *Step
	for i := range c.Steps {
		if c.Steps[i].Op == "sell" {
			sell = &c.Steps[i]
		}
	}
	if sell == nil || sell.Mode != SellTake || sell.Qty != 3 || sell.Unsold != 7 {
		t.Fatalf("出售步骤要写明卖 3 件、剩 7 件: %+v", sell)
	}
	d := c.Daily
	if d.Binding != "take" || d.Units != 3 || d.Limits.Take == nil || *d.Limits.Take != 3 {
		t.Fatalf("吃买单为主时日化以卖得掉的件数封顶: %+v", d)
	}
	assertNear(t, "日化", d.Profit, 3*tk.Unit, 1e-6)

	// 主出售方式是挂卖时,没有 take 这一道上限
	_, c = dailyPlan(t, nil, Volume{})
	if c.Daily.Limits.Take != nil || c.Daily.Binding != "qty" {
		t.Fatalf("挂卖为主时不该有 take 上限: %+v", c.Daily)
	}
	for _, s := range c.Steps {
		if s.Op == "sell" && (s.Qty != 10 || s.Unsold != 0) {
			t.Fatalf("挂卖按整批出: %+v", s)
		}
	}
}
