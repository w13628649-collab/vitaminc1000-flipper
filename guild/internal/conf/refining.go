package conf

import (
	"fmt"
	"sort"
	"time"
)

// Refining 是精炼页(/api/refine/*)的服务端默认值(设计 §4.6)。
//
// 服务端是全公会共用的,**这里不放个人参数**:专精 FCE、今日可用专注、自己看到的站费、
// 自己实测的转化系数 k,都由客户端存本地、每次请求带上。这里只是请求没带时用的默认值,
// 以及读数据的窗口这类服务端才能决定的东西。
//
// 已核实的游戏常量(基础加成 18、专精 +40、专注 +59、专精城 ↔ 精炼线)不做成配置:
// 它们在 refine/rules.go 和 refine/lines.go 里带着规则表编号和出处(C2/C3/C4 ✔),
// 配置里改了只会让结果和数据文件对不上。
//
// 带「未核实」的几项默认值照设计(design-questions.md),和 refine 里的 Default* 常量
// 是同一组数(refine 的测试断言两边一致;conf 不能 import refine,那边 import 了这里)。
type Refining struct {
	// WindowHours 是精炼页读抓包的窗口,也是请求里 sources.max_age_hours 的默认值和上限。
	// 默认 72:用户是抓一次包、之后再算,查价页那 6 小时一过就读不到了。
	// 超过 6 小时的价格照用,结果里按年龄标黄(6h)、标红(24h)
	WindowHours float64 `yaml:"window_hours"`
	// VolumeDays 是日成交量取多少个自然日(按「总量 ÷ 自然日」平均,设计 §2.5)
	VolumeDays int `yaml:"volume_days"`
	// DailyBonus 是今天的每日生产加成,精炼线 → 0 / 10 / 20(规则表 C6 ◐)。
	// 游戏里全服一样,所以放服务端;没有接口能取,只能看「活动」窗口手填。键是 ORE / HIDE / FIBER / WOOD
	DailyBonus map[string]float64 `yaml:"daily_bonus"`
	// StationFeePer100 是精炼站使用费,银 / 100 营养(E5 ?,未核实;默认 500 是表格作者用的数)
	StationFeePer100 float64 `yaml:"station_fee_per100"`
	// StationFeeByCity 按生产城市覆盖 StationFeePer100(各城站点归玩家所有、费率不同,E5)
	StationFeeByCity map[string]float64 `yaml:"station_fee_by_city"`
	// TransmuteFactor 是转化实付系数 k(B5 ?,未核实;亚服表格反推 1.154)
	TransmuteFactor float64 `yaml:"transmute_factor"`
	// TransmuteMeasuredAt / TransmuteGoldPrice 记下 k 是哪天、什么金价下量的,只原样回给界面。
	// k 随时间变过(2023-10 约 1.00,2025-11 西服约 1.156),全局折扣也和金价挂钩(B7)
	TransmuteMeasuredAt string  `yaml:"transmute_measured_at"`
	TransmuteGoldPrice  float64 `yaml:"transmute_gold_price"`
	// TransmuteStationFeePer100 是转化的主人费率(B6 ◐)
	TransmuteStationFeePer100 float64 `yaml:"transmute_station_fee_per100"`
	// HeartReturns:精炼用的心是否也按回收率返还(A7 ?)
	HeartReturns bool `yaml:"heart_returns"`
	// Leftover 是批末余料的估值方式:carry | bid | zero(设计 §2.4)
	Leftover string `yaml:"leftover"`
	// IncludeTail:把批末余料算进开工本金(设计 §2.4)
	IncludeTail bool `yaml:"include_tail"`
	// FocusPerDay 是今日可用专注的默认值:premium 每天恢复 10,000(D4 ✔)
	FocusPerDay float64 `yaml:"focus_per_day"`
	// FocusCap 是专注上限 30,000,满了就不再恢复(D4 ✔)。请求里的今日专注不能超过它:
	// 专注按角色结算,一次精炼只用得上一个角色的专注
	FocusCap float64 `yaml:"focus_cap"`
	// VolumeShare 是能吃到日成交量的几成(用户问题 6,没核实)
	VolumeShare float64 `yaml:"volume_share"`
	// TransportPct 是外城买料的运费比例
	TransportPct float64 `yaml:"transport_pct"`
	// BidOrders:把「挂买单收料」列进候选。默认只当参照行,不进推荐(收不收得到不知道)
	BidOrders bool `yaml:"bid_orders"`

	// StationBuildingTypes:精炼线(ORE/HIDE/FIBER/WOOD)→ 额外认作精炼站的建筑类型 id(op 197 的 key17)。
	// 平时不用填:内置表(internal/gamedata)已经从客户端 buildings.xml 算好了每条线 T2~T8 的站,
	// 2026-10-19 拿 N 地图核过。游戏更新加了新站、内置表还没重新生成时,在这里补上就能先用。
	// 认得出的站有新鲜观测就用最便宜那台的费率,优先于 station_fee_by_city
	StationBuildingTypes map[string][]int32 `yaml:"station_building_types"`
	// StationFeeMaxAgeHours:抓包费率多旧以内才用。房主设的费率不常变,默认 168(一周)
	StationFeeMaxAgeHours float64 `yaml:"station_fee_max_age_hours"`
	// StationFeeTransmute:抓到的精炼站费率也当转化主人费率用。转化在精炼站上做是静态数据定的
	// (buildings.xml 里冶炼匠的可制造清单有 T?_ORE_LEVELn,分在 TRANSMUTATION 类),
	// 一台站一个使用费;做成开关是留个退路,默认 true
	StationFeeTransmute bool `yaml:"station_fee_transmute"`
}

