package protocol

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"albion-guild/internal/model"
	"albion-guild/internal/world"
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
	keyCanUse       = 30 // bool[],对抓包的这个角色能不能用
	keyAccess       = 31 // AccessRights.Preset[]:Closed 0 / Private 1 / Public 2 / Custom 3
	keyUsable       = 32 // bool[],和 30 在样本里总是一样
	// 两档使用费,线上是 int[](a7m 定点 ×10000)。33 = 公会 / 盟友价、34 = 对外公开价:
	// 社区一个来源这么说,2026-10-19 的 495 台站里没有一台 33 比 34 贵,和"对自己人便宜"对得上
	keyAssociateFee = 33
	keyPublicFee    = 34
)

// maxUsageFee 是 buildings.xml 里的 maxuseagefee:游戏内能设的使用费上限(每 100 营养,银币)。
// 归一化之后再拿它做一次量纲断言
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
// 在等的请求**按区去重后只剩一个区**才配。真出现了不同的区就说不清哪条响应对哪个区,
// 宁可整批丢掉也不猜:配错了,一个区的费率就挂到了另一个区上,而这块数据
// 没有 AODP 兜底,错了没人能发现。
//
// 同一个区的几条不算歧义:两张网卡各抓到同一个包(Hyper-V 外部交换机、桥接网卡),
// 或者上一条响应没抓到、30 秒内同城又开了一次图,点名的都是同一个区。
// 以前按条数数,这两种情况一条都配不上
//
// firstDrop 是"这次运行里第一次因为歧义丢响应",调用方据此 Warn 一次
func (p *Parser) takeZoneRequest(now time.Time) (zone, why string, firstDrop bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	reqs := pruneZoneRequests(p.zoneReqs, now)
	p.zoneReqs = nil
	if len(reqs) == 0 {
		return "", "没有对应的 197 请求", false
	}
	zones := map[string]bool{}
	for _, r := range reqs {
		zones[r.zone] = true
	}
	if len(zones) == 1 {
		return reqs[0].zone, "", false
	}
	p.zoneDropped++
	return "", fmt.Sprintf("同时有 %d 个不同的区在等 197 响应,配不出是哪个区", len(zones)), p.zoneDropped == 1
}

// ZoneMapDropped 是这次运行里因为配不出区而丢掉的 197 响应条数。
func (p *Parser) ZoneMapDropped() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.zoneDropped
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

// CityZone 判断一个 197 区 id 是不是城区本体(world.ClusterCity 认得的那几个)。
//
// 以前只判"纯数字":AODP 处理 197 时是这么跳过地下城/藏身处/个人岛的。但野外区的
// id 也是纯数字,那样会把野外的建筑、所有者和坐标一起收进来,和"我们只要城里的
// 制造站"不符。服务端收上传时用同一个判断(api.saveCaptureExtras)
func CityZone(zone string) bool {
	_, ok := world.ClusterCity(zone)
	return ok
}

