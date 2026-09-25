package arb

import (
	"math"
	"slices"
	"strings"
	"testing"

	"albion-guild/internal/conf"
	"albion-guild/internal/screen"
)

// 一端是 Brecilien 的路线要穿迷雾:路上时间用单独的配置,带 mists 标记,
// 置信度最高 medium。同一组价格放在两个皇家城市之间是 high、没有标记
func TestRoute_Brecilien经迷雾带风险标记且置信度最高medium(t *testing.T) {
	o := opts()
	if o.TravelHours != 0.5 || o.BrecilienTravelHours != 1.0 {
		t.Fatalf("前提变了:默认单程应为皇家 0.5h、Brecilien 1.0h,得到 %v / %v", o.TravelHours, o.BrecilienTravelHours)
	}

	royal, ok := routeOf(find(market("Lymhurst", 1000, 950, 5000), market("Martlock", 1600, 1500, 5000)), "Lymhurst", "Martlock")
	if !ok {
		t.Fatal("对照:皇家城市之间应有路线")
	}
	if royal.Confidence != screen.High || len(royal.RiskTags) != 0 || royal.TravelHours != 0.5 {
		t.Fatalf("对照前提:皇家城市之间 high、无风险标记、单程 0.5h,得到 %s / %v / %v",
			royal.Confidence, royal.RiskTags, royal.TravelHours)
	}

	for _, c := range []struct{ from, to string }{
		{conf.Brecilien, "Martlock"}, // 从迷雾里运出来
		{"Lymhurst", conf.Brecilien}, // 运进迷雾去卖
	} {
		t.Run(c.from+"→"+c.to, func(t *testing.T) {
			r, ok := routeOf(find(market(c.from, 1000, 950, 5000), market(c.to, 1600, 1500, 5000)), c.from, c.to)
			if !ok {
				t.Fatal("价差一样,路线应该照出")
			}
			if !slices.Equal(r.RiskTags, []string{RiskMists}) {
				t.Fatalf("应带 risk_tags=[mists],得到 %v", r.RiskTags)
			}
			if r.Confidence != screen.Medium {
				t.Fatalf("同样的价格在皇家城市之间是 high,经迷雾应压到 medium,得到 %s", r.Confidence)
			}
			if !strings.Contains(strings.Join(r.Warnings, ";"), "经迷雾,有被劫风险") {
				t.Fatalf("应提示经迷雾有被劫风险,得到 %v", r.Warnings)
			}
			if r.TravelHours != 1.0 {
				t.Fatalf("单程应按 Brecilien 专用的 1.0h,得到 %v", r.TravelHours)
			}
			for _, m := range r.Modes {
				want := 2*1.0 + float64(makerLegs(m.Mode))*o.FillHours
				if math.Abs(m.HoursPerTrip-want) > 1e-9 {
					t.Fatalf("%s 一趟应为 %.1fh(两趟 1.0h 路程 + 挂单等待),得到 %v", m.Label, want, m.HoursPerTrip)
				}
			}
		})
	}
}

// 已经是 low 的不会被"最高 medium"抬上去
func TestRoute_Brecilien不会把low抬成medium(t *testing.T) {
	to := market(conf.Brecilien, 1600, 1500, 5000)
	to.AgeHours = 5 // 超过 4h → low
	r, ok := routeOf(find(market("Lymhurst", 1000, 950, 5000), to), "Lymhurst", conf.Brecilien)
	if !ok {
		t.Fatal("5h 仍在 6h 新鲜度内,路线应该在")
	}
	if r.Confidence != screen.Low {
		t.Fatalf("数据 5h 的路线应是 low,得到 %s", r.Confidence)
	}
}

// 本金受限时路上时间直接决定周转:经迷雾走得久,同样的价差日收益更低
func TestRoute_Brecilien路上更久本金受限时日收益更低(t *testing.T) {
	o := opts()
	o.Capital = 200_000 // 一趟只买得起两百来件,市场一天吃得下上万件
	o.MinDailyVolumeSilver = 0
	get := func(from, to string) Route {
		r, ok := routeOf(Find("T5_CLOTH", "精布", 1,
			[]Market{market(from, 1000, 950, 500_000), market(to, 1600, 1500, 500_000)},
			conf.Default().Economics, o, now), from, to)
		if !ok {
			t.Fatalf("%s → %s 应有路线", from, to)
		}
		return r
	}
	royal, mists := get("Lymhurst", "Martlock"), get("Lymhurst", conf.Brecilien)
	if royal.Mode != mists.Mode {
		t.Fatalf("前提:两条路线应选同一种执行方式,得到 %s / %s", royal.Mode, mists.Mode)
	}
	if !(mists.DailyProfit < royal.DailyProfit) || !(mists.TripsPerDay < royal.TripsPerDay) {
		t.Fatalf("经迷雾日收益和周转都应更低,得到 %.0f(%.2f 趟) vs %.0f(%.2f 趟)",
			mists.DailyProfit, mists.TripsPerDay, royal.DailyProfit, royal.TripsPerDay)
	}
}

// brecilien_travel_hours=0 时退回 travel_hours;风险标记和置信度上限照旧
func TestRoute_Brecilien专用时间为0时跟travel_hours(t *testing.T) {
	o := opts()
	o.BrecilienTravelHours = 0
	r, ok := routeOf(Find("T5_CLOTH", "精布", 1,
		[]Market{market(conf.Brecilien, 1000, 950, 5000), market("Martlock", 1600, 1500, 5000)},
		conf.Default().Economics, o, now), conf.Brecilien, "Martlock")
	if !ok {
		t.Fatal("应有路线")
	}
	if r.TravelHours != o.TravelHours || r.Confidence != screen.Medium || len(r.RiskTags) != 1 {
		t.Fatalf("单程应退回 %.1fh、标记和上限不变,得到 %v / %s / %v", o.TravelHours, r.TravelHours, r.Confidence, r.RiskTags)
	}
}

func makerLegs(key string) int {
	n := 0
	for _, leg := range strings.SplitN(key, "-", 2) {
		if leg == "maker" {
			n++
		}
	}
	return n
}