// RefiningLines 是 daily_bonus 认的精炼线,和 refine.Lines 的 Key 一致(refine 的测试断言)。
// 石头线本期不做(规则表 A8),写了也不生效,所以不认
var RefiningLines = []string{"ORE", "HIDE", "FIBER", "WOOD"}

// 几个范围和 refine 的校验同一口径(refine.CheckStationFee / CheckTransmuteFactor),
// 这里重复一份是为了配置写错时在启动那一刻就报出来,而不是等第一次请求
const (
	refiningMaxStationFee = 1000.0 // E3 ✔:gamedata.xml maxuseagefee=1000
	refiningMinK          = 0.5
	refiningMaxK          = 3.0
	refiningMaxWindowDays = 30 // 挂单最长挂 30 天(F6),读得再久也没有意义
	refiningMaxVolumeDays = 90
	// refiningMaxStationAgeHours:抓包费率最多认 30 天前的(和读簿窗口上限同一个数)
	refiningMaxStationAgeHours = 24 * 30.0
)

// DefaultRefining 是精炼页的默认值,见各字段注释。
func DefaultRefining() Refining {
	return Refining{
		WindowHours:               72,
		VolumeDays:                14,
		DailyBonus:                map[string]float64{},
		StationFeePer100:          500,
		StationFeeByCity:          map[string]float64{},
		TransmuteFactor:           1.154,
		TransmuteStationFeePer100: 500,
		HeartReturns:              true,
		Leftover:                  "carry",
		IncludeTail:               true,
		FocusPerDay:               10000,
		FocusCap:                  30000,
		VolumeShare:               0.3,
		TransportPct:              0,
		BidOrders:                 false,
		StationBuildingTypes:      map[string][]int32{},
		StationFeeMaxAgeHours:     168,
		StationFeeTransmute:       true,
	}
}

// Window 是精炼页读抓包的窗口。
func (r Refining) Window() time.Duration { return hours(r.WindowHours) }

// StationFeeFor 是 city 的精炼站费率:按城市覆盖过的用覆盖值。
func (r Refining) StationFeeFor(city string) float64 {
	if v, ok := r.StationFeeByCity[city]; ok {
		return v
	}
	return r.StationFeePer100
}

