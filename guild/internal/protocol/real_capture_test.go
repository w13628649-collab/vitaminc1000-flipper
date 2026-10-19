package protocol

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"testing"

	"albion-guild/internal/capture"
	"albion-guild/internal/model"
)

// 真实游戏录包(testdata/openradar,MIT,见那里的 README)过整条解析链:
// photon 层 → protocol.Parser。合成数据测不出"线上类型和我们以为的不一样"这类问题——
// 角色 id 是 16 字节 Guid 而不是字符串,就是这么查出来的

func replayInto(t *testing.T, p *Parser, name string) capture.ReplayStats {
	t.Helper()
	h := capture.Handler{
		OnRequest:  p.HandleRequest,
		OnResponse: func(c byte, _ int16, _ string, params map[byte]any) { p.HandleResponse(c, params) },
		OnEvent:    p.HandleEvent,
	}
	st, err := capture.ReadFile(context.Background(), filepath.Join("testdata", "openradar", name), 0, h, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Fed == 0 {
		t.Fatalf("%s 一个包都没交给解析器: %+v", name, st)
	}
	return st
}

var dotnetGUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// 进图完成的 Join 响应:区域在 key8,角色名在 key2,角色 id 在 key1 而且是 16 字节 Guid。
// 这段录包里有两次进图,先 0201 后 0000
func TestRealCapture_Join解出位置和角色(t *testing.T) {
	p := New()
	var ids, names, locs []string
	p.OnIdentity = func(i, n, l string) { ids, names, locs = append(ids, i), append(names, n), append(locs, l) }
	replayInto(t, p, "join-finished.pcap")
	if !slices.Equal(locs, []string{"0201", "0000"}) || names[0] != "XXXXX" {
		t.Fatalf("位置 %v、角色名 %v", locs, names)
	}
	if !dotnetGUID.MatchString(ids[0]) || ids[0] != ids[1] {
		t.Fatalf("角色 id 应该是同一个 Guid 字符串,得到 %v", ids)
	}
}

// 切区走 op 41 的响应,key0 是目的地
func TestRealCapture_ChangeCluster更新位置(t *testing.T) {
	p := New()
	replayInto(t, p, "change-cluster.pcap")
	if p.Location() != "0000" {
		t.Fatalf("位置 %q", p.Location())
	}
}

// 一段走动 + 切区 + 进图:位置跟着变,Move 事件(没有 252)和 Move 请求都记了样本
func TestRealCapture_走动切区进图(t *testing.T) {
	p := New()
	var samples []model.CaptureSample
	p.OnSample = func(s model.CaptureSample) { samples = append(samples, s) }
	var locs []string
	p.OnIdentity = func(_, _, l string) { locs = append(locs, l) }
	st := replayInto(t, p, "move_map_change.pcap")
	if st.Packets != 584 {
		t.Fatalf("包数 %d", st.Packets)
	}
	if len(locs) < 2 || p.Location() == "" {
		t.Fatalf("位置变化 %v,最后在 %q", locs, p.Location())
	}
	n := map[string]int{}
	for _, s := range samples {
		n[s.Kind+"/"+strconv.Itoa(s.Code)]++
	}
	for _, k := range []string{"event/3", "op_request/22", "op_response/2", "op_response/41"} {
		if n[k] == 0 {
			t.Errorf("没记到 %s 的样本:%v", k, n)
		}
	}
	if n["event/3"] > MaxSamplesPerCode {
		t.Errorf("Move 样本超过上限: %d", n["event/3"])
	}
}

// .NET new Guid(byte[]) 的布局:前三段小端
func TestGuidParam(t *testing.T) {
	b := []byte{0x33, 0x22, 0x11, 0x00, 0x55, 0x44, 0x77, 0x66, 0x88, 0x99, 0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF}
	if got := guidParam(b); got != "00112233-4455-6677-8899-aabbccddeeff" {
		t.Fatalf("得到 %q", got)
	}
	if guidParam("已经是字符串") != "已经是字符串" || guidParam([]byte{1, 2}) != "" || guidParam(42) != "" {
		t.Fatal("非 16 字节的、不是字节也不是字符串的应该返回空")
	}
}
