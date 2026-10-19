package protocol

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/model"
)

// 照客户端声明的类型拼一条 197 响应:int[] / float[][] / long[] / string[]
func clusterMapResponse() map[byte]any {
	return map[byte]any{
		paramOperationCode: int16(197),
		keyBuildingType:    []int32{501, 502},
		keyPosition:        []any{[]float32{10.5, -3}, []float32{20, 40.25}},
		keyDurability:      []int64{100000, 0},
		keyNutrition:       []int64{25000, 12},
		keyUnknown23:       []int64{7, 8},
		keySilver:          []int64{1 << 40, 5},
		keyOwners:          []string{"甲公会", ""},
		keyCanUse:          []bool{true, false},
		keyAccess:          []byte{2, 0, 0, 0, 0, 0, 0, 0}, // 线上的形态:每个建筑 4 字节小端,Public / Closed
		keyUsable:          []bool{true, false},
		keyAssociateFee:    []int32{3_600_000, 0}, // 线上是 int[]
		keyPublicFee:       []int32{3_900_000, 10_000_000},
		99:                 "白名单外的键",
	}
}

func newMapParser(t *testing.T) (*Parser, *[]model.StationObs, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	p := New()
	p.clock = func() time.Time { return now }
	var got []model.StationObs
	p.OnStations = func(o []model.StationObs) { got = append(got, o...) }
	return p, &got, &now
}

func TestClusterMap_按请求的区解出每个建筑(t *testing.T) {
	p, got, now := newMapParser(t)
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "0000"})
	p.HandleResponse(1, clusterMapResponse())

	if len(*got) != 2 {
		t.Fatalf("应解出 2 个建筑,得到 %d", len(*got))
	}
	a, b := (*got)[0], (*got)[1]
	if a.ZoneID != "0000" || a.Index != 0 || a.BuildingType != 501 || !a.ObservedAt.Equal(*now) {
		t.Fatalf("第一个建筑字段不对: %+v", a)
	}
	if len(a.Pos) != 2 || a.Pos[0] != 10.5 || a.Pos[1] != -3 {
		t.Fatalf("坐标: %v", a.Pos)
	}
	// long 不能塞进 int 截掉(AODP 那个结构体的坑)
	if *a.Silver != 1<<40 || *a.Durability != 100000 || *a.Nutrition != 25000 {
		t.Fatalf("耐久/营养/银: %d %d %d", *a.Durability, *a.Nutrition, *a.Silver)
	}
	// 营养 0 和"没收到"不是一回事
	if b.Durability == nil || *b.Durability != 0 {
		t.Fatal("观测到的 0 要存成 0,不是 nil")
	}
	if a.Owner != "甲公会" || b.Owner != "" {
		t.Fatalf("所有者: %q %q", a.Owner, b.Owner)
	}
	// 23 是对得上建筑数的数组,存那一格
	if string(a.Key23) != `{"t":"int64","v":7}` {
		t.Fatalf("23 原样存: %s", a.Key23)
	}
	// 31 解成访问权限预设,解开了就不再原样存;30/32 是能不能用
	if a.Access == nil || *a.Access != 2 || b.Access == nil || *b.Access != 0 || a.Key31 != nil {
		t.Fatalf("访问权限: %v %v key31=%s", a.Access, b.Access, a.Key31)
	}
	if !*a.CanUse || !*a.Usable || *b.CanUse || *b.Usable {
		t.Fatalf("能不能用: %v %v / %v %v", *a.CanUse, *a.Usable, *b.CanUse, *b.Usable)
	}
	if a.BuildingKey() != "501@10.5,-3" {
		t.Fatalf("连接键: %s", a.BuildingKey())
	}
}

// 标度 2026-10-19 实测定死:a7m 定点 ÷10000(N 地图 495 台站的原值全是 10000 的整数倍,
// 最大正好 1000 银 × 10000)。原值和归一化值都存
func TestClusterMap_费率标度(t *testing.T) {
	p, got, _ := newMapParser(t)
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "0000"})
	p.HandleResponse(1, clusterMapResponse())
	a, b := (*got)[0], (*got)[1]
	cases := []struct {
		raw  *int64
		norm *float64
		want float64
	}{
		{a.AssociateFeeRaw, a.AssociateFee, 360},
		{b.AssociateFeeRaw, b.AssociateFee, 0},
		{a.PublicFeeRaw, a.PublicFee, 390},
		{b.PublicFeeRaw, b.PublicFee, 1000}, // 上限
	}
	for i, c := range cases {
		if c.raw == nil || c.norm == nil || *c.norm != c.want {
			t.Errorf("第 %d 条: 原值 %v 归一化 %v,想要 %v", i, c.raw, c.norm, c.want)
		}
	}
	if *a.AssociateFeeRaw != 3_600_000 {
		t.Error("原值要原样存")
	}
}

