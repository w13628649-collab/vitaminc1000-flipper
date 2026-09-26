package refine

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

// 参数不合法返回 error;缺价、深度不够不是 error(见 chain_test)。
func TestPlan参数校验(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Params)
		want string
	}{
		{"不可精炼的物品", func(p *Params) { p.Target = "T8_2H_FIRESTAFF" }, "不是本期能算的半成品"},
		{"原料不是精炼产物", func(p *Params) { p.Target = "T8_ORE_LEVEL4@4" }, "不是本期能算的半成品"},
		{"T3 以下不算", func(p *Params) { p.Target = "T3_METALBAR" }, "不是本期能算的半成品"},
		{"石头线不做", func(p *Params) { p.Target = "T8_STONEBLOCK_LEVEL4@4" }, "不是本期能算的半成品"},
		{"件数 0", func(p *Params) { p.Qty = 0 }, "件数"},
		{"没给 Now", func(p *Params) { p.Now = time.Time{} }, "Now"},
		{"k 录成百分数", func(p *Params) { p.Fees.TransmuteFactor = 115.4 }, "k"},
		{"站费超上限", func(p *Params) { p.Fees.StationPer100 = 1001 }, "使用费"},
		{"FCE 超满级", func(p *Params) { p.Focus.FCE[8] = 40001 }, "FCE"},
		{"余料估值", func(p *Params) { p.Leftover = "sell" }, "余料"},
		{"出售方式", func(p *Params) { p.Sell.Modes = []string{"dump"} }, "出售方式"},
		{"自定义专注不在链上", func(p *Params) { p.Focus.Mode = FocusCustom; p.Focus.CustomTiers = []int{3} }, "不在这条链上"},
	}
	for _, c := range cases {
		p := params("T8_METALBAR_LEVEL4@4")
		p.Focus.FCE = map[int]float64{}
		c.mut(&p)
		_, err := Plan(Input{Params: p, Recipes: EmbeddedRecipes()})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s:错误 %v,想要包含 %q", c.name, err, c.want)
		}
	}
	p := params("T8_METALBAR_LEVEL4@4")
	if _, err := Plan(Input{Params: p}); err == nil {
		t.Fatal("没有配方数据应当报错")
	}
}

// 同一份输入永远得到同一个结果(纯函数、确定性):JSON 逐字节相同。
// Books 是 map,遍历顺序每次不同,这里跑几遍来暴露依赖遍历顺序的地方。
func TestPlan确定性(t *testing.T) {
	for _, l := range Lines {
		books := synthBooks(l, l.City, 42)
		p := params(l.RefinedID(7, 3))
		p.Sources.BidOrders = true
		var first []byte
		for run := 0; run < 5; run++ {
			res, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes(),
				Volume: Volume{PerDay: 12, Days: 14, DaysWithData: 10, Known: true}})
			if err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(res)
			if err != nil {
				t.Fatal(err)
			}
			if first == nil {
				first = raw
			} else if !bytes.Equal(first, raw) {
				t.Fatalf("%s 第 %d 次结果和第 1 次不同", l.Key, run+1)
			}
		}
	}
}

// 候选去重:步骤清单相同的只留一条,平局按 id 留字典序小的;参照行固定跟在最后。
func TestPlan去重和参照行(t *testing.T) {
	l := Lines[1]
	books := synthBooks(l, l.City, 9)
	p := params(l.RefinedID(6, 4))
	p.Focus.Mode = FocusOff
	p.Top = 100
	res, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	sigs := map[string]string{}
	var refs []Candidate
	for i, c := range res.Candidates {
		if c.Reference != "" {
			refs = append(refs, c)
			continue
		}
		if len(refs) > 0 {
			t.Fatalf("第 %d 条普通候选排在参照行后面", i)
		}
		if prev, dup := sigs[c.sig]; dup {
			t.Fatalf("%s 和 %s 步骤相同却都留下了", prev, c.ID)
		}
		sigs[c.sig] = c.ID
		if len(c.Focus.Tiers) != 0 {
			t.Fatalf("不开专注模式下 %s 开了专注", c.ID)
		}
	}
	if len(refs) != 2 || refs[0].Reference != "market" || refs[1].Reference != "bid" {
		t.Fatalf("参照行 %+v", refs)
	}
	// 「全部吃卖单、不转化、下级料买现成」:只有直接买目标原料和下级料两种买入,没有转化
	for _, s := range refs[0].Steps {
		if s.Op == "transmute" || (s.Op == "buy" && s.For != "") {
			t.Fatalf("market 参照行不该转化: %+v", s)
		}
	}
	if !hasWarning(refs[1].Warnings, WarnBidRisk, l.RawID(6, 4)) {
		t.Fatalf("挂买单参照行要警告收不收得到: %+v", refs[1].Warnings)
	}
	// 平局按 id:两个 id 步骤相同时留字典序小的(auto < make1)
	ev := map[string]*evalResult{}
	for _, c := range res.Candidates {
		ev[c.ID] = c.ev
	}
	auto := findCandidate(t, res, "mixed|auto|focus:")
	e := mustEngine(t, Input{Params: p, Books: books})
	for d := 1; d <= 3; d++ {
		c := e.candidate(e.evaluate(strategy{raw: rawMixed, depth: d, focus: 0}), res.Sell, nil, "")
		if c.sig == auto.sig {
			if _, kept := ev[c.ID]; kept {
				t.Fatalf("%s 和 mixed|auto 步骤相同,应当只留 auto", c.ID)
			}
		}
	}
}

