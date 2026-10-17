package flip

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/model"
	"albion-guild/internal/refine"
	"albion-guild/internal/store"
)

// feeRefine 是带抓包站费的假数据源(实现 StationFeeSource)。
type feeRefine struct {
	*fakeRefine
	stations []store.StationFee
	calls    int
	since    time.Time
}

func (f *feeRefine) StationFees(_ context.Context, since time.Time) ([]store.StationFee, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.since = since
	var out []store.StationFee
	for _, s := range f.stations {
		if s.ObservedAt.After(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

func fptr(v float64) *float64 { return &v }
func iptr(v int64) *int64     { return &v }

// station 是一台站的最新观测。
func station(city, zone string, btype int32, x float64, fee float64, owner string, seen time.Time) store.StationFee {
	o := model.StationObs{ZoneID: zone, BuildingType: btype, Pos: []float64{x, 0}, PublicFee: fptr(fee),
		Owner: owner, ObservedAt: seen}
	return store.StationFee{StationObs: o, City: city, BuildingKey: o.BuildingKey(), Reporter: "乙"}
}

func newFeeService(stations ...store.StationFee) (*Service, *feeRefine) {
	src := &feeRefine{fakeRefine: barFixture(), stations: stations}
	s, _ := newRefineService(src.fakeRefine)
	s.RefineSrc = src
	return s, src
}

// 映射上了、新鲜:用这座城这条线最便宜的那台;太旧的、别的城的、映射外的、没营养的都不算;
// 越界的费率丢掉计数。打底参数里站费和(开关打开时)主人费率都换成它,收尾时补进 Confirmed。
func TestRefine抓到的站费映射命中且新鲜时用最便宜那台(t *testing.T) {
	empty := station("Thetford", "0000", 501, 9, 100, "空站", rNow.Add(-time.Hour))
	empty.Nutrition = iptr(0)
	s, src := newFeeService(
		station("Thetford", "0000", 501, 1, 420, "甲", rNow.Add(-3*time.Hour)),
		station("Thetford", "0000", 501, 2, 350, "乙", rNow.Add(-5*time.Hour)),
		station("Thetford", "0000", 501, 3, 200, "旧站", rNow.Add(-200*time.Hour)), // 超过 168h
		station("thetford", "0000", 502, 4, 380, "丙", rNow.Add(-time.Hour)),      // 城市不区分大小写
		station("Martlock", "3004", 501, 5, 10, "外城", rNow.Add(-time.Hour)),
		station("Thetford", "0000", 777, 6, 50, "不认识的类型", rNow.Add(-2*time.Hour)),
		station("Thetford", "0000", 501, 7, 5000, "标度错了", rNow.Add(-time.Hour)),
		empty,
	)
	s.Cfg.Refining.StationBuildingTypes = map[string][]int32{"ORE": {501, 502}, "HIDE": {601}}

	p, info := s.RefineDefaultsLive(context.Background(), tBar, "")
	if info.Source != FeeSourceCapture || info.Fee != 350 || p.Fees.StationPer100 != 350 || p.Fees.TransmuteStationPer100 != 350 {
		t.Fatalf("应当用抓到的最便宜那台 350: info=%+v fees=%+v", info, p.Fees)
	}
	if st := info.Station; st == nil || st.Owner != "乙" || st.BuildingType != 501 || st.AgeH != 5 {
		t.Fatalf("选中的站 %+v", info.Station)
	}
	if info.Stations != 3 || *info.Min != 350 || *info.Max != 420 || info.Bad != 1 || info.Unusable != 1 {
		t.Fatalf("台数 / 范围 / 丢掉的: %+v", info)
	}
	if len(info.Unmapped) != 1 || info.Unmapped[0].BuildingType != 777 || info.Unmapped[0].FeeMin != 50 {
		t.Fatalf("没映射的类型 %+v", info.Unmapped)
	}
	if want := rNow.Add(-168 * time.Hour); !src.since.Equal(want) {
		t.Fatalf("读站费的窗口 %v,想要 %v", src.since, want)
	}
	// 请求体里的 confirmed 会整个替换打底那份:收尾时补上
	p.Confirmed = []string{"focus.fce"}
	SettleStationFee(&p, &info, false, false)
	for _, k := range []string{"fees.station_per100", "fees.transmute_station_per100", "focus.fce"} {
		if !slices.Contains(p.Confirmed, k) {
			t.Fatalf("Confirmed 里少了 %s: %v", k, p.Confirmed)
		}
	}
	res := mustRefine(t, s, p)
	if res.ParamsUsed.Fees.StationPer100 != 350 {
		t.Fatalf("计算用的站费 %v", res.ParamsUsed.Fees.StationPer100)
	}
	for _, c := range res.Candidates {
		for _, w := range c.Warnings {
			if w.Code == refine.WarnUnverified && strings.Contains(w.Msg, "精炼站费") {
				t.Fatalf("抓包命中后不该再说站费没核实: %s", w.Msg)
			}
		}
	}
	// 60 秒内再要一次不读库
	s.RefineDefaultsLive(context.Background(), tBar, "")
	if src.calls != 1 {
		t.Fatalf("站费应当缓存,读了 %d 次", src.calls)
	}

	// 主人费率开关关掉:只换精炼站费
	s2, _ := newFeeService(station("Thetford", "0000", 501, 1, 420, "甲", rNow.Add(-time.Hour)))
	s2.Cfg.Refining.StationBuildingTypes = map[string][]int32{"ORE": {501}}
	s2.Cfg.Refining.StationFeeTransmute = false
	p2, info2 := s2.RefineDefaultsLive(context.Background(), tBar, "")
	if p2.Fees.StationPer100 != 420 || p2.Fees.TransmuteStationPer100 != refine.DefaultTransmuteStationFeePer100 || info2.Transmute {
		t.Fatalf("开关关掉时不动主人费率: %+v %+v", p2.Fees, info2)
	}
}

// 映射为空(默认):抓到的费率不进计算,按配置算;响应里把抓到的建筑类型列出来让人去填。
func TestRefine抓到的站费没映射时用配置并列出建筑类型(t *testing.T) {
	s, _ := newFeeService(
		station("Thetford", "0000", 501, 1, 420, "甲", rNow.Add(-3*time.Hour)),
		station("Thetford", "0000", 501, 2, 300, "乙", rNow.Add(-time.Hour)),
		station("Thetford", "0000", 610, 3, 80, "丙", rNow.Add(-2*time.Hour)),
	)
	p, info := s.RefineDefaultsLive(context.Background(), tBar, "")
	if info.Source != FeeSourceDefault || info.Fee != 500 || p.Fees.StationPer100 != 500 || info.Station != nil {
		t.Fatalf("映射为空时按配置默认 500: %+v", info)
	}
	if info.Seen != 3 || len(info.Unmapped) != 2 {
		t.Fatalf("抓到 3 台、2 种类型: %+v", info)
	}
	u := info.Unmapped[0]
	if u.BuildingType != 501 || u.Stations != 2 || u.FeeMin != 300 || u.FeeMax != 420 || u.AgeH != 1 {
		t.Fatalf("501 那一类 %+v", u)
	}
	SettleStationFee(&p, &info, false, false)
	if slices.Contains(p.Confirmed, "fees.station_per100") {
		t.Fatal("没用抓包,不该标核对过")
	}
	// 按城市覆盖过的叫 config
	s.Cfg.Refining.StationFeeByCity = map[string]float64{"Thetford": 800}
	if _, info := s.RefineDefaultsLive(context.Background(), tBar, ""); info.Source != FeeSourceConfig || info.Fee != 800 {
		t.Fatalf("按城市覆盖: %+v", info)
	}
	// 数据源没有站费这项能力(老的假数据源):一样按配置
	s3, _ := newRefineService(barFixture())
	if _, info := s3.RefineDefaultsLive(context.Background(), tBar, ""); info.Source != FeeSourceDefault || info.Unmapped == nil {
		t.Fatalf("没接站费: %+v", info)
	}
}

// 请求里写了站费:以请求为准,source=request;抓到的那台照样留着给界面对照。
func TestRefine请求带了站费时以请求为准(t *testing.T) {
	s, _ := newFeeService(station("Thetford", "0000", 501, 1, 420, "甲", rNow.Add(-time.Hour)))
	s.Cfg.Refining.StationBuildingTypes = map[string][]int32{"ORE": {501}}
	p, info := s.RefineDefaultsLive(context.Background(), tBar, "")
	p.Fees.StationPer100 = 250                                              // 请求体解码进来
	p.Fees.TransmuteStationPer100 = refine.DefaultTransmuteStationFeePer100 // 主人费率也写了
	p.Confirmed = []string{"fees.station_per100"}
	SettleStationFee(&p, &info, true, true)
	if info.Source != FeeSourceRequest || info.Fee != 250 || info.Transmute || info.Station == nil {
		t.Fatalf("请求为准: %+v", info)
	}
	if slices.Contains(p.Confirmed, "fees.transmute_station_per100") {
		t.Fatalf("请求写了主人费率,不该替它标核对过: %v", p.Confirmed)
	}
}
