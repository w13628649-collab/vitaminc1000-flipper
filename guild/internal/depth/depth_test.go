package depth

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// 总件数会被 1 银占位单骗,近价件数不会。这两组是实测数据。
func TestNearDepthSeparatesRealBidsFromPlaceholders(t *testing.T) {
	// T6_METALBAR_LEVEL4@4 @ Martlock:顶上 7 档共 11 件,第 8 档掉 79.6%,
	// 底下躺着 2,000+ 件 1 银的占位单
	thin := []Level{
		{260066, 1}, {260065, 2}, {260064, 1}, {260056, 1},
		{260053, 1}, {260039, 1}, {260033, 4},
		{53001, 15}, {53000, 1}, {52579, 30}, {52577, 15}, {50000, 1},
		{1, 2814},
	}
	s := Analyze(thin, 0.05)
	if s.Best != 260066 {
		t.Fatalf("最优买价 = %d,想要 260066", s.Best)
	}
	if s.QtyAtBest != 1 {
		t.Fatalf("最优档件数 = %d,想要 1", s.QtyAtBest)
	}
	if s.QtyNear != 11 {
		t.Fatalf("近价件数 = %d,想要 11", s.QtyNear)
	}
	if s.QtyTotal != 2887 {
		t.Fatalf("总件数 = %d,想要 2887", s.QtyTotal)
	}
	// 断崖要测到窗口外那一档,不是"下一档"(顶上几档只差 1 银)
	if !roughly(s.GapAfterNear, 0.796, 0.002) {
		t.Fatalf("窗口外落差 = %.4f,想要 ~0.796", s.GapAfterNear)
	}

	// 对照:T4_METALBAR @ Thetford 是连续阶梯
	thick := []Level{
		{285, 3116}, {284, 1999}, {283, 4927}, {281, 623}, {280, 4635},
		{279, 6591}, {276, 4418}, {275, 1265}, {274, 4995}, {273, 4810},
		{269, 7915},
		{200, 1054}, {100, 1232}, {51, 3210},
	}
	c := Analyze(thick, 0.05)
	if c.QtyNear != 37379 { // 285×0.95 = 270.75,269 那档落在窗口外
		t.Fatalf("大宗近价件数 = %d,想要 37379", c.QtyNear)
	}
	// 两者的总件数差不到 20 倍,近价件数差 4000 倍 —— 这就是要用近价的理由
	if c.QtyNear/s.QtyNear < 1000 {
		t.Fatalf("近价件数应当拉开数量级:大宗 %d vs 薄的 %d", c.QtyNear, s.QtyNear)
	}
}

// 卖单侧是升序,窗口要往上开。
func TestNearDepthHandlesAscendingSellLadder(t *testing.T) {
	sell := []Level{{357999, 12}, {358000, 81}, {359985, 3}, {359987, 15}, {500000, 9}}
	s := Analyze(sell, 0.05)
	if s.Best != 357999 || s.QtyAtBest != 12 {
		t.Fatalf("最优 %d × %d,想要 357999 × 12", s.Best, s.QtyAtBest)
	}
	if s.QtyNear != 111 { // 12+81+3+15,50 万那档在 5% 之外
		t.Fatalf("近价件数 = %d,想要 111", s.QtyNear)
	}
	if !roughly(s.GapAfterNear, 0.3966, 0.001) {
		t.Fatalf("窗口外落差 = %.4f,想要 ~0.3966", s.GapAfterNear)
	}
}

// 首档没货时不能拿它当锚,否则整条判据歪掉。
func TestNearDepthSkipsEmptyLeadingLevels(t *testing.T) {
	s := Analyze([]Level{{999999, 0}, {1000, 50}, {990, 20}}, 0.05)
	if s.Best != 1000 {
		t.Fatalf("最优价 = %d,想要 1000(跳过空档)", s.Best)
	}
	if s.QtyNear != 70 {
		t.Fatalf("近价件数 = %d,想要 70", s.QtyNear)
	}
}

func TestNearDepthEmptyBook(t *testing.T) {
	if s := Analyze(nil, 0.05); s.Best != 0 || s.QtyNear != 0 {
		t.Fatalf("空盘口应当全 0,得到 %+v", s)
	}
	if s := Analyze([]Level{{100, 0}}, 0.05); s.Best != 0 {
		t.Fatalf("只有空档时不该定出最优价,得到 %+v", s)
	}
}

