package protocol

import (
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"albion-guild/internal/model"
)

// op 197 GetClusterMapInfo:区域地图(城里按 N 键那张)。一次响应覆盖整个区的建筑,
// 精炼/制造成本模型缺的那几个输入(使用费、营养、耐久)都在里面。
// 键和类型见 docs/client-data.md 第二点六节。
const (
	keyBuildingType = 17 // int[]
	keyPosition     = 18 // float[][]
	keyDurability   = 20 // long[]
	keyNutrition    = 22 // long[](AvailableFood)
	keyUnknown23    = 23 // 含义未知,原样存
	keySilver       = 24 // long[](AvailableSilver)
	keyOwners       = 25 // string[]
	keyUnknown31    = 31 // 含义未知,原样存
	keyAssociateFee = 33 // long[],只有社区一个来源说它是 Associate
	keyPublicFee    = 34 // long[],只有社区一个来源说它是 Public
)

// maxUsageFee 是 buildings.xml 里的 maxuseagefee:游戏内能设的使用费上限(每 100 营养,银币)。
// 线上 key33/34 的标度没定,拿它做量纲断言(normalizeFee)
const maxUsageFee = 1000

// zoneRequestTTL 是一条 197 请求等响应的最长时间。响应正常一两百毫秒就到,
// 超过这个数还没配上的请求多半是响应没抓到(加密、丢包),留着只会把下一条配错
const zoneRequestTTL = 30 * time.Second

type zoneRequest struct {
	zone string
	at   time.Time
}

// noteZoneRequest 记一条 197 请求。非数字的区(地下城/藏身处/个人岛)也记,
// 只是到时候不收——不记的话它的响应会被配到前一条请求的区上。
func (p *Parser) noteZoneRequest(zone string, now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.zoneReqs = append(pruneZoneRequests(p.zoneReqs, now), zoneRequest{zone, now})
}

// takeZoneRequest 给一条 197 响应配请求。响应里不一定带区 id,只能靠请求关联。
//
// 只在**恰好一条**请求在等的时候配。同时有好几条在等就说不清哪条响应对哪个区,
// 宁可整批丢掉也不猜:配错了,一个区的费率就挂到了另一个区上,而这块数据
// 没有 AODP 兜底,错了没人能发现。
func (p *Parser) takeZoneRequest(now time.Time) (zone string, why string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reqs := pruneZoneRequests(p.zoneReqs, now)
	p.zoneReqs = nil
	switch len(reqs) {
	case 0:
		return "", "没有对应的 197 请求"
	case 1:
		return reqs[0].zone, ""
	}
	return "", fmt.Sprintf("同时有 %d 条 197 请求在等,配不出是哪个区", len(reqs))
}

func pruneZoneRequests(reqs []zoneRequest, now time.Time) []zoneRequest {
	out := reqs[:0]
	for _, r := range reqs {
		if now.Sub(r.at) <= zoneRequestTTL {
			out = append(out, r)
		}
	}
	return out
}

