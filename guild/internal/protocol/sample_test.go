package protocol

import (
	"encoding/json"
	"strings"
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

// 专精事件(用户 2026-10-17 批准的例外)原样记下来,每个参数带 Go 类型
func TestHandleEvent_专精事件记样本(t *testing.T) {
	p, got := sampleParser(t)
	p.HandleEvent(1, map[byte]any{paramEventCode: int16(153),
		0: int64(42), 1: []int32{1, 2}, 2: []any{"x", int16(3)}})
	if len(*got) != 1 {
		t.Fatalf("应记 1 条,得到 %d", len(*got))
	}
	s := (*got)[0]
	if s.Code != 153 || s.Kind != model.SampleEvent || s.ClientVersion != "1.2.3" || s.ObservedAt.IsZero() {
		t.Fatalf("样本头不对: %+v", s)
	}
	var params []TaggedParam
	if err := json.Unmarshal(s.Params, &params); err != nil {
		t.Fatal(err)
	}
	types := map[int]string{}
	for _, tp := range params {
		types[tp.K] = tp.T
	}
	if types[0] != "int64" || types[1] != "[]int32" || types[2] != "[]interface {}" || types[252] != "int16" {
		t.Fatalf("类型标签: %v\n%s", types, s.Params)
	}
}

// 不在例外表里的事件立刻返回,什么都不记;码按 int 认,150+256 不能被截成 150
func TestHandleEvent_其他事件一律丢弃(t *testing.T) {
	p, got := sampleParser(t)
	var identity bool
	p.OnIdentity = func(_, _, _ string) { identity = true }
	for _, code := range []any{byte(3), byte(29), int16(151), int16(406), int32(711), int16(2)} {
		p.HandleEvent(1, map[byte]any{paramEventCode: code, 0: "x", 1: "guid", 2: "名字", 8: "0007"})
	}
	p.HandleEvent(1, map[byte]any{0: "没有事件码"})
	if len(*got) != 0 || identity || p.Location() != "" {
		t.Fatalf("事件漏进来了:样本 %d 条、身份回调 %v、位置 %q", len(*got), identity, p.Location())
	}
}

// 每个码每次运行最多 20 条,各码分开数
func TestSample_每个码最多20条(t *testing.T) {
	p, got := sampleParser(t)
	for range 30 {
		p.HandleEvent(1, map[byte]any{paramEventCode: int16(150)})
		p.HandleEvent(1, map[byte]any{paramEventCode: int16(152)})
	}
	n := map[int]int{}
	for _, s := range *got {
		n[s.Code]++
	}
	if n[150] != MaxSamplesPerCode || n[152] != MaxSamplesPerCode {
		t.Fatalf("得到 %v", n)
	}
}

// 197 响应:样本记全部键(布局发现用),业务层照样只拿白名单里那几个
func TestSample_197响应记全键业务层照样剥键(t *testing.T) {
	p, got := sampleParser(t)
	var stations []model.StationObs
	p.OnStations = func(o []model.StationObs) { stations = append(stations, o...) }
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "0000"})
	p.HandleResponse(1, clusterMapResponse())

	if len(*got) != 1 || (*got)[0].Kind != model.SampleOpResponse || (*got)[0].Code != 197 {
		t.Fatalf("应记一条 197 样本,得到 %+v", *got)
	}
	if !strings.Contains(string((*got)[0].Params), `{"k":99,"t":"string","v":"白名单外的键"}`) {
		t.Fatalf("样本要带全部键: %s", (*got)[0].Params)
	}
	if len(stations) != 2 {
		t.Fatalf("业务层照常解出建筑,得到 %d", len(stations))
	}
	// 请求不记样本
	for _, s := range *got {
		if s.Kind != model.SampleOpResponse {
			t.Fatalf("只有响应和事件记样本: %+v", s)
		}
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
