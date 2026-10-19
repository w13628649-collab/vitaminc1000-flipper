package refine

import (
	"math"
	"testing"

	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
)

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// C1/C5:按规则原文 PB/(100+PB) 复算设计 §4.7 里的五个数。
func TestReturnRate按规则原文(t *testing.T) {
	cases := []struct {
		pb   float64
		want float64
	}{
		{117, 0.53917}, // 专精城 + 专注:18 + 40 + 59
		{58, 0.36709},  // 专精城不开专注
		{127, 0.55947}, // 再加每日 +10;表格下拉写的 0.558 是算错了
		{40, 0.28571},  // 个人岛(绑专精城)不开专注
		{77, 0.43503},  // 非专精城 + 专注
	}
	for _, c := range cases {
		if got := ReturnRate(c.pb); !near(got, c.want, 5e-6) {
			t.Fatalf("PB=%v 回收率 %.6f,想要 %.5f", c.pb, got, c.want)
		}
	}
	if got := ReturnRate(0); got != 0 {
		t.Fatalf("没有加成时回收率应为 0,得到 %v", got)
	}
}

// 加成是直接相加的,各种组合要落到上面那几个 PB 上。
func TestBonus组合(t *testing.T) {
	cases := []struct {
		name string
		b    Bonus
		want float64
	}{
		{"专精城 + 专注", Bonus{Specialty: true, Focus: true}, 117},
		{"专精城", Bonus{Specialty: true}, 58},
		{"专精城 + 专注 + 每日 10", Bonus{Specialty: true, Focus: true, Daily: 10}, 127},
		{"专精城 + 专注 + 每日 20", Bonus{Specialty: true, Focus: true, Daily: 20}, 137},
		{"非专精城", Bonus{}, 18},
		{"非专精城 + 专注", Bonus{Focus: true}, 77},
		// C7:个人岛没有 18 的基础加成,但保留专精 +40
		{"个人岛(绑专精城)", Bonus{Island: true, Specialty: true}, 40},
		{"个人岛(绑专精城)+ 专注", Bonus{Island: true, Specialty: true, Focus: true}, 99},
	}
	for _, c := range cases {
		if got := c.b.Total(); got != c.want {
			t.Fatalf("%s:加成 %v,想要 %v", c.name, got, c.want)
		}
	}
	// C5 / C7 的百分数:15.25%、57.81%(+20)、个人岛 28.6% / 49.7%
	for _, c := range []struct {
		b    Bonus
		want float64
	}{
		{Bonus{}, 0.1525},
		{Bonus{Specialty: true, Focus: true, Daily: 20}, 0.5781},
		{Bonus{Island: true, Specialty: true}, 0.2857},
		{Bonus{Island: true, Specialty: true, Focus: true}, 0.4975},
	} {
		if got := ReturnRate(c.b.Total()); !near(got, c.want, 5e-5) {
			t.Fatalf("%+v 回收率 %.5f,想要 %.4f", c.b, got, c.want)
		}
	}
}

// D2:4714 × 0.5^(40000/10000) = 294.625,即满专精降到 1/16。
func TestFocusCost按规则原文(t *testing.T) {
	if got := FocusCost(4714, 40000); got != 294.625 {
		t.Fatalf("T8.4 满专精专注 %v,想要 294.625", got)
	}
	if got := FocusCost(4714, 0); got != 4714 {
		t.Fatalf("零专精专注 %v,想要 4714", got)
	}
	if got := FocusCost(4714, 10000); got != 2357 {
		t.Fatalf("FCE 10000 专注 %v,想要减半 2357", got)
	}
	// 和游戏数据里的写法等价:每 100 点 FCE 除一次 costreductionconstant
	const costReductionConstant = 1.00695555005672 // gamedata.xml 1174
	for _, fce := range []float64{0, 250, 3000, 12345, 40000} {
		viaConstant := 4714 / math.Pow(costReductionConstant, fce/100)
		if !near(FocusCost(4714, fce), viaConstant, 1e-6) {
			t.Fatalf("FCE=%v:0.5 次幂 %.6f 和常数写法 %.6f 对不上", fce, FocusCost(4714, fce), viaConstant)
		}
	}
	if err := CheckFCE(40000); err != nil {
		t.Fatalf("满专精应当合法: %v", err)
	}
	if err := CheckFCE(40001); err == nil {
		t.Fatal("FCE 超过 40000 应当报错")
	}
	if err := CheckFCE(-1); err == nil {
		t.Fatal("FCE 为负应当报错")
	}
}

// D3:作者 2026-10-19 抓包读到的纤维线(T5/T6 满级,T7/T8 刚解锁)
func TestSpecFCE(t *testing.T) {
	fiber := [5]int{60, 100, 100, 1, 1} // 和 262 → 7860
	want := map[int]float64{4: 22860, 5: 32860, 6: 32860, 7: 8110, 8: 8110}
	got := SpecFCE(fiber)
	for tier, w := range want {
		if got[tier] != w {
			t.Errorf("T%d FCE = %v, want %v", tier, got[tier], w)
		}
	}
	if full := SpecFCE([5]int{100, 100, 100, 100, 100}); full[8] != MaxFCE || full[4] != MaxFCE {
		t.Fatalf("全满应当正好 %v: %v", MaxFCE, full)
	}
	for _, c := range []struct {
		lv   [5]int
		want int
	}{{fiber, 8}, {[5]int{15, 27, 2, 0, 0}, 6}, {[5]int{21, 1, 0, 0, 0}, 5}, {[5]int{}, 3}} {
		if got := SpecUnlockedTier(c.lv); got != c.want {
			t.Errorf("%v 解锁到 T%d, want T%d", c.lv, got, c.want)
		}
	}
}

