package protocol

import (
	"maps"
	"slices"
	"strings"
	"testing"
)

// 默认码下白名单的完整内容。改表时这里必须一起改——这就是审计点:
// 多收一个键、多放一个码,都得在这张对照里留下痕迹
func TestAllowlist_默认码下的完整内容(t *testing.T) {
	want := map[ruleKey][]byte{
		{KindResponse, 81}:  {0},
		{KindResponse, 82}:  {0},
		{KindResponse, 2}:   {1, 2, 8},
		{KindResponse, 197}: {17, 18, 20, 22, 23, 24, 25, 31, 33, 34},
		{KindRequest, 17}:   {0},
		{KindRequest, 197}:  {0},
		{KindRequest, 81}:   nil,
		{KindRequest, 82}:   nil,
		{KindEvent, 150}:    nil,
		{KindEvent, 152}:    nil,
		{KindEvent, 153}:    nil,
	}
	got := map[ruleKey][]byte{}
	for _, r := range Allowlist(DefaultOpCodes(), DefaultEventCodes()) {
		k := ruleKey{r.Kind, r.Code}
		if _, dup := got[k]; dup {
			t.Fatalf("(%v, %d) 在表里出现了两次", r.Kind, r.Code)
		}
		got[k] = r.Keys
		if r.Why == "" {
			t.Errorf("(%v, %d) 没写为什么能收", r.Kind, r.Code)
		}
	}
	if !maps.EqualFunc(got, want, func(a, b []byte) bool { return slices.Equal(a, b) }) {
		t.Fatalf("白名单变了\n得到 %v\n想要 %v", got, want)
	}
}

// 抓包只解操作响应,一条事件都不解。唯一的例外是用户 2026-10-17 批准的三条专精事件,
// 而且只记样本:业务层一个键都拿不到
func TestAllowlist_事件只有专精三条例外且只记样本(t *testing.T) {
	var events []int
	sampled := map[ruleKey]bool{}
	for _, r := range Allowlist(DefaultOpCodes(), DefaultEventCodes()) {
		if r.Sample {
			sampled[ruleKey{r.Kind, r.Code}] = true
		}
		if r.Kind != KindEvent {
			continue
		}
		events = append(events, r.Code)
		if len(r.Keys) != 0 {
			t.Errorf("事件 %d 给了业务层键 %v", r.Code, r.Keys)
		}
		if !r.Sample || !strings.Contains(r.Why, "2026-10-17") {
			t.Errorf("事件 %d 只能作为批准过的样本例外存在", r.Code)
		}
	}
	slices.Sort(events)
	if !slices.Equal(events, []int{150, 152, 153}) {
		t.Fatalf("事件例外应该只有 150/152/153,得到 %v", events)
	}
	// 记样本的只有 197 响应(只记 Keys 里的键)和这三条事件
	want := map[ruleKey]bool{{KindResponse, 197}: true, {KindEvent, 150}: true,
		{KindEvent, 152}: true, {KindEvent, 153}: true}
	if !maps.Equal(sampled, want) {
		t.Fatalf("样本范围变了: %v", sampled)
	}
	// 全键样本("全抓")只有用户批准过的三条专精事件。197 的全键样本没有批准
	allKeys := map[ruleKey]bool{}
	for _, r := range Allowlist(DefaultOpCodes(), DefaultEventCodes()) {
		if r.SampleAllKeys {
			allKeys[ruleKey{r.Kind, r.Code}] = true
			if !r.Sample || r.Kind != KindEvent || !strings.Contains(r.Why, "批准") {
				t.Errorf("(%v, %d) 开了全键样本但不是批准过的事件例外", r.Kind, r.Code)
			}
		}
	}
	if !maps.Equal(allKeys, map[ruleKey]bool{{KindEvent, 150}: true, {KindEvent, 152}: true, {KindEvent, 153}: true}) {
		t.Fatalf("全键样本范围变了: %v", allKeys)
	}
}

