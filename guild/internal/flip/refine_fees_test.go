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
	// 一次 197 响应是整区的:观测时刻不同的站放在不同的区(同一个区里更早的会当成已经没了)
	empty := station("Thetford", "0000", 501, 9, 100, "空站", rNow.Add(-time.Hour))
	empty.Nutrition = iptr(0)
	s, src := newFeeService(
		station("Thetford", "0001", 501, 1, 420, "甲", rNow.Add(-3*time.Hour)),
		station("Thetford", "0002", 501, 2, 350, "乙", rNow.Add(-5*time.Hour)),
		station("Thetford", "0003", 501, 3, 200, "旧站", rNow.Add(-200*time.Hour)), // 超过 168h
		station("thetford", "0000", 502, 4, 380, "丙", rNow.Add(-time.Hour)),      // 城市不区分大小写
		station("Martlock", "3004", 501, 5, 10, "外城", rNow.Add(-time.Hour)),
		station("Thetford", "0004", 777, 6, 50, "不认识的类型", rNow.Add(-2*time.Hour)),
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

// 不配置也认得出精炼站:内置表(gamedata)里 431 是 T8 冶炼匠、438 是 T8 伐木场。
// Thetford 精炼金属锭只看冶炼匠;伐木场是别的线的站,不算没映射;铁匠铺 410 才是
func TestRefine内置表认得出精炼站(t *testing.T) {
	s, _ := newFeeService(
		station("Thetford", "0006", 431, 1, 420, "甲", rNow.Add(-3*time.Hour)),
		station("Thetford", "0007", 431, 2, 390, "乙", rNow.Add(-time.Hour)),
		station("Thetford", "0007", 438, 3, 80, "伐木场", rNow.Add(-time.Hour)),
		station("Thetford", "0007", 410, 4, 50, "铁匠铺", rNow.Add(-time.Hour)),
	)
	p, info := s.RefineDefaultsLive(context.Background(), tBar, "")
	if info.Source != FeeSourceCapture || info.Fee != 390 || p.Fees.StationPer100 != 390 || info.Stations != 2 {
		t.Fatalf("应当用内置表认出的冶炼匠里最便宜的 390: %+v", info)
	}
	if info.Unmapped == nil || len(info.Unmapped) != 1 || info.Unmapped[0].BuildingType != 410 {
		t.Fatalf("只有铁匠铺算没映射: %+v", info.Unmapped)
	}
}

// 对你不开放的站不参与取最便宜:以前 Lymhurst 那台 Closed 的(营养满格、没人用过)也会被挑中
func TestRefine不开放的站不算(t *testing.T) {
	no, closed, private, public := false, int32(0), int32(1), int32(2)
	cheapClosed := station("Thetford", "0007", 431, 1, 100, "关门", rNow.Add(-time.Hour))
	cheapClosed.CanUse, cheapClosed.Usable, cheapClosed.Access = &no, &no, &closed
	oldPrivate := station("Thetford", "0007", 431, 2, 150, "老观测私有", rNow.Add(-time.Hour))
	oldPrivate.Access = &private // 老观测只有访问权限、没有 canUse
	open := station("Thetford", "0007", 431, 3, 420, "开着", rNow.Add(-time.Hour))
	open.Access = &public
	s, _ := newFeeService(cheapClosed, oldPrivate, open,
		station("Thetford", "0007", 431, 4, 400, "什么都没有的老观测", rNow.Add(-time.Hour)))
	_, info := s.RefineDefaultsLive(context.Background(), tBar, "")
	if info.Fee != 400 || info.Closed != 2 || info.Stations != 2 {
		t.Fatalf("应当排除 2 台不开放的、取 400: %+v", info)
	}
}

// 站只能炼到本级为止:T6 纺织匠(457)炼不了 T8 布。目标 T8 时只认 T8 的站;每级最便宜的另记一份给矩阵
func TestRefine站的级别不够不算(t *testing.T) {
	s, _ := newFeeService(
		station("Thetford", "0007", 457, 1, 380, "T6站", rNow.Add(-time.Hour)),
		station("Thetford", "0007", 459, 2, 740, "T8站", rNow.Add(-time.Hour)),
	)
	_, info := s.RefineDefaultsLive(context.Background(), "T8_CLOTH_LEVEL4@4", "Thetford")
	if info.Fee != 740 || info.LowTier != 1 || info.Stations != 1 || info.Tier != 8 {
		t.Fatalf("T8 布只能用 T8 站 740: %+v", info)
	}
	if info.ByTier[6] != 380 || info.ByTier[7] != 740 || info.ByTier[8] != 740 || info.ByTier[4] != 380 {
		t.Fatalf("每级最便宜: %v", info.ByTier)
	}
	_, info6 := s.RefineDefaultsLive(context.Background(), "T6_CLOTH", "Thetford")
	if info6.Fee != 380 || info6.LowTier != 0 {
		t.Fatalf("T6 布用 T6 站 380: %+v", info6)
	}
}

// 一次 197 响应是整区的全部建筑:同一个区后来又扫过、那次里没有的站就是拆了或搬了。
// 以前 Lymhurst 那台 330 的站 10-17 之后两次整城扫描都不在,还被当成最便宜的
func TestRefine后来整区扫描里没有的站不算(t *testing.T) {
	s, _ := newFeeService(
		station("Thetford", "0007", 431, 1, 230, "拆了", rNow.Add(-40*time.Hour)),
		station("Thetford", "0007", 431, 2, 400, "还在", rNow.Add(-time.Hour)),
		station("Thetford", "0007", 410, 3, 50, "铁匠铺也在最新那次", rNow.Add(-time.Hour-time.Minute)),
		// 别的区的老观测不受这个区的新扫描影响
		station("Thetford", "0008", 431, 4, 300, "别的区", rNow.Add(-40*time.Hour)),
	)
	_, info := s.RefineDefaultsLive(context.Background(), tBar, "")
	if info.Fee != 300 || info.Gone != 1 || info.Stations != 2 {
		t.Fatalf("应当剔掉 0007 区里已经没了的 230,取别的区的 300: %+v", info)
	}
}

// 最新那次扫描只存下一部分(客户端断网积压时从最旧的丢、上传分批)时不判「没了」:
// 下标 0 那几台被截掉了,还在的旧观测不能当成拆了(审查 finding:否则会换成更贵的站)
func TestRefine扫描不完整时不判没了(t *testing.T) {
	at := func(st store.StationFee, idx int) store.StationFee { st.Index = idx; return st }
	old := at(station("Thetford", "0007", 431, 1, 200, "截掉的那台", rNow.Add(-20*time.Hour)), 5)
	s, _ := newFeeService(old,
		at(station("Thetford", "0007", 431, 2, 600, "最新那次 27", rNow.Add(-time.Hour)), 27),
		at(station("Thetford", "0007", 410, 3, 50, "最新那次 28", rNow.Add(-time.Hour)), 28),
	)
	_, info := s.RefineDefaultsLive(context.Background(), tBar, "")
	if info.Fee != 200 || info.Gone != 0 {
		t.Fatalf("最新那次不完整(下标不从 0 开始),不该剔掉 200 那台: %+v", info)
	}
	// 同一次扫描完整(下标 0..2 连续):照常剔
	s2, _ := newFeeService(old,
		at(station("Thetford", "0007", 431, 2, 600, "a", rNow.Add(-time.Hour)), 0),
		at(station("Thetford", "0007", 410, 3, 50, "b", rNow.Add(-time.Hour)), 1),
		at(station("Thetford", "0007", 410, 4, 50, "c", rNow.Add(-time.Hour)), 2),
	)
	if _, info := s2.RefineDefaultsLive(context.Background(), tBar, ""); info.Fee != 600 || info.Gone != 1 {
		t.Fatalf("完整扫描里没有它,应当剔掉: %+v", info)
	}
}

func TestRefine内置表_只有铁匠铺算没映射(t *testing.T) {
	s, _ := newFeeService(
		station("Thetford", "0007", 431, 2, 390, "乙", rNow.Add(-time.Hour)),
		station("Thetford", "0007", 438, 3, 80, "伐木场", rNow.Add(-time.Hour)),
		station("Thetford", "0007", 410, 4, 50, "铁匠铺", rNow.Add(-time.Hour)),
	)
	_, info := s.RefineDefaultsLive(context.Background(), tBar, "")
	if len(info.Unmapped) != 1 || info.Unmapped[0].BuildingType != 410 {
		t.Fatalf("只有铁匠铺算没映射: %+v", info.Unmapped)
	}
}

// 不是精炼站的类型(或者游戏更新后内置表旧了):抓到的费率不进计算,按配置算;响应里列出来。
func TestRefine抓到的站费没映射时用配置并列出建筑类型(t *testing.T) {
	s, _ := newFeeService(
		station("Thetford", "0001", 501, 1, 420, "甲", rNow.Add(-3*time.Hour)),
		station("Thetford", "0002", 501, 2, 300, "乙", rNow.Add(-time.Hour)),
		station("Thetford", "0003", 610, 3, 80, "丙", rNow.Add(-2*time.Hour)),
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

// 只写了站费、没写主人费率:主人费率还是抓到的那台的,要标核对过、Transmute 留着,
// 界面据此写「抓包」。以前不标,抓到的 420 被标成「服务端默认值,没核实」
func TestRefine只写了站费时主人费率仍按抓包标(t *testing.T) {
	s, _ := newFeeService(station("Thetford", "0000", 501, 1, 420, "甲", rNow.Add(-time.Hour)))
	s.Cfg.Refining.StationBuildingTypes = map[string][]int32{"ORE": {501}}
	p, info := s.RefineDefaultsLive(context.Background(), tBar, "")
	p.Fees.StationPer100 = 250
	SettleStationFee(&p, &info, true, false)
	if info.Source != FeeSourceRequest || !info.Transmute || p.Fees.TransmuteStationPer100 != 420 {
		t.Fatalf("主人费率仍是抓包的 420: %+v fees=%+v", info, p.Fees)
	}
	if !slices.Contains(p.Confirmed, "fees.transmute_station_per100") || slices.Contains(p.Confirmed, "fees.station_per100") {
		t.Fatalf("只替抓包的主人费率标核对过,站费是你填的(由请求自己的 confirmed 管): %v", p.Confirmed)
	}
}
