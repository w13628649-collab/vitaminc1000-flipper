package protocol

import (
	"maps"
	"slices"
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
	}
	got := map[ruleKey][]byte{}
	for _, r := range Allowlist(DefaultOpCodes()) {
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

// 抓包只解操作响应,一条事件都不解
func TestAllowlist_没有一条事件(t *testing.T) {
	for _, r := range Allowlist(DefaultOpCodes()) {
		if r.Kind == KindEvent {
			t.Fatalf("事件 %d 进了白名单", r.Code)
		}
	}
}

// 在表里的码,表外的键在进业务层之前剥掉
func TestRuleFilter_表外的键剥掉(t *testing.T) {
	var join Rule
	for _, r := range Allowlist(DefaultOpCodes()) {
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