// 服务端校验上传样本用的查询:只认允许记样本的 (方向, 码),非全键的只认 Keys 里的键
func TestSampleRule_服务端按白名单校验(t *testing.T) {
	op, ev := DefaultOpCodes(), DefaultEventCodes()
	r, ok := SampleRule(op, ev, "op_response", 197)
	if !ok || !r.SampleKeyAllowed(34) || r.SampleKeyAllowed(99) || r.SampleKeyAllowed(253) {
		t.Fatalf("197 样本只认白名单键: ok=%v", ok)
	}
	r, ok = SampleRule(op, ev, "event", 153)
	if !ok || !r.SampleKeyAllowed(252) || !r.SampleKeyAllowed(7) {
		t.Fatal("专精事件是全键样本")
	}
	for _, c := range []struct {
		kind string
		code int
	}{{"event", 29}, {"event", 3}, {"op_response", 81}, {"op_request", 197}, {"随便写", 197}, {"event", 197}} {
		if _, ok := SampleRule(op, ev, c.kind, c.code); ok {
			t.Errorf("(%s, %d) 不该允许记样本", c.kind, c.code)
		}
	}
}

// 没有 253 的响应不能退回用 Photon 层那个码查白名单:那是进 Join / 197 的侧门
func TestHandleResponse_没有253不走Photon层码(t *testing.T) {
	p, got, _ := newMapParser(t)
	var identity bool
	p.OnIdentity = func(_, _, _ string) { identity = true }
	p.HandleResponse(2, map[byte]any{1: "guid", 2: "名字", 8: "Martlock"})
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(197), 0: "0000"})
	resp := clusterMapResponse()
	delete(resp, paramOperationCode)
	p.HandleResponse(197, resp)
	if identity || p.Location() != "" || len(*got) != 0 {
		t.Fatalf("侧门开着:身份 %v、位置 %q、建筑 %d", identity, p.Location(), len(*got))
	}
}

// 在表里的码,表外的键在进业务层之前剥掉
func TestRuleFilter_表外的键剥掉(t *testing.T) {
	var join Rule
	for _, r := range Allowlist(DefaultOpCodes(), DefaultEventCodes()) {
		if r.Kind == KindResponse && r.Code == 2 {
			join = r
		}
	}
	in := map[byte]any{paramOperationCode: int16(2), 0: "x", 1: "guid", 2: "名字", 8: "0007", 9: "坐标", 200: 1}
	out := join.filter(in)
	if !slices.Equal(slices.Sorted(maps.Keys(out)), []byte{1, 2, 8}) {
		t.Fatalf("Join 只该留 1/2/8,得到 %v", out)
	}
	// 拷出来的是新 map:业务层改它不会回写到原包
	out[1] = "改了"
	if in[1] != "guid" {
		t.Fatal("filter 返回的不是新 map")
	}
	// 只要"发生了"的那几行,一个键都不给
	req := Rule{Kind: KindRequest, Code: 81}
	if got := req.filter(in); len(got) != 0 {
		t.Fatalf("无键的行应该给空 map,得到 %v", got)
	}
}

// 不在表里的码,业务层拿不到任何参数:换一个码带上 Join 的键,不能改身份
func TestHandleResponse_不在白名单的码一个键都拿不到(t *testing.T) {
	p := New()
	var called bool
	p.OnIdentity = func(_, _, _ string) { called = true }
	got := collect(t, p)
	for _, code := range []any{byte(3), int16(300), int32(555), int64(1000)} {
		p.HandleResponse(1, map[byte]any{paramOperationCode: code,
			0: []string{offerJSON}, 1: "guid", 2: "名字", 8: "Martlock"})
	}
	if called || len(*got) != 0 || p.Location() != "" {
		t.Fatalf("不在白名单的码影响了业务层:身份回调 %v、挂单 %d 条、位置 %q",
			called, len(*got), p.Location())
	}
}

// 码表可以热换(将来由服务端下发):索引要跟着重建,不能沿用旧码
func TestAllowlist_换码表后索引跟着变(t *testing.T) {
	p := New()
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(17), 0: "0007"})
	if p.Location() != "0007" {
		t.Fatalf("前提:17 是切区,位置 %q", p.Location())
	}
	p.Codes.GetGameServerByCluster = 18
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(17), 0: "4002"})
	if p.Location() != "0007" {
		t.Fatalf("换码之后 17 不该再是切区,位置变成了 %q", p.Location())
	}
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(18), 0: "4002"})
	if p.Location() != "4002" {
		t.Fatalf("新码 18 应该是切区,位置 %q", p.Location())
	}
}