// E1/E3:T8.4 条 IV 4096,按 500 是 2304,按 1000 是 4608;T2 免费。
func TestStationFee按规则原文(t *testing.T) {
	cases := []struct {
		name string
		iv   float64
		tier int
		fee  float64
		want float64
	}{
		{"T8.4 条 @500", 4096, 8, 500, 2304},
		{"T8.4 条 @1000", 4096, 8, 1000, 4608},
		{"T4 条 @500", 16, 4, 500, 9},
		{"T3 条 @500", 8, 3, 500, 4.5},
		{"T2 条免费", 4, 2, 1000, 0},
	}
	for _, c := range cases {
		if got := StationFee(c.iv, c.tier, c.fee); !near(got, c.want, 1e-9) {
			t.Fatalf("%s:站费 %v,想要 %v", c.name, got, c.want)
		}
	}
	if err := CheckStationFee(1000); err != nil {
		t.Fatalf("上限 1000 应当合法: %v", err)
	}
	if err := CheckStationFee(1001); err == nil {
		t.Fatal("超过 1000 应当报错(Foundations 之后的上限)")
	}
}

// B4–B6:8.3→8.4 矿的基础价是 748,440(wiki 写的 784,440 是笔误,B4′),
// k=1.154、主人费率 500 时实付 863,930,和表格 J27 手抄的 863,926 差不到 6 银。
func TestTransmuteCost按规则原文(t *testing.T) {
	const (
		base83to84 = 748_440 // items.xml 8734,T8_ORE_LEVEL4 的第二条配方
		ivT84Ore   = 409.6   // T8_ORE_LEVEL4 @itemvalue
	)
	// 主人费用:409.6 × 0.1125 × 5 = 230.4(B6 说的「约 230 银」)
	if got := TransmuteCost(0, 1, ivT84Ore, 500); !near(got, 230.4, 1e-9) {
		t.Fatalf("主人费用 %v,想要 230.4", got)
	}
	got := TransmuteCost(base83to84, 1.154, ivT84Ore, 500)
	if !near(got, 863_930, 0.5) {
		t.Fatalf("k=1.154 实付 %.2f,想要 863,930", got)
	}
	const sheetJ27 = 863_926
	if math.Abs(got-sheetJ27) > 6 {
		t.Fatalf("和表格 J27 差 %.2f 银,超过 6", got-sheetJ27)
	}
	// k=1.0(2023-10 论坛实测的口径)
	if got := TransmuteCost(base83to84, 1.0, ivT84Ore, 500); !near(got, 748_670.4, 1e-6) {
		t.Fatalf("k=1.0 实付 %v,想要 748,670.4", got)
	}
	if err := CheckTransmuteFactor(DefaultTransmuteFactor); err != nil {
		t.Fatalf("默认 k 应当合法: %v", err)
	}
	if err := CheckTransmuteFactor(115.4); err == nil {
		t.Fatal("k 录成百分数应当报错")
	}
}

// F3:挂卖 ×0.935、吃买单 ×0.96、挂买单 ×1.025、吃卖单 ×1,并且和 econ.Breakeven 一致。
func TestMarketFees与econ一致(t *testing.T) {
	cfg := conf.Default().Economics
	m := MarketFees(cfg)
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"吃卖单买入", m.TakeAsk, 1.0},
		{"挂买单收料", m.PostBid, 1.025},
		{"吃买单卖出", m.TakeBid, 0.96},
		{"挂卖单卖出", m.PostAsk, 0.935},
	} {
		if !near(c.got, c.want, 1e-12) {
			t.Fatalf("%s 系数 %v,想要 %v", c.name, c.got, c.want)
		}
	}

	pick := map[econ.Leg][2]float64{
		econ.Taker: {m.TakeAsk, m.TakeBid},
		econ.Maker: {m.PostBid, m.PostAsk},
	}
	for _, mode := range econ.Modes {
		got := pick[mode.Buy][0]/pick[mode.Sell][1] - 1
		if want := mode.Breakeven(cfg); !near(got, want, 1e-12) {
			t.Fatalf("%s:精炼系数算出的盈亏平衡 %.6f,econ 是 %.6f", mode.Label(), got, want)
		}
	}

	// 改税率走的是同一份逻辑:非会员 8% 时挂卖到手 0.895
	cfg.MarketTax = 0.08
	if got := MarketFees(cfg).PostAsk; !near(got, 0.895, 1e-12) {
		t.Fatalf("8%% 税时挂卖系数 %v,想要 0.895", got)
	}
}

// 每个未核实参数都要有编号和核对方法,否则 params_used.unverified 里给不出下一步。
func TestUnverifiedParams都写了核对方法(t *testing.T) {
	seen := map[string]bool{}
	for _, u := range UnverifiedParams {
		if u.Key == "" || u.Rule == "" || u.Check == "" {
			t.Fatalf("未核实参数缺字段: %+v", u)
		}
		if seen[u.Key] {
			t.Fatalf("未核实参数重复: %s", u.Key)
		}
		seen[u.Key] = true
	}
	for _, key := range []string{"fees.transmute_factor", "focus.fce", "focus.unlocked_tier",
		"fees.station_per100", "rrr.daily_bonus", "heart_returns"} {
		if !seen[key] {
			t.Fatalf("待核对清单里少了 %s", key)
		}
	}
}
