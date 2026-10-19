package protocol

import (
	"encoding/json"
	"testing"

	"albion-guild/internal/model"
)

func sampleParser(t *testing.T) (*Parser, *[]model.CaptureSample) {
	t.Helper()
	p := New()
	p.Version = "1.2.3"
	var got []model.CaptureSample
	p.OnSample = func(s model.CaptureSample) { got = append(got, s) }
	return p, &got
}

func sampleKeys(t *testing.T, s model.CaptureSample) map[int]string {
	t.Helper()
	var params []TaggedParam
	if err := json.Unmarshal(s.Params, &params); err != nil {
		t.Fatal(err)
	}
	types := map[int]string{}
	for _, tp := range params {
		types[tp.K] = tp.T
	}
	return types
}

// 明文全抓:任何事件都原样记全键样本,每个参数带 Go 类型
func TestHandleEvent_任何事件都记全键样本(t *testing.T) {
	p, got := sampleParser(t)
	p.HandleEvent(1, map[byte]any{paramEventCode: int16(448),
		0: int64(42), 1: []float32{1, 2}, 3: "TNL-031", 7: int64(638000000000000000)})
	if len(*got) != 1 {
		t.Fatalf("应记 1 条,得到 %d", len(*got))
	}
	s := (*got)[0]
	if s.Code != 448 || s.Kind != model.SampleEvent || s.ClientVersion != "1.2.3" || s.ObservedAt.IsZero() {
		t.Fatalf("样本头不对: %+v", s)
	}
	types := sampleKeys(t, s)
	if types[0] != "int64" || types[1] != "[]float32" || types[3] != "string" || types[7] != "int64" ||
		types[252] != "int16" {
		t.Fatalf("类型标签: %v\n%s", types, s.Params)
	}
}

// 事件记样本,但不进业务层:带上 Join 的键也改不了身份和位置;没有码的不记
func TestHandleEvent_事件不进业务层(t *testing.T) {
	p, got := sampleParser(t)
	var identity bool
	p.OnIdentity = func(_, _, _ string) { identity = true }
	for _, code := range []any{byte(3), byte(29), int16(2), int16(406)} {
		p.HandleEvent(1, map[byte]any{paramEventCode: code, 0: "x", 1: "guid", 2: "名字", 8: "0007"})
	}
	p.HandleEvent(1, map[byte]any{0: "没有事件码"})
	if identity || p.Location() != "" {
		t.Fatalf("事件进了业务层:身份回调 %v、位置 %q", identity, p.Location())
	}
	if len(*got) != 4 {
		t.Fatalf("4 条有码的事件都该记样本,得到 %d", len(*got))
	}
}

// 请求、响应、事件三个方向都记,同一个码按方向分开数
func TestSample_三个方向都记且分开数(t *testing.T) {
	p, got := sampleParser(t)
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(81), 1: "请求"})
	p.HandleResponse(1, map[byte]any{paramOperationCode: int16(81), 0: []string{offerJSON}})
	p.HandleEvent(1, map[byte]any{paramEventCode: int16(81)})
	kinds := map[string]int{}
	for _, s := range *got {
		if s.Code != 81 {
			t.Fatalf("码不对: %+v", s)
		}
		kinds[s.Kind]++
	}
	want := map[string]int{model.SampleOpRequest: 1, model.SampleOpResponse: 1, model.SampleEvent: 1}
	if len(kinds) != 3 || kinds[model.SampleOpRequest] != 1 || kinds[model.SampleOpResponse] != 1 ||
		kinds[model.SampleEvent] != 1 {
		t.Fatalf("得到 %v,想要 %v", kinds, want)
	}
}

// 每个 (方向, 码) 每次运行最多 20 条,各码分开数
func TestSample_每个码最多20条(t *testing.T) {
	p, got := sampleParser(t)
	for range 30 {
		p.HandleEvent(1, map[byte]any{paramEventCode: int16(3)})
		p.HandleEvent(1, map[byte]any{paramEventCode: int16(448)})
	}
	n := map[int]int{}
	for _, s := range *got {
		n[s.Code]++
	}
	if n[3] != MaxSamplesPerCode || n[448] != MaxSamplesPerCode {
		t.Fatalf("得到 %v", n)
	}
}

// 样本在白名单过滤之前记:197 响应的样本带全部键(含表外的 99 和 253),
// 配没配上请求、是不是城区都记;业务层照旧只认配上了请求的城区
func TestSample_记在白名单过滤之前(t *testing.T) {
	p, got := sampleParser(t)
	var stations []model.StationObs
	p.OnStations = func(o []model.StationObs) { stations = append(stations, o...) }

	resp := func() map[byte]any { r := clusterMapResponse(); r[99] = "表外的键"; return r }
	p.HandleResponse(1, resp()) // 没有请求
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "1234"})
	p.HandleResponse(1, resp()) // 野外区
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "0000"})
	p.HandleResponse(1, resp()) // 城区

	var responses []model.CaptureSample
	for _, s := range *got {
		if s.Kind == model.SampleOpResponse && s.Code == 197 {
			responses = append(responses, s)
		}
	}
	if len(responses) != 3 {
		t.Fatalf("三条 197 响应都该记样本,得到 %d", len(responses))
	}
	types := sampleKeys(t, responses[2])
	if types[99] != "string" || types[253] != "int16" || types[34] == "" {
		t.Fatalf("样本该带全部键: %v", types)
	}
	if len(stations) != 2 {
		t.Fatalf("业务层只认配上请求的城区,应解出 2 个建筑,得到 %d", len(stations))
	}
}

// 挂单走的是没有 253 的那条路,按卖单列表的码记
func TestSample_没有253的挂单也记(t *testing.T) {
	p, got := sampleParser(t)
	p.HandleResponse(1, map[byte]any{0: []string{offerJSON}})
	if len(*got) != 1 || (*got)[0].Kind != model.SampleOpResponse || (*got)[0].Code != p.Codes.AuctionGetOffers {
		t.Fatalf("得到 %+v", *got)
	}
}

// 码超出 MaxCode 的不记:服务端也按这个上限收,记了也传不上去
func TestSample_码超范围不记(t *testing.T) {
	p, got := sampleParser(t)
	p.HandleEvent(1, map[byte]any{paramEventCode: int32(MaxCode + 1)})
	p.HandleEvent(1, map[byte]any{paramEventCode: int16(-1)})
	if len(*got) != 0 {
		t.Fatalf("得到 %+v", *got)
	}
}

// 没接 OnSample 时不记数也不 panic
func TestSample_没接回调不记(t *testing.T) {
	p := New()
	p.HandleEvent(1, map[byte]any{paramEventCode: int16(150)})
	if len(p.sampled) != 0 {
		t.Fatal("没接回调不该占名额")
	}
}
