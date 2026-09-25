package arb

import (
	"math"
	"testing"

	"albion-guild/internal/conf"
)

// 同城第五层(max_margin)补的缺口跨城一样有:两腿各自对本城均价都"合规",
// 组合起来却是好几倍的价差。审查实测 T4_RUNE Fort Sterling → Caerleon 挂买挂卖
// 6 → 15、毛利 128%,两头都是 AODP、没有深度,排进了组合页第一。
//
// 但城市之间可以有结构性价差:销地 7 日均价本来就是产地的两倍多,
// 按历史价搬过去就是 100% 以上的毛利。判之前先除掉两城均价之比
func TestRoute_毛利率上限扣掉两城均价之差再判(t *testing.T) {
	mk := func(toAvg float64) (Market, Market) {
		from := market("Fort Sterling", 10, 5, 200_000) // 买一 0.5×、卖一 1.0× 均价
		from.Stats.AvgPrice7d = 10
		to := market("Caerleon", 24, 0, 200_000) // 只有卖单
		to.Stats.AvgPrice7d = toAvg
		return from, to
	}

	// 两城均价一样(10):销地卖一 24 是 2.4×、产地买一 5 是 0.5×,各自都在 [0.4, 2.5] 里,
	// 同侧价格比 24/10 = 2.4 也不到 3——以前一层都拦不住
	from, to := mk(10)
	off := opts()
	off.MaxMargin = 0
	loose, ok := routeOf(Find("T4_RUNE", "符文", 1, []Market{from, to}, conf.Default().Economics, off, now), "Fort Sterling", "Caerleon")
	if !ok || loose.Mode != "maker-maker" || loose.Margin < 2 {
		t.Fatalf("前提:关掉这一层时应出毛利 >200%% 的挂买挂卖,得到 %s / %.0f%%", loose.Mode, loose.Margin*100)
	}
	if _, ok := routeOf(find2(from, to), "Fort Sterling", "Caerleon"); ok {
		t.Fatal("两城均价一样、毛利 250%(秒买挂卖也有 115%),两个模式都该砍掉,整条路线不出")
	}

	// 销地均价 22:卖一 24 只是 1.09×,这是结构性价差,扣掉均价之比后挂买挂卖只剩约 59%
	from, to = mk(22)
	r, ok := routeOf(find2(from, to), "Fort Sterling", "Caerleon")
	if !ok || r.Mode != "maker-maker" || r.Margin < 2 {
		t.Fatalf("结构性价差不该被当成高得不真实,得到 %v / %+v", ok, r)
	}
	if got := excessMargin(r.Margin, from.Stats, to.Stats); got > 1 || got < 0 {
		t.Fatalf("扣掉均价之比后应在 0~100%% 之间,得到 %.0f%%", got*100)
	}
}

// 按执行方式逐个判:只砍超了的那个模式,别的模式用的是另外两边,照样能做
func TestRoute_毛利率上限只砍超了的模式(t *testing.T) {
	from := market("Fort Sterling", 10, 5, 200_000)
	from.Stats.AvgPrice7d = 10
	to := market("Caerleon", 18, 0, 200_000) // 卖一 1.8× 均价
	to.Stats.AvgPrice7d = 10
	r, ok := routeOf(find2(from, to), "Fort Sterling", "Caerleon")
	if !ok || r.Mode != "taker-maker" || r.Margin > 1 {
		t.Fatalf("挂买挂卖(6 → 17,毛利约 158%%)该砍,秒买挂卖(10 → 17,约 59%%)留着,得到 %v / %+v", ok, r)
	}
	for _, m := range r.Modes {
		if m.Mode == "maker-maker" {
			t.Fatalf("超了上限的挂买挂卖不该出现在 modes 里: %+v", m)
		}
	}
}

// 两城是同一个均价时,excessMargin 就是毛利率本身——和同城第五层逐字同一个判据;
// 任一端没有 7 日均价时退回原始毛利率
func TestExcessMargin_口径(t *testing.T) {
	s := func(avg float64) *Market {
		m := market("X", 10, 9, 1)
		m.Stats.AvgPrice7d = avg
		return &m
	}
	if got := excessMargin(1.28, s(7).Stats, s(7).Stats); math.Abs(got-1.28) > 1e-12 {
		t.Fatalf("同一个均价应原样返回,得到 %v", got)
	}
	if got := excessMargin(1.5, s(10).Stats, s(25).Stats); math.Abs(got-0) > 1e-12 {
		t.Fatalf("(1+1.5)×10/25−1 = 0,得到 %v", got)
	}
	if got := excessMargin(1.5, s(0).Stats, s(25).Stats); got != 1.5 {
		t.Fatalf("没有均价时退回原始毛利率,得到 %v", got)
	}
	if got := excessMargin(1.5, nil, s(25).Stats); got != 1.5 {
		t.Fatalf("没有统计时退回原始毛利率,得到 %v", got)
	}
}

func find2(markets ...Market) []Route {
	return Find("T4_RUNE", "符文", 1, markets, conf.Default().Economics, opts(), now)
}
