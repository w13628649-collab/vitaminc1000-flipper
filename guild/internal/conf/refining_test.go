package conf

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// 精炼段的默认值照设计 §4.6。带「未核实」的几项改了等于改结论,要有测试拦着
func TestRefining默认值(t *testing.T) {
	r := Default().Refining
	want := Refining{
		WindowHours: 72, VolumeDays: 14,
		DailyBonus: map[string]float64{}, StationFeePer100: 500, StationFeeByCity: map[string]float64{},
		TransmuteFactor: 1.154, TransmuteStationFeePer100: 500,
		HeartReturns: true, Leftover: "carry", IncludeTail: true,
		FocusPerDay: 10000, FocusCap: 30000, VolumeShare: 0.3,
		// 建筑类型 → 精炼线要等实测样本才知道:默认空,空着时抓到的费率不进计算
		StationBuildingTypes: map[string][]int32{}, StationFeeMaxAgeHours: 168, StationFeeTransmute: true,
	}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("精炼默认值不对\n得到 %+v\n应为 %+v", r, want)
	}
	if r.Window() != 72*time.Hour {
		t.Fatalf("窗口应为 72h,得到 %v", r.Window())
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	// 每次调 Default 都是新的 map:一个 Config 改了覆盖表,不能串到别的 Config 上
	a, b := Default(), Default()
	a.Refining.StationFeeByCity["Thetford"] = 1
	if len(b.Refining.StationFeeByCity) != 0 {
		t.Fatal("Default 的精炼段共用了同一个 map")
	}
}

func TestRefining只写几项其余保持默认(t *testing.T) {
	c, err := Load(writeYAML(t, `
refining:
  window_hours: 96
  daily_bonus: {ORE: 10}
  station_fee_by_city: {Thetford: 800}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Refining
	if r.WindowHours != 96 || r.DailyBonus["ORE"] != 10 || r.StationFeeFor("Thetford") != 800 {
		t.Fatalf("写了的没生效: %+v", r)
	}
	// 没覆盖的城市用全局站费;没写的键保持默认
	if r.StationFeeFor("Martlock") != 500 || r.TransmuteFactor != 1.154 || !r.HeartReturns || r.FocusCap != 30000 {
		t.Fatalf("没写的应保持默认: %+v", r)
	}
}

func TestRefining校验点名写错的键(t *testing.T) {
	for _, tc := range []struct {
		key string
		mut func(*Refining)
	}{
		{"window_hours", func(r *Refining) { r.WindowHours = 0 }},
		{"window_hours", func(r *Refining) { r.WindowHours = 24*30 + 1 }},
		{"volume_days", func(r *Refining) { r.VolumeDays = 0 }},
		{"daily_bonus", func(r *Refining) { r.DailyBonus = map[string]float64{"ore": 10} }},
		{"daily_bonus", func(r *Refining) { r.DailyBonus = map[string]float64{"ROCK": 10} }},
		{"daily_bonus.HIDE", func(r *Refining) { r.DailyBonus = map[string]float64{"HIDE": 120} }},
		{"station_fee_per100", func(r *Refining) { r.StationFeePer100 = 1001 }},
		{"station_fee_by_city.Martlock", func(r *Refining) { r.StationFeeByCity = map[string]float64{"Martlock": -1} }},
		{"transmute_station_fee_per100", func(r *Refining) { r.TransmuteStationFeePer100 = -5 }},
		{"transmute_factor", func(r *Refining) { r.TransmuteFactor = 115.4 }},
		{"transmute_gold_price", func(r *Refining) { r.TransmuteGoldPrice = -1 }},
		{"leftover", func(r *Refining) { r.Leftover = "sell" }},
		{"focus_per_day", func(r *Refining) { r.FocusPerDay = 40000 }},
		{"volume_share", func(r *Refining) { r.VolumeShare = 0 }},
		{"transport_pct", func(r *Refining) { r.TransportPct = -0.1 }},
		{"station_building_types", func(r *Refining) { r.StationBuildingTypes = map[string][]int32{"ore": {501}} }},
		{"station_building_types", func(r *Refining) { r.StationBuildingTypes = map[string][]int32{"ROCK": {501}} }},
		{"station_building_types.ORE", func(r *Refining) { r.StationBuildingTypes = map[string][]int32{"ORE": {}} }},
		{"station_building_types.HIDE", func(r *Refining) { r.StationBuildingTypes = map[string][]int32{"HIDE": {0}} }},
		{"station_fee_max_age_hours", func(r *Refining) { r.StationFeeMaxAgeHours = 0 }},
		{"station_fee_max_age_hours", func(r *Refining) { r.StationFeeMaxAgeHours = 24*30 + 1 }},
	} {
		c := Default()
		tc.mut(&c.Refining)
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "refining."+tc.key) {
			t.Errorf("%s 写错应报错并点名,得到 %v", tc.key, err)
		}
	}
}

// 建筑类型映射按 YAML 写进来;线 → 类型、类型 → 线两个方向都查得到。
func TestRefining工作站建筑类型映射(t *testing.T) {
	c, err := Load(writeYAML(t, `
refining:
  station_building_types: {ORE: [501, 502], HIDE: [601]}
  station_fee_max_age_hours: 24
  station_fee_transmute: false
`))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Refining
	if got := r.StationTypesFor("ORE"); !reflect.DeepEqual(got, []int32{501, 502}) {
		t.Fatalf("ORE 映射 %v", got)
	}
	if r.StationTypesFor("WOOD") != nil {
		t.Fatal("没映射的线应当是 nil")
	}
	if l, ok := r.StationLineOf(601); !ok || l != "HIDE" {
		t.Fatalf("601 → %q %v", l, ok)
	}
	if _, ok := r.StationLineOf(999); ok {
		t.Fatal("没映射的类型不该查到")
	}
	if r.StationFeeMaxAgeHours != 24 || r.StationFeeTransmute {
		t.Fatalf("写了的没生效: %+v", r)
	}
}

// 仓库自带的 config.yaml 里写了精炼段,要和内置默认值一致(界面上的「未核实」徽章按默认值判)
func TestRefining仓库自带配置和默认一致(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.Refining, Default().Refining) {
		t.Fatalf("自带配置的精炼段和默认值不一致\n得到 %+v\n应为 %+v", c.Refining, Default().Refining)
	}
}