func TestNormalizeFee(t *testing.T) {
	// 以前 ≤ 1000 当整银:原值 1000 会读成 1000 银,其实是 0.1 银
	for raw, want := range map[int64]float64{0: 0, 1000: 0.1, 10_000: 1, 5_000_000: 500, 10_000_000: 1000} {
		if got, ok := normalizeFee(raw); !ok || got != want {
			t.Errorf("%d → %v %v,想要 %v", raw, got, ok, want)
		}
	}
	if got, ok := normalizeFee(-1); ok {
		t.Errorf("负数应判失败,得到 %v", got)
	}
}

// 数组长度和 key17 对不上:那个键当没收到并告警,别的键照常,建筑数不截。
// 以前按最短对齐,owners 短一格就把全区截短,前缀也不保证对得齐
func TestDecodeStations_长度对不上的键当没收到(t *testing.T) {
	params := map[byte]any{
		keyBuildingType: []int32{1, 2, 3},
		keyPublicFee:    []any{int64(10), int32(20), int64(30)},
		keyOwners:       []string{"a", "b"},                      // 少一个
		keyNutrition:    []int64{1, 2, 3, 4},                     // 多一个
		keyPosition:     []any{[]float32{1, 2}, []float32{3, 4}}, // 少一行
		keyDurability:   int64(7),                                // 标量当长度 1
	}
	obs, warns := decodeStations("1000", params, time.Now())
	if len(obs) != 3 {
		t.Fatalf("建筑数以 key17 为准不截短,得到 %d", len(obs))
	}
	for i, o := range obs {
		if o.PublicFeeRaw == nil || *o.PublicFeeRaw != int64(10*(i+1)) {
			t.Fatalf("对得上的费率要照常按下标配: %+v", o)
		}
		if o.Owner != "" || o.Nutrition != nil || o.Pos != nil || o.Durability != nil {
			t.Fatalf("对不上的键应当没收到: %+v", o)
		}
	}
	if len(warns) != 4 || !strings.Contains(strings.Join(warns, ";"), "当作没收到") {
		t.Fatalf("四个对不上的键各告警一次,得到 %v", warns)
	}
}

// key18 里的 NaN/Inf 整组判失败:进了 Pos 的话,客户端 json.Marshal 整批上传会报错,
// 同一批的挂单一起丢
func TestDecodeStations_坐标NaN当没收到(t *testing.T) {
	nan := float32(math.NaN())
	for name, v := range map[string]any{
		"NaN":  []any{[]float32{1, 2}, []float32{nan, 3}},
		"+Inf": [][]float64{{1, 2}, {math.Inf(1), 3}},
		"行内": []any{[]any{float32(1), float32(2)}, []any{float64(math.Inf(-1)), float32(3)}},
	} {
		obs, warns := decodeStations("1000", map[byte]any{
			keyBuildingType: []int32{1, 2},
			keyPosition:     v,
			keyPublicFee:    []int64{100, 200},
		}, time.Now())
		if len(obs) != 2 || obs[0].Pos != nil || obs[1].Pos != nil || *obs[1].PublicFeeRaw != 200 {
			t.Fatalf("%s:坐标应当没收到、其余照常,得到 %+v", name, obs)
		}
		if len(warns) != 1 {
			t.Fatalf("%s:要告警一次,得到 %v", name, warns)
		}
		if _, err := json.Marshal(obs); err != nil {
			t.Fatalf("%s:解出来的建筑必须能序列化: %v", name, err)
		}
	}
}

// key31 的几种形态:每建筑 4 字节小端、每建筑 1 字节、整数数组;长度对不上就原样存
func TestAccessPresets(t *testing.T) {
	for name, v := range map[string]any{
		"4 字节小端": []byte{3, 0, 0, 0, 1, 0, 0, 0},
		"1 字节":   []byte{3, 1},
		"整数数组":   []int32{3, 1},
	} {
		got, ok := accessPresets(v, 2)
		if !ok || len(got) != 2 || got[0] != 3 || got[1] != 1 {
			t.Errorf("%s: %v %v", name, got, ok)
		}
	}
	obs, warns := decodeStations("1000", map[byte]any{keyBuildingType: []int32{1, 2}, keyAccess: []byte{1, 2, 3}}, time.Now())
	if len(warns) != 1 || obs[0].Access != nil || obs[0].Key31 == nil {
		t.Fatalf("长度对不上应当告警、原样存: %v %+v", warns, obs[0])
	}
}

// 认不出形态的数组当作没收到并告警,不能让它拖垮整条
func TestDecodeStations_认不出的数组当没收到(t *testing.T) {
	params := map[byte]any{
		keyBuildingType: []int64{1},
		keyNutrition:    "不是数组",
		keyPosition:     []float32{1, 2}, // 一维,不是 float[][]
	}
	obs, warns := decodeStations("1000", params, time.Now())
	if len(obs) != 1 || obs[0].Nutrition != nil || obs[0].Pos != nil {
		t.Fatalf("得到 %+v", obs)
	}
	if len(warns) != 2 {
		t.Fatalf("两个认不出的键各告警一次,得到 %v", warns)
	}
	// 没有建筑类型就认不出有几个建筑,整条不要
	if obs, _ := decodeStations("1000", map[byte]any{keyPublicFee: []int64{1}}, time.Now()); obs != nil {
		t.Fatalf("没有 key17 应该整条丢,得到 %+v", obs)
	}
}

