package flip

import (
	"context"
	"log/slog"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"albion-guild/internal/gamedata"
	"albion-guild/internal/refine"
	"albion-guild/internal/store"
)

// 抓到的工作站费率接进精炼(op 197 GetClusterMapInfo,docs/client-data.md 第二点六节)。
//
// 优先级从高到低:
//  1. 请求里写明的 fees.station_per100(用户手填)
//  2. 抓包:这座城、这条线的精炼站,不超过 station_fee_max_age_hours 的,取最便宜那台
//  3. 配置的 station_fee_by_city
//  4. 配置的 station_fee_per100
//
// 哪种建筑类型是哪条线的精炼站用内置表(internal/gamedata,从客户端 buildings.xml 算的,
// 2026-10-19 拿 N 地图 495 台站核过),配置 refining.station_building_types 可以再补。
// 表里没有的类型不进计算,只在响应里列出来(StationFeeInfo.Unmapped)。

// StationFeeSource 是 RefineSource 的可选能力:抓包存下来的工作站观测,每台站最新一条
// (*store.Store 有;测试里的假数据源实现它就能喂费率)。
type StationFeeSource interface {
	StationFees(ctx context.Context, since time.Time) ([]store.StationFee, error)
}

const (
	// stationFeeCacheTTL:站费读库结果缓存 60 秒。房主设的费率不常变,一分钟的延迟无所谓
	stationFeeCacheTTL = 60 * time.Second
	// stationFeeWait:点击路径上最多等读库这么久,到点就当没抓到(用配置),读库留在后台跑完进缓存
	stationFeeWait = 2 * time.Second
	// stationFeeMax:费率上限(规则表 E3 ✔,maxuseagefee=1000)。抓包侧已经按 raw > 1000 → /10000
	// 归一化过,这里再断言一次:标度猜错时宁可不用,也不悄悄算错
	stationFeeMax = 1000.0
)

// 站费来源(StationFeeInfo.Source)
const (
	FeeSourceRequest = "request"
	FeeSourceCapture = "capture"
	FeeSourceConfig  = "config"
	FeeSourceDefault = "default"
)

