package depth

import (
	"math"
	"testing"
)

// 整数件数时和 Walk 完全一致。
func TestWalkFrac整数件数和Walk一致(t *testing.T) {
	levels := []Level{{100, 3}, {101, 0}, {105, 2}, {120, 10}}
	for want := int64(1); want <= 20; want++ {
		w := Walk(levels, want)
		f := WalkFrac(levels, float64(want))
		if f.Got != float64(w.Got) || math.Abs(f.Total-w.Total) > 1e-9 || f.Filled != w.Filled ||
			f.Worst != w.Worst || f.Best != w.Best || math.Abs(f.VWAP-w.VWAP) > 1e-9 {
			t.Fatalf("want=%d: WalkFrac %+v,Walk %+v", want, f, w)
		}
	}
}

// 小数件数:23.0415 个矿不进成 24 个,最后一档只吃 0.0415 件。
func TestWalkFrac小数件数(t *testing.T) {
	// T8.4 矿 @ Thetford 09-26 的前几档
	levels := []Level{{1_042_319, 2}, {1_042_320, 1}, {1_142_320, 1}, {1_142_321, 4}, {1_199_938, 4}}
	f := WalkFrac(levels, 3.5)
	if !f.Filled || f.Got != 3.5 {
		t.Fatalf("%+v", f)
	}
	want := 2*1_042_319.0 + 1_042_320 + 0.5*1_142_320
	if math.Abs(f.Total-want) > 1e-6 || f.Worst != 1_142_320 || len(f.Levels) != 3 || f.Levels[2].Qty != 0.5 {
		t.Fatalf("总价 %.2f 想要 %.2f,%+v", f.Total, want, f)
	}
	// 吃穿:Filled=false,Got 是盘口全部件数
	f = WalkFrac(levels, 12.25)
	if f.Filled || f.Got != 12 {
		t.Fatalf("吃穿时 %+v", f)
	}
	// 连乘出来的件数带浮点误差,也算吃满
	f = WalkFrac([]Level{{10, 3}}, 3+1e-12)
	if !f.Filled {
		t.Fatalf("浮点误差内应当算吃满: %+v", f)
	}
	if f := WalkFrac(levels, 0); f.Got != 0 || f.Filled || f.Levels != nil {
		t.Fatalf("0 件: %+v", f)
	}
	if f := WalkFrac(nil, 1); f.Got != 0 || f.Filled {
		t.Fatalf("空盘口: %+v", f)
	}
}