// Validate 检查精炼段。错误文本写清是哪个键、合法范围是什么。
func (r Refining) Validate() error {
	if !(r.WindowHours > 0 && r.WindowHours <= 24*refiningMaxWindowDays) {
		return fmt.Errorf("refining.window_hours 要在 (0, %d] 内,得到 %g", 24*refiningMaxWindowDays, r.WindowHours)
	}
	if r.VolumeDays < 1 || r.VolumeDays > refiningMaxVolumeDays {
		return fmt.Errorf("refining.volume_days 要在 [1, %d] 内,得到 %d", refiningMaxVolumeDays, r.VolumeDays)
	}
	known := map[string]bool{}
	for _, l := range RefiningLines {
		known[l] = true
	}
	lines := make([]string, 0, len(r.DailyBonus))
	for l := range r.DailyBonus {
		lines = append(lines, l)
	}
	sort.Strings(lines)
	for _, l := range lines {
		v := r.DailyBonus[l]
		if !known[l] {
			return fmt.Errorf("refining.daily_bonus 的键 %q 不认识,只有 %v", l, RefiningLines)
		}
		if !(v >= 0 && v <= 100) {
			return fmt.Errorf("refining.daily_bonus.%s 要在 [0, 100] 内,得到 %g", l, v)
		}
	}
	fee := func(key string, v float64) error {
		if !(v >= 0 && v <= refiningMaxStationFee) {
			return fmt.Errorf("refining.%s 要在 [0, %g] 银/100 营养内,得到 %g", key, refiningMaxStationFee, v)
		}
		return nil
	}
	if err := fee("station_fee_per100", r.StationFeePer100); err != nil {
		return err
	}
	cities := make([]string, 0, len(r.StationFeeByCity))
	for c := range r.StationFeeByCity {
		cities = append(cities, c)
	}
	sort.Strings(cities)
	for _, c := range cities {
		if err := fee("station_fee_by_city."+c, r.StationFeeByCity[c]); err != nil {
			return err
		}
	}
	if err := fee("transmute_station_fee_per100", r.TransmuteStationFeePer100); err != nil {
		return err
	}
	if !(r.TransmuteFactor >= refiningMinK && r.TransmuteFactor <= refiningMaxK) {
		return fmt.Errorf("refining.transmute_factor 要在 [%g, %g] 内(见过的实测 1.00~1.16),得到 %g",
			refiningMinK, refiningMaxK, r.TransmuteFactor)
	}
	if r.TransmuteGoldPrice < 0 {
		return fmt.Errorf("refining.transmute_gold_price 不能为负,得到 %g", r.TransmuteGoldPrice)
	}
	switch r.Leftover {
	case "carry", "bid", "zero":
	default:
		return fmt.Errorf("refining.leftover 只能是 carry / bid / zero,得到 %q", r.Leftover)
	}
	if !(r.FocusCap > 0) || !(r.FocusPerDay >= 0 && r.FocusPerDay <= r.FocusCap) {
		return fmt.Errorf("refining.focus_per_day 要在 [0, focus_cap=%g] 内,得到 %g", r.FocusCap, r.FocusPerDay)
	}
	if !(r.VolumeShare > 0 && r.VolumeShare <= 1) {
		return fmt.Errorf("refining.volume_share 要在 (0, 1] 内,得到 %g", r.VolumeShare)
	}
	if !(r.TransportPct >= 0 && r.TransportPct < 10) {
		return fmt.Errorf("refining.transport_pct 要在 [0, 10) 内,得到 %g", r.TransportPct)
	}
	types := make([]string, 0, len(r.StationBuildingTypes))
	for l := range r.StationBuildingTypes {
		types = append(types, l)
	}
	sort.Strings(types)
	for _, l := range types {
		ids := r.StationBuildingTypes[l]
		if !known[l] {
			return fmt.Errorf("refining.station_building_types 的键 %q 不认识,只有 %v", l, RefiningLines)
		}
		if len(ids) == 0 {
			return fmt.Errorf("refining.station_building_types.%s 是空列表:不知道就整行删掉", l)
		}
		for _, id := range ids {
			if id <= 0 {
				return fmt.Errorf("refining.station_building_types.%s 里的 %d 不是建筑类型 id(要是正整数)", l, id)
			}
		}
	}
	if !(r.StationFeeMaxAgeHours > 0 && r.StationFeeMaxAgeHours <= refiningMaxStationAgeHours) {
		return fmt.Errorf("refining.station_fee_max_age_hours 要在 (0, %g] 内,得到 %g",
			refiningMaxStationAgeHours, r.StationFeeMaxAgeHours)
	}
	return nil
}