// 成交均价贴着卖价 = 没人砸到买价上 = 挂买单收不到货。
func TestFillPositionTellsWhichSideTrades(t *testing.T) {
	// 实测 T6_METALBAR_LEVEL4@4:买 260,066 卖 357,999,7 日均价 355k
	p, ok := FillPosition(260066, 357999, 355000)
	if !ok || !roughly(p, 0.969, 0.002) {
		t.Fatalf("成交位置 = %.4f(ok=%v),想要 ~0.969", p, ok)
	}
	// 落在中间 = 两侧都在成交
	if p, _ := FillPosition(1000, 1200, 1100); !roughly(p, 0.5, 1e-6) {
		t.Fatalf("成交位置 = %.4f,想要 0.5", p)
	}
	// 交叉盘和缺数据都要说不知道,不能硬算
	if _, ok := FillPosition(1300, 1200, 1250); ok {
		t.Fatal("交叉盘时不该给出成交位置")
	}
	if _, ok := FillPosition(0, 1200, 1100); ok {
		t.Fatal("缺买价时不该给出成交位置")
	}
}

func roughly(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// 最优价只是第一件的价格。做大单必须往下吃档。
func TestVWAPIsWorseThanBestPrice(t *testing.T) {
	book := []Level{{1000, 10}, {1100, 50}, {1300, 200}}
	f := Walk(book, 100)

	if !f.Filled || f.Got != 100 {
		t.Fatalf("应该吃满 100 件,实际 %d", f.Got)
	}
	// 10×1000 + 50×1100 + 40×1300 = 117000,均价 1170
	if !near(f.VWAP, 1170) {
		t.Fatalf("实际均价 = %v,想要 1170", f.VWAP)
	}
	if f.Best != 1000 || f.Worst != 1300 {
		t.Fatalf("最优/最差 = %d/%d", f.Best, f.Worst)
	}
	if !near(f.Slippage, 0.17) {
		t.Fatalf("滑点 = %v,想要 0.17", f.Slippage)
	}
}

// 只看最优价会高估机会,量越大越离谱——这正是这个包存在的理由。
func TestSlippageGrowsWithSize(t *testing.T) {
	book := []Level{{1000, 10}, {1100, 50}, {1300, 200}, {2000, 1000}}
	small := Walk(book, 10)
	large := Walk(book, 500)

	if small.Slippage != 0 {
		t.Fatalf("只吃第一档不该有滑点,得到 %v", small.Slippage)
	}
	if !(large.Slippage > small.Slippage) {
		t.Fatalf("大单滑点 %v 应该高于小单 %v", large.Slippage, small.Slippage)
	}
}

// 盘口不够深必须说出来,而不是假装吃满了。
func TestUnfilledIsReported(t *testing.T) {
	f := Walk([]Level{{1000, 10}}, 100)
	if f.Filled {
		t.Fatal("盘口只有 10 件,不该报成吃满")
	}
	if f.Got != 10 {
		t.Fatalf("实际拿到 %d 件,想要 10", f.Got)
	}
	if !near(f.VWAP, 1000) {
		t.Fatalf("均价 = %v", f.VWAP)
	}
}

// 卖出方向价格递减,滑点同样是"比最优价差多少"。
func TestSellSideSlippageIsPositive(t *testing.T) {
	book := []Level{{1000, 10}, {900, 50}} // 买单,价格从高到低
	f := Walk(book, 60)
	if !near(f.VWAP, (1000*10+900*50)/60.0) {
		t.Fatalf("均价 = %v", f.VWAP)
	}
	if f.Slippage <= 0 {
		t.Fatalf("滑点 = %v,应该为正", f.Slippage)
	}
}

func TestEmptyBook(t *testing.T) {
	f := Walk(nil, 100)
	if f.Filled || f.Got != 0 || f.VWAP != 0 {
		t.Fatalf("空盘口应该什么都吃不到:%+v", f)
	}
}

// 挂单党更常问"我只接受到这个价,能吃到几件"。
func TestMaxQtyWithinLimit(t *testing.T) {
	book := []Level{{1000, 10}, {1100, 50}, {1300, 200}}
	qty, cost := MaxQtyWithin(book, 1100, true)
	if qty != 60 {
		t.Fatalf("1100 以内能吃 %d 件,想要 60", qty)
	}
	if !near(cost, 1000*10+1100*50) {
		t.Fatalf("花费 = %v", cost)
	}

	sell := []Level{{1000, 10}, {900, 50}, {700, 200}}
	q2, _ := MaxQtyWithin(sell, 900, false)
	if q2 != 60 {
		t.Fatalf("900 以上能卖 %d 件,想要 60", q2)
	}
}