// numericZone 判断区 id 是不是纯数字。地下城、藏身处、个人岛的 id 带字母,
// AODP 处理 197 时也是这么跳过的;我们只要城里的制造站。
func numericZone(zone string) bool {
	if zone == "" {
		return false
	}
	for _, r := range zone {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (p *Parser) handleClusterMap(params map[byte]any) {
	now := p.now()
	zone, why := p.takeZoneRequest(now)
	if zone == "" {
		slog.Debug("区域地图响应丢弃", "reason", why)
		return
	}
	if !numericZone(zone) {
		return
	}
	obs, warns := decodeStations(zone, params, now)
	for _, w := range warns {
		slog.Warn("区域地图响应:"+w, "zone", zone)
	}
	if len(obs) > 0 && p.OnStations != nil {
		p.OnStations(obs)
	}
}

// decodeStations 把一条 197 响应按下标拆成一个建筑一行。
//
// 以 key17(建筑类型)为锚:没有它就认不出有几个建筑,整条不要。
// 其余数组长度和它不一致时按最短的对齐并告警——多出来的那几格对不上是哪个建筑,
// 硬对会把费率挂到别的建筑上。认不出形态的数组当作没收到,也告警。
func decodeStations(zone string, params map[byte]any, now time.Time) ([]model.StationObs, []string) {
	var warns []string
	types, ok := int64s(params[keyBuildingType])
	if !ok || len(types) == 0 {
		if params[keyBuildingType] != nil {
			warns = append(warns, fmt.Sprintf("key17 形态认不出(%T),整条丢弃", params[keyBuildingType]))
		}
		return nil, warns
	}
	n := len(types)
	lens := map[byte]int{}

	longs := func(key byte) []int64 {
		v, present := params[key]
		if !present {
			return nil
		}
		a, ok := int64s(v)
		if !ok {
			warns = append(warns, fmt.Sprintf("key%d 形态认不出(%T),当作没收到", key, v))
			return nil
		}
		lens[key] = len(a)
		return a
	}
	durability := longs(keyDurability)
	nutrition := longs(keyNutrition)
	silver := longs(keySilver)
	assoc := longs(keyAssociateFee)
	public := longs(keyPublicFee)

	var owners []string
	if v, present := params[keyOwners]; present {
		if a, ok := strs(v); ok {
			owners = a
			lens[keyOwners] = len(a)
		} else {
			warns = append(warns, fmt.Sprintf("key25 形态认不出(%T),当作没收到", v))
		}
	}

	var pos [][]float64
	if v, present := params[keyPosition]; present {
		if m, ok := floatMatrix(v); ok {
			pos = alignPositions(m, n)
			lens[keyPosition] = len(pos)
		} else {
			warns = append(warns, fmt.Sprintf("key18 形态认不出(%T),当作没收到", v))
		}
	}

	for key, l := range lens {
		if l != len(types) {
			warns = append(warns, fmt.Sprintf("key%d 有 %d 项,key17 有 %d 项,按最短对齐", key, l, len(types)))
		}
		n = min(n, l)
	}

	out := make([]model.StationObs, n)
	for i := range n {
		o := model.StationObs{
			ZoneID:       zone,
			Index:        i,
			BuildingType: int32(types[i]),
			ObservedAt:   now,
			Durability:   at(durability, i),
			Nutrition:    at(nutrition, i),
			Silver:       at(silver, i),
		}
		if pos != nil {
			o.Pos = pos[i]
		}
		if owners != nil {
			o.Owner = owners[i]
		}
		if r := at(assoc, i); r != nil {
			o.AssociateFeeRaw, o.AssociateFee = r, ptr(normalizeFee(*r))
		}
		if r := at(public, i); r != nil {
			o.PublicFeeRaw, o.PublicFee = r, ptr(normalizeFee(*r))
		}
		o.Key23 = rawElement(params, keyUnknown23, i, len(types))
		o.Key31 = rawElement(params, keyUnknown31, i, len(types))
		out[i] = o
	}

	// 量纲断言:归一化之后还超过上限,说明标度规则不对(或者那个键根本不是费率)
	for _, o := range out {
		for _, f := range []*float64{o.AssociateFee, o.PublicFee} {
			if f != nil && *f > maxUsageFee {
				warns = append(warns, fmt.Sprintf("使用费归一化后是 %.2f,超过上限 %d,标度规则可能不对", *f, maxUsageFee))
				return out, warns
			}
		}
	}
	return out, warns
}

// alignPositions 把坐标统一成"一个建筑一行"。客户端声明的是 float[][],
// 但没说外层是建筑还是轴:外层长度是 2、内层两行都和建筑数一样长时,
// 当成 [xs, ys] 转置一下。其余原样(外层就是建筑)。
func alignPositions(m [][]float64, n int) [][]float64 {
	if len(m) == 2 && n != 2 && len(m[0]) == n && len(m[1]) == n {
		out := make([][]float64, n)
		for i := range n {
			out[i] = []float64{m[0][i], m[1][i]}
		}
		return out
	}
	return m
}

// normalizeFee 把线上的使用费换算成银币。客户端字段是 a7m(定点,比例 10000),
// 但换算发生在没反汇编的代码里,线上到底是定点还是整银没定。
// 规则(docs/client-data.md 第二点六节):原值 > maxuseagefee(1000)按 /10000,否则原值。
func normalizeFee(raw int64) float64 {
	if raw > maxUsageFee {
		return float64(raw) / model.PriceScale
	}
	return float64(raw)
}

// rawElement 把含义未知的键带类型标签原样留下:是数组且和建筑数对得上,
// 存这个建筑那一格;对不上存整个值。键不在就是 nil。
func rawElement(params map[byte]any, key byte, i, n int) []byte {
	v, ok := params[key]
	if !ok {
		return nil
	}
	rv := reflect.ValueOf(v)
	if v != nil && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) && rv.Len() == n {
		return tagJSON(rv.Index(i).Interface())
	}
	return tagJSON(v)
}

func at(a []int64, i int) *int64 {
	if i >= len(a) {
		return nil
	}
	return ptr(a[i])
}

func ptr[T any](v T) *T { return &v }