// 坐标外层可能是 [xs, ys]:两行都和建筑数一样长时转置
func TestDecodeStations_坐标转置(t *testing.T) {
	params := map[byte]any{
		keyBuildingType: []int32{1, 2, 3},
		keyPosition:     [][]float32{{1, 2, 3}, {4, 5, 6}},
	}
	obs, warns := decodeStations("1000", params, time.Now())
	if len(obs) != 3 || len(warns) != 0 {
		t.Fatalf("得到 %d 个建筑,告警 %v", len(obs), warns)
	}
	if obs[2].Pos[0] != 3 || obs[2].Pos[1] != 6 {
		t.Fatalf("转置错了: %v", obs[2].Pos)
	}
}

// 归一化之后还超过上限,说明标度规则不对:告警(量纲断言)
func TestDecodeStations_归一化后超上限要告警(t *testing.T) {
	params := map[byte]any{
		keyBuildingType: []int32{1},
		keyPublicFee:    []int64{20_000_000}, // /10000 = 2000 > 1000
	}
	_, warns := decodeStations("1000", params, time.Now())
	if len(warns) != 1 || !strings.Contains(warns[0], "超过上限") {
		t.Fatalf("得到 %v", warns)
	}
}

// 非数字的区(地下城/藏身处/个人岛)跳过;但它的请求也要记,
// 否则它的响应会被配到前一条请求的区上
func TestClusterMap_非数字的区跳过且不串到别的区(t *testing.T) {
	p, got, _ := newMapParser(t)
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "0000"})
	p.HandleResponse(1, clusterMapResponse())
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "ISLAND-PLAYER-xxx"})
	p.HandleResponse(1, clusterMapResponse())
	if len(*got) != 2 {
		t.Fatalf("个人岛那条不该收,也不该配到 0000 上,得到 %d 条", len(*got))
	}
	// 野外区的 id 也是纯数字,但不是城区本体:不收(我们只要城里的制造站)
	for _, zone := range []string{"1234", "0301"} {
		p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: zone})
		p.HandleResponse(1, clusterMapResponse())
	}
	if len(*got) != 2 {
		t.Fatalf("野外区 / 传送门城不该收,得到 %d 条", len(*got))
	}
}

// 同一个区的几条请求不算歧义:两张网卡各抓到同一个包,或者上一条响应没抓到、
// 30 秒内同城又开了一次图。以前按条数数,一条都配不上
func TestClusterMap_同区重复请求照样配(t *testing.T) {
	p, got, now := newMapParser(t)
	req := map[byte]any{paramOperationCode: int16(197), 0: "0000"}
	// 双网卡:请求、响应各来两次
	p.HandleRequest(1, req)
	p.HandleRequest(1, req)
	p.HandleResponse(1, clusterMapResponse())
	p.HandleResponse(1, clusterMapResponse()) // 第二份没请求可配,丢,不重复收
	if len(*got) != 2 || (*got)[0].ZoneID != "0000" {
		t.Fatalf("双网卡重复抓包应配上一次,得到 %d 条", len(*got))
	}
	// 第一条响应没抓到,10 秒后同城再开一次
	p.HandleRequest(1, req)
	*now = now.Add(10 * time.Second)
	p.HandleRequest(1, req)
	p.HandleResponse(1, clusterMapResponse())
	if len(*got) != 4 {
		t.Fatalf("同城两次请求应配上,得到 %d 条", len(*got))
	}
	if p.ZoneMapDropped() != 0 {
		t.Fatalf("同区不算配不出,丢弃计数 %d", p.ZoneMapDropped())
	}
	// 真出现两个区才算配不出,计数
	p.HandleRequest(1, req)
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "1000"})
	p.HandleResponse(1, clusterMapResponse())
	if len(*got) != 4 || p.ZoneMapDropped() != 1 {
		t.Fatalf("两个区应丢并计数,得到 %d 条、计数 %d", len(*got), p.ZoneMapDropped())
	}
}

// 响应只能靠请求关联。没有请求、或者同时好几条在等,都配不出来,宁可丢
func TestClusterMap_配不出请求就丢(t *testing.T) {
	p, got, now := newMapParser(t)
	p.HandleResponse(1, clusterMapResponse()) // 没有请求
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "0000"})
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "1000"})
	p.HandleResponse(1, clusterMapResponse()) // 两条在等,不知道是谁的
	p.HandleResponse(1, clusterMapResponse()) // 上一条把等待清空了
	if len(*got) != 0 {
		t.Fatalf("配不出请求的响应不该收,得到 %d 条", len(*got))
	}
	// 过期的请求不算在等:30 秒前那条响应多半是没抓到
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "0000"})
	*now = now.Add(time.Minute)
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "1000"})
	p.HandleResponse(1, clusterMapResponse())
	if len(*got) != 2 || (*got)[0].ZoneID != "1000" {
		t.Fatalf("过期请求不该挡住新请求,得到 %+v", *got)
	}
}
