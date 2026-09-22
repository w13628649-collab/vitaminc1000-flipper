package depth

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

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