// StationFeeInfo 说明这次精炼站费是从哪来的,跟着 /api/refine/plan、/api/refine/matrix 的响应走。
type StationFeeInfo struct {
	// Source:request 你填的 | capture 抓包 | config 配置按城市覆盖 | default 配置的全局默认
	Source string  `json:"source"`
	Fee    float64 `json:"fee"`
	City   string  `json:"city"`
	Line   string  `json:"line"`
	// Transmute:抓包费率也当了转化主人费率(refining.station_fee_transmute,请求里写了主人费率时为 false)
	Transmute bool `json:"transmute"`
	// Station 是抓包里这座城这条线最便宜的那台(Source=request 时也照给,方便对照)
	Station *StationPick `json:"station,omitempty"`
	// Stations 是这座城这条线抓到、新鲜、能用的台数;Min / Max 是它们的费率范围
	Stations int      `json:"stations"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
	// Seen 是这座城抓到的新鲜站总数(任何建筑类型、费率合法的)
	Seen int `json:"seen"`
	// Unmapped 是这座城抓到、但不是任何一条线精炼站的建筑类型(铁匠铺、厨房……)
	Unmapped []KindFee `json:"unmapped"`
	// Unusable:映射上了、但观测到营养或耐久是 0 的台数(这种站不能精炼),不参与取最便宜
	Unusable int `json:"unusable"`
	// Closed:映射上了、但对你不开放的台数(房主设成 Closed / Private,或抓包的角色用不了),不参与
	Closed int `json:"closed"`
	// Bad:费率不在 [0, 1000] 内、整台丢掉的台数(标度可能猜错了,服务端日志有告警)
	Bad int `json:"bad"`
	// MaxAgeHours 是认多旧以内的抓包费率(refining.station_fee_max_age_hours)
	MaxAgeHours float64 `json:"max_age_hours"`
	// Error 非空:这次没读到抓包费率(库慢、出错),按配置算
	Error string `json:"error,omitempty"`
}

// StationPick 是被选中的那台站。
type StationPick struct {
	ZoneID       string    `json:"zone_id"`
	BuildingKey  string    `json:"building_key"`
	BuildingType int32     `json:"building_type"`
	Owner        string    `json:"owner"`
	Fee          float64   `json:"fee"`
	SeenAt       time.Time `json:"seen_at"`
	AgeH         float64   `json:"age_h"`
	Reporter     string    `json:"reporter"`
}

// KindFee 是一种没映射的建筑类型在这座城的费率汇总。
type KindFee struct {
	BuildingType int32   `json:"building_type"`
	FeeMin       float64 `json:"fee_min"`
	FeeMax       float64 `json:"fee_max"`
	Stations     int     `json:"stations"`
	// AgeH 是这一类里最新那条观测的年龄
	AgeH float64 `json:"age_h"`
}

// RefineDefaultsLive 是一次请求打底的参数,和 RefineDefaults 一样,再叠上抓到的站费:
// 这座城这条线有新鲜观测、建筑类型也映射了,就用最便宜那台的费率(开关打开时连转化主人费率一起)。
// 返回的 StationFeeInfo 要在请求体 / 查询参数写进参数之后交给 SettleStationFee 收尾。
func (s *Service) RefineDefaultsLive(ctx context.Context, target, city string) (refine.Params, StationFeeInfo) {
	p := s.RefineDefaults(target, city)
	line, ok := refine.LineOfItem(p.Target)
	if !ok {
		return p, StationFeeInfo{Source: FeeSourceDefault, Fee: p.Fees.StationPer100, City: p.City, Unmapped: []KindFee{}}
	}
	info := s.stationFeeInfo(ctx, line, p.City, s.refine.now().UTC())
	if info.Source == FeeSourceCapture {
		p.Fees.StationPer100 = info.Fee
		if s.Cfg.Refining.StationFeeTransmute {
			p.Fees.TransmuteStationPer100 = info.Fee
			info.Transmute = true
		}
	}
	return p, info
}

// SettleStationFee 在请求写进参数之后收尾。reqStation / reqTransmute:请求里写明了精炼站费 /
// 转化主人费率。
//   - 写明了站费:以请求为准,source=request(抓到的那台照样留在 info 里给界面对照)
//   - 抓包命中:往 Confirmed 里补站费键(开关打开、请求没写主人费率时连主人费率键),
//     「参数未核实」的警告就不再出现。请求体里的 confirmed 会整个替换打底那份,所以必须在解码之后补。
//     这改变了 Confirmed 只表示「用户说过」的语义,靠 station_fee.source 区分
func SettleStationFee(p *refine.Params, info *StationFeeInfo, reqStation, reqTransmute bool) {
	if reqTransmute {
		info.Transmute = false
	}
	if reqStation {
		info.Source, info.Fee = FeeSourceRequest, p.Fees.StationPer100
		// 只写了站费、没写主人费率:主人费率还是抓包那台的(打底时填的),照样补「核对过」,
		// 来源靠 info.Transmute 区分。以前不补,界面把抓到的数标成「服务端默认值,没核实」
		if info.Transmute {
			p.Confirmed = append(p.Confirmed, "fees.transmute_station_per100")
		}
		return
	}
	if info.Source != FeeSourceCapture {
		return
	}
	p.Confirmed = append(p.Confirmed, "fees.station_per100")
	if info.Transmute {
		p.Confirmed = append(p.Confirmed, "fees.transmute_station_per100")
	}
}

// stationFeeInfo 读抓到的站费(60 秒缓存),按城市和线挑。读不到时按配置算,不报错。
func (s *Service) stationFeeInfo(ctx context.Context, line refine.Line, city string, now time.Time) StationFeeInfo {
	s.refine.init()
	r := s.Cfg.Refining
	info := StationFeeInfo{Source: FeeSourceDefault, Fee: r.StationFeeFor(city), City: city, Line: line.Key,
		Unmapped: []KindFee{}, MaxAgeHours: r.StationFeeMaxAgeHours}
	if _, ok := r.StationFeeByCity[city]; ok {
		info.Source = FeeSourceConfig
	}
	src, ok := s.refineSource().(StationFeeSource)
	if !ok || r.StationFeeMaxAgeHours <= 0 {
		return info
	}
	maxAge := r.StationFeeMaxAgeHours
	wctx, cancel := context.WithTimeout(ctx, stationFeeWait)
	defer cancel()
	key := strconv.FormatFloat(maxAge, 'f', -1, 64)
	rows, _, err := s.refine.fees.get(wctx, key, func(c context.Context) ([]store.StationFee, error) {
		return src.StationFees(c, s.refine.now().Add(-time.Duration(maxAge*float64(time.Hour))))
	})
	if err != nil {
		slog.Warn("精炼页没读到抓包站费,按配置算", "city", city, "err", err)
		info.Error = err.Error()
		return info
	}
	m := stationMapping(r.StationBuildingTypes)
	return pickStationFee(info, rows, m[line.Key], m, now)
}

// stationMapping 是精炼线 → 认哪些建筑类型:内置表加上配置里额外写的
func stationMapping(extra map[string][]int32) map[string][]int32 {
	out := make(map[string][]int32, len(gamedata.Lines))
	for _, l := range gamedata.Lines {
		out[l] = gamedata.StationTypes(l)
	}
	for l, ids := range extra {
		for _, id := range ids {
			if !slices.Contains(out[l], id) {
				out[l] = append(out[l], id)
			}
		}
	}
	return out
}

// pickStationFee 是纯函数:在 rows 里挑这座城、这条线映射到的建筑里最便宜的一台。
// 假设玩家会去最便宜的站;同价取观测最新的。对你不开放的、营养或耐久观测到 0 的站不能用,不参与。
func pickStationFee(info StationFeeInfo, rows []store.StationFee, types []int32,
	mapping map[string][]int32, now time.Time) StationFeeInfo {

	mapped := map[int32]bool{}
	for _, ids := range mapping {
		for _, id := range ids {
			mapped[id] = true
		}
	}
	unmapped := map[int32]*KindFee{}
	var pick *StationPick
	for _, row := range rows {
		if !strings.EqualFold(row.City, info.City) || row.PublicFee == nil {
			continue
		}
		age := math.Max(0, now.Sub(row.ObservedAt).Hours())
		if age > info.MaxAgeHours {
			continue
		}
		fee := *row.PublicFee
		if math.IsNaN(fee) || fee < 0 || fee > stationFeeMax {
			info.Bad++
			slog.Warn("抓到的站费不在 [0, 1000] 内,不用(标度可能猜错了)", "city", row.City, "zone", row.ZoneID,
				"building", row.BuildingKey, "fee", fee, "raw", row.PublicFeeRaw)
			continue
		}
		info.Seen++
		if !slices.Contains(types, row.BuildingType) {
			if !mapped[row.BuildingType] {
				k := unmapped[row.BuildingType]
				if k == nil {
					k = &KindFee{BuildingType: row.BuildingType, FeeMin: fee, FeeMax: fee, AgeH: age}
					unmapped[row.BuildingType] = k
				}
				k.Stations++
				k.FeeMin, k.FeeMax, k.AgeH = math.Min(k.FeeMin, fee), math.Max(k.FeeMax, fee), math.Min(k.AgeH, age)
			}
			continue
		}
		if row.Closed() {
			info.Closed++
			continue
		}
		if (row.Nutrition != nil && *row.Nutrition == 0) || (row.Durability != nil && *row.Durability == 0) {
			info.Unusable++
			continue
		}
		info.Stations++
		if info.Min == nil || fee < *info.Min {
			v := fee
			info.Min = &v
		}
		if info.Max == nil || fee > *info.Max {
			v := fee
			info.Max = &v
		}
		if pick == nil || fee < pick.Fee || (fee == pick.Fee && row.ObservedAt.After(pick.SeenAt)) {
			pick = &StationPick{ZoneID: row.ZoneID, BuildingKey: row.BuildingKey, BuildingType: row.BuildingType,
				Owner: row.Owner, Fee: fee, SeenAt: row.ObservedAt.UTC(), AgeH: round3(age), Reporter: row.Reporter}
		}
	}
	for _, k := range unmapped {
		k.AgeH = round3(k.AgeH)
		info.Unmapped = append(info.Unmapped, *k)
	}
	sort.Slice(info.Unmapped, func(i, j int) bool { return info.Unmapped[i].BuildingType < info.Unmapped[j].BuildingType })
	if pick != nil {
		info.Source, info.Fee, info.Station = FeeSourceCapture, pick.Fee, pick
	}
	return info
}