func (p *Parser) handleClusterMap(params map[byte]any) {
	now := p.now()
	zone, why, firstDrop := p.takeZoneRequest(now)
	if zone == "" {
		// 配不出的那种只在第一次 Warn:成员看不到 Debug,一条站费都传不上去时得有迹可查
		if firstDrop {
			slog.Warn("区域地图响应丢弃(之后同类只记 Debug)", "reason", why)
		} else {
			slog.Debug("区域地图响应丢弃", "reason", why)
		}
		return
	}
	if !CityZone(zone) {
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
// 其余数组长度和它对不上时**这个键当作没收到**并告警,别的键照常——
// 以前按最短的对齐,一个非关键键(比如 owners)短一格就把全区截短,前缀也不保证
// 对得齐,注释里说要避免的「费率挂到别的建筑上」照样会发生。
// 认不出形态的数组(含 NaN/Inf 坐标)也当作没收到,也告警。
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
	// aligned 判长度,对不上就告警并当没收到
	aligned := func(key byte, l int) bool {
		if l == n {
			return true
		}
		warns = append(warns, fmt.Sprintf("key%d 有 %d 项,key17 有 %d 项,对不上是哪个建筑,当作没收到", key, l, n))
		return false
	}

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
		if !aligned(key, len(a)) {
			return nil
		}
		return a
	}
	durability := longs(keyDurability)
	nutrition := longs(keyNutrition)
	silver := longs(keySilver)
	assoc := longs(keyAssociateFee)
	public := longs(keyPublicFee)

	var owners []string
	if v, present := params[keyOwners]; present {
		if a, ok := strs(v); !ok {
			warns = append(warns, fmt.Sprintf("key25 形态认不出(%T),当作没收到", v))
		} else if aligned(keyOwners, len(a)) {
			owners = a
		}
	}

	flags := func(key byte) []bool {
		v, present := params[key]
		if !present {
			return nil
		}
		a, ok := v.([]bool)
		if !ok {
			warns = append(warns, fmt.Sprintf("key%d 形态认不出(%T),当作没收到", key, v))
			return nil
		}
		if !aligned(key, len(a)) {
			return nil
		}
		return a
	}
	canUse, usable := flags(keyCanUse), flags(keyUsable)
	var access []int64
	if v, present := params[keyAccess]; present {
		if a, ok := accessPresets(v, n); !ok {
			warns = append(warns, fmt.Sprintf("key31 形态认不出(%T),当作没收到", v))
		} else if aligned(keyAccess, len(a)) {
			access = a
		}
	}

	var pos [][]float64
	if v, present := params[keyPosition]; present {
		// floatMatrix 遇到 NaN/Inf 整组判失败:坐标进了 Pos,上传时 json.Marshal 整批报错
		if m, ok := floatMatrix(v); !ok {
			warns = append(warns, fmt.Sprintf("key18 形态认不出(%T,或含 NaN/Inf),当作没收到", v))
		} else if m = alignPositions(m, n); aligned(keyPosition, len(m)) {
			pos = m
		}
	}

	ambiguous := 0
	fee := func(raw int64) *float64 {
		f, ok := normalizeFee(raw)
		if !ok {
			ambiguous++
			return nil
		}
		return &f
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
			o.AssociateFeeRaw, o.AssociateFee = r, fee(*r)
		}
		if r := at(public, i); r != nil {
			o.PublicFeeRaw, o.PublicFee = r, fee(*r)
		}
		if canUse != nil {
			o.CanUse = ptr(canUse[i])
		}
		if usable != nil {
			o.Usable = ptr(usable[i])
		}
		if access != nil {
			o.Access = ptr(int32(access[i]))
		} else {
			o.Key31 = rawElement(params, keyAccess, i, len(types))
		}
		o.Key23 = rawElement(params, keyUnknown23, i, len(types))
		out[i] = o
	}
	if ambiguous > 0 {
		warns = append(warns, fmt.Sprintf("%d 个使用费原值为负,只存原值", ambiguous))
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

// normalizeFee 把线上的使用费换算成银币:a7m 定点,÷10000。
//
// 2026-10-19 实测定死(docs/client-data.md 第二点六节):N 地图 495 台站,key33/34 原值全是
// 10000 的整数倍,最大正好 10,000,000 = maxuseagefee 1000 × 10000。以前标度没定,
// 原值 ≤ 1000 当整银——那样原值 1000(0.1 银)会读成 1000 银。负数判失败
func normalizeFee(raw int64) (float64, bool) {
	if raw < 0 {
		return 0, false
	}
	return float64(raw) / model.PriceScale, true
}

// accessPresets 解 key31(AccessRights.Preset[])。线上到我们这里是 byte[],每个建筑 4 字节
// 小端 int32(2026-10-19 Lymhurst 99 个建筑 396 字节,值只有 0/2/3);也认一个建筑一字节、
// 和直接给整数数组的形态
func accessPresets(v any, n int) ([]int64, bool) {
	if b, ok := v.([]byte); ok {
		switch len(b) {
		case 4 * n:
			out := make([]int64, n)
			for i := range n {
				out[i] = int64(int32(binary.LittleEndian.Uint32(b[4*i:])))
			}
			return out, true
		case n:
			out := make([]int64, n)
			for i, x := range b {
				out[i] = int64(x)
			}
			return out, true
		}
		return nil, false
	}
	return int64s(v)
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