// params_used.unverified:每个待核对的参数都列出来,带这次用的值和是不是默认值。
func TestPlan列出未核实参数(t *testing.T) {
	l := Lines[0]
	p := params(l.RefinedID(8, 4))
	p.Fees.TransmuteFactor = 1.1
	p.Focus.FCE[8] = 12000
	res, err := Plan(Input{Params: p, Books: synthBooks(l, l.City, 1), Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]UnverifiedUsed{}
	for _, u := range res.ParamsUsed.Unverified {
		got[u.Key] = u
	}
	for _, u := range UnverifiedParams {
		if _, ok := got[u.Key]; !ok {
			t.Fatalf("params_used.unverified 里少了 %s", u.Key)
		}
	}
	if k := got["fees.transmute_factor"]; k.Value != 1.1 || k.IsDefault {
		t.Fatalf("k: %+v", k)
	}
	if s := got["fees.station_per100"]; s.Value != DefaultStationFeePer100 || !s.IsDefault {
		t.Fatalf("站费: %+v", s)
	}
	fce := got["focus.fce"]
	if fce.IsDefault || fce.Value.(map[int]float64)[8] != 12000 {
		t.Fatalf("FCE: %+v", fce)
	}
	raw, _ := json.Marshal(res.ParamsUsed)
	for _, key := range []string{`"unverified"`, `"transmute_factor":1.1`, `"now":"2026-09-26T10:00:00Z"`} {
		if !bytes.Contains(raw, []byte(key)) {
			t.Fatalf("params_used 的 JSON 里没有 %s:%s", key, raw)
		}
	}
}

// JSON 形状:界面和 API 靠这些字段名。
func TestPlanJSON字段(t *testing.T) {
	l := Lines[2]
	res, err := Plan(Input{Params: params(l.RefinedID(6, 1)), Books: synthBooks(l, l.City, 2), Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"target", "params_used", "data", "sell", "volume", "chain", "raw_sources", "focus", "candidates", "warnings", "recipe_source"} {
		if _, ok := m[k]; !ok {
			t.Fatalf("结果里没有 %s", k)
		}
	}
	c := m["candidates"].([]any)[0].(map[string]any)
	for _, k := range []string{"id", "label", "filled", "unit_cost", "total_cost", "upfront_capital", "profit", "focus",
		"silver_per_focus", "daily", "oldest_age_h", "age_level", "steps", "leftover", "warnings"} {
		if _, ok := c[k]; !ok {
			t.Fatalf("候选里没有 %s", k)
		}
	}
	ops := map[string]bool{}
	for _, s := range c["steps"].([]any) {
		ops[s.(map[string]any)["op"].(string)] = true
	}
	if !ops["buy"] || !ops["refine"] || !ops["sell"] {
		t.Fatalf("步骤清单 %v", ops)
	}
	d := m["data"].(map[string]any)
	for _, k := range []string{"missing", "truncated", "depth_unknown", "stale"} {
		if _, ok := d[k].([]any); !ok {
			t.Fatalf("data.%s 应当是数组(空也是 []),得到 %v", k, d[k])
		}
	}
}

// 深度未知(AODP 兜底,只有价格)的盘口按不限量算,并标出来;截断的盘口吃到最后一档要警告。
func TestPlan深度未知和截断(t *testing.T) {
	books := Books{}
	b := Book{Asks: askSide(1000, 1)}
	b.Asks.DepthUnknown, b.Asks.Source = true, "aodp"
	books[Key{"T5_ORE_LEVEL4@4", "Thetford"}] = b
	tr := Book{Asks: askSide(5000, 1, 5100, 2)}
	tr.Asks.Truncated = true
	books[Key{"T4_METALBAR_LEVEL4@4", "Thetford"}] = tr
	setAsks(books, "T5_METALBAR_LEVEL4@4", "Thetford", 30000, 5)
	p := params("T5_METALBAR_LEVEL4@4")
	p.Qty = 5
	p.Focus.Mode = FocusTop
	res, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	// T4.4 条做不出来(没有 T4.4 矿和 T3 条),自动比价就是买现成的,和 make1 同一条,去重后留 auto
	c := findCandidate(t, res, "mixed|auto|focus:5")
	if !c.Filled {
		t.Fatalf("深度未知按不限量算,应当拿满: %+v", c.Warnings)
	}
	if !hasWarning(c.Warnings, WarnDepthUnknown, "T5_ORE_LEVEL4@4") || !hasWarning(c.Warnings, WarnTruncated, "T4_METALBAR_LEVEL4@4") {
		t.Fatalf("警告 %+v", c.Warnings)
	}
	if !contains(res.Data.DepthUnknown, "T5_ORE_LEVEL4@4") || !contains(res.Data.Truncated, "T4_METALBAR_LEVEL4@4") {
		t.Fatalf("data %+v", res.Data)
	}
}

// 比 max_age_hours 还旧的盘口当作缺价,记进 data.stale。
func TestPlan过期盘口当缺价(t *testing.T) {
	books := Books{}
	setAsks(books, "T5_ORE_LEVEL4@4", "Thetford", 1000, 100)
	setAsks(books, "T4_METALBAR_LEVEL4@4", "Thetford", 5000, 100)
	old := books[Key{"T4_METALBAR_LEVEL4@4", "Thetford"}]
	old.Asks.Seen = testNow.Add(-100 * time.Hour)
	books[Key{"T4_METALBAR_LEVEL4@4", "Thetford"}] = old
	p := params("T5_METALBAR_LEVEL4@4")
	p.Focus.Mode = FocusTop
	res, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(res.Data.Stale, "T4_METALBAR_LEVEL4@4") {
		t.Fatalf("data.stale = %v", res.Data.Stale)
	}
	c := findCandidate(t, res, "mixed|make1|focus:5")
	if c.Filled || !hasWarning(c.Warnings, WarnMissing, "T4_METALBAR_LEVEL4@4") {
		t.Fatalf("过期的下级料应当当作缺价: %+v", c.Warnings)
	}
	p.Sources.MaxAgeHours = 200
	res, err = Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	c = findCandidate(t, res, "mixed|auto|focus:5") // 能买了,auto 和 make1 相同,去重后留 auto
	if !c.Filled || c.AgeLevel != "red" {
		t.Fatalf("放宽到 200 小时后应当能用、标红: filled=%v age=%s", c.Filled, c.AgeLevel)
	}
}

// 一边的阶梯可能拼了几次浏览(续页之前那一页、串城时暂停剔除留下的旧单):每一档带自己的
// 观测时刻(Ladder.LevelSeen)。步骤和候选的新鲜度按实际吃到的最旧那一档报;比 max_age_hours
// 还旧的档单独不用,不连累同一边新的档(审查 finding 2)。
func TestPlan按档记观测时刻(t *testing.T) {
	books := Books{}
	ore := Book{Asks: Ladder{Levels: lv(1000, 2, 1100, 100), Seen: testNow.Add(-time.Hour),
		LevelSeen: []time.Time{testNow.Add(-10 * time.Hour), testNow.Add(-time.Hour)}, Source: "capture"}}
	books[Key{"T5_ORE_LEVEL4@4", "Thetford"}] = ore
	setAsks(books, "T4_METALBAR_LEVEL4@4", "Thetford", 5000, 100)
	setAsks(books, "T5_METALBAR_LEVEL4@4", "Thetford", 30000, 5)
	p := params("T5_METALBAR_LEVEL4@4")
	p.Focus.Mode = FocusOff
	buy := func(res *Result) Step {
		t.Helper()
		for _, s := range findCandidate(t, res, "mixed|auto|focus:").Steps {
			if s.Op == "buy" && s.Item == "T5_ORE_LEVEL4@4" {
				return s
			}
		}
		t.Fatal("没有买 T5.4 矿")
		return Step{}
	}
	res, err := Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	s := buy(res)
	if s.PriceFrom != 1000 || s.AgeH == nil || *s.AgeH != 10 {
		t.Fatalf("吃到了 10 小时前那一档,步骤年龄应是 10h: %+v", s)
	}
	c := findCandidate(t, res, "mixed|auto|focus:")
	if c.AgeLevel != "yellow" || c.OldestAgeH == nil || *c.OldestAgeH != 10 {
		t.Fatalf("候选的新鲜度按最旧那一档: %s %v", c.AgeLevel, c.OldestAgeH)
	}

	// 数据最大年龄 5h:10 小时前那一档不用,1 小时前那档照用,整边不算缺价
	p.Sources.MaxAgeHours = 5
	res, err = Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	s = buy(res)
	if s.PriceFrom != 1100 || *s.AgeH != 1 || len(res.Data.Stale) != 0 {
		t.Fatalf("只该丢掉太旧的那一档: %+v stale=%v", s, res.Data.Stale)
	}

	// LevelSeen 和 Levels 对不上长度:不认,整边按 Seen 算
	bad := ore
	bad.Asks.LevelSeen = bad.Asks.LevelSeen[:1]
	books[Key{"T5_ORE_LEVEL4@4", "Thetford"}] = bad
	p.Sources.MaxAgeHours = DefaultMaxAgeHours
	res, err = Plan(Input{Params: p, Books: books, Recipes: EmbeddedRecipes()})
	if err != nil {
		t.Fatal(err)
	}
	if s := buy(res); *s.AgeH != 1 {
		t.Fatalf("LevelSeen 长度不对时按整边的 Seen: %+v", s)
	}
}

// 步骤清单是账本:每一步的金额加起来就是这一批的稳态成本;转化次数和买来转化的件数对得上;
// 精炼一步的材料消耗 = 次数 × count × (1 − r)。
func Test步骤清单的账对得上(t *testing.T) {
	for _, l := range Lines {
		for seed := uint64(1); seed <= 4; seed++ {
			p := params(l.RefinedID(8, int(seed%5)))
			p.Sources.BidOrders = true
			p.Top = 100
			res, err := Plan(Input{Params: p, Books: synthBooks(l, l.City, seed), Recipes: EmbeddedRecipes()})
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range res.Candidates {
				sum := 0.0
				bought := map[string]float64{} // 买来转化成 X 的件数
				made := map[string]float64{}   // 转化出来的 X
				for _, s := range c.Steps {
					switch s.Op {
					case "buy", "bid":
						sum += s.Cost
						if s.For != "" {
							bought[s.For] += s.Qty
						}
						lvq := 0.0
						for _, x := range s.Levels {
							lvq += x.Qty
						}
						assertNear(t, c.ID+" 买入各档件数", lvq, s.Qty, 1e-9)
					case "transmute":
						sum += s.Cost
						made[s.To] += s.Qty
					case "refine":
						sum += s.Cost
						assertNear(t, c.ID+" 站费", s.Cost, s.StationFee, 1e-9)
					}
				}
				assertNear(t, c.ID+" 步骤金额合计", sum, c.TotalCost, 1e-6*math.Max(1, c.TotalCost))
				for target, q := range bought {
					if made[target] < q-1e-9 {
						t.Fatalf("%s:买了 %.3f 件来转成 %s,转化步骤只转出 %.3f", c.ID, q, target, made[target])
					}
				}
				tail := 0.0
				for _, lo := range c.Leftover {
					tail += lo.Cost
				}
				assertNear(t, c.ID+" 开工本金", c.UpfrontCapital, c.TotalCost+tail, 1e-6*math.Max(1, c.TotalCost))
			}
		}
	}
}

// PremiumSilverPerFocus:金价 13,554 时约 169 银/点(D6)。
func TestPremium门槛(t *testing.T) {
	assertNear(t, "premium 门槛", PremiumSilverPerFocus(13_554), 169.425, 1e-9)
}
