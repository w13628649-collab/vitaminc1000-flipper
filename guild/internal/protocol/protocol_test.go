package protocol

import (
	"testing"
	"time"

	"albion-guild/internal/model"
)

// UnitPriceSilver 在包里是 ×10000 的定点数,11200000 就是 1120 银
const offerJSON = `{"Id":123,"ItemTypeId":"T5_METALBAR","LocationId":"",` +
	`"QualityLevel":1,"EnchantmentLevel":0,"UnitPriceSilver":11200000,"Amount":50,` +
	`"AuctionType":"offer","Expires":"2026-09-30T00:00:00"}`

const requestJSON = `{"Id":124,"ItemTypeId":"T5_METALBAR","LocationId":"",` +
	`"QualityLevel":1,"EnchantmentLevel":0,"UnitPriceSilver":11000000,"Amount":30,` +
	`"AuctionType":"request","Expires":"2026-09-30T00:00:00"}`

func collect(t *testing.T, p *Parser) *[]model.MarketOrder {
	t.Helper()
	var got []model.MarketOrder
	p.OnOrders = func(o []model.MarketOrder) { got = append(got, o...) }
	return &got
}

func TestHandleResponse_解析挂单(t *testing.T) {
	p := New()
	got := collect(t, p)
	p.HandleResponse(1, map[byte]any{8: "Martlock", 1: "guid", 2: "Tessaria",
		paramOperationCode: byte(p.Codes.Join)})
	p.HandleResponse(1, map[byte]any{
		paramOperationCode: byte(p.Codes.AuctionGetOffers),
		0:                  []string{offerJSON, requestJSON},
	})

	if len(*got) != 2 {
		t.Fatalf("应解析出 2 张挂单,得到 %d", len(*got))
	}
	o := (*got)[0]
	if o.OrderID != 123 || o.ItemID != "T5_METALBAR" || o.UnitPrice != 1120 || o.Amount != 50 {
		t.Fatalf("字段不对: %+v", o)
	}
	if o.Side != model.SideOffer || (*got)[1].Side != model.SideRequest {
		t.Fatal("AuctionType 没映射成正确的方向")
	}
}

// 真实抓包的一条挂单原文。游戏挂单界面显示 329,997 银,包里是 3,299,970,000。
// 以前按 albiondata-client 的行为不除(它把原文传给 AODP、由 AODP 服务端除),
// 抓包价就比 AODP 高一万倍
func TestHandleResponse_挂单价从定点数还原成银币(t *testing.T) {
	const real = `{"Id":17316457311,"ItemTypeId":"T6_METALBAR_LEVEL4@4","LocationId":"",` +
		`"QualityLevel":1,"EnchantmentLevel":4,"UnitPriceSilver":3299970000,"Amount":36,` +
		`"AuctionType":"offer","Expires":"2026-10-21T08:00:00"}`
	p := New()
	got := collect(t, p)
	p.HandleResponse(1, map[byte]any{paramOperationCode: byte(p.Codes.Join), 8: "0007"})
	p.HandleResponse(1, map[byte]any{
		paramOperationCode: byte(p.Codes.AuctionGetOffers), 0: []string{real}})

	if len(*got) != 1 {
		t.Fatalf("应解析出 1 张挂单,得到 %d", len(*got))
	}
	if o := (*got)[0]; o.UnitPrice != 329997 || o.Amount != 36 {
		t.Fatalf("应该是 329,997 银 × 36 件,得到 %d × %d", o.UnitPrice, o.Amount)
	}
}

func TestHandleResponse_城市挂单用当前所在地补位置(t *testing.T) {
	// 城市挂单的 LocationId 在包里是空的,得靠 opJoin 记下来的位置补。
	// 这也是多开时城市会串的根源。
	p := New()
	got := collect(t, p)
	p.HandleResponse(1, map[byte]any{paramOperationCode: byte(p.Codes.Join), 8: "Lymhurst"})
	p.HandleResponse(1, map[byte]any{
		paramOperationCode: byte(p.Codes.AuctionGetOffers), 0: []string{offerJSON}})

	if len(*got) != 1 || (*got)[0].LocationID != "Lymhurst" {
		t.Fatalf("位置没补上: %+v", *got)
	}
}

func TestHandleResponse_还不知道位置就先扣着(t *testing.T) {
	// 位置不明时入库会污染数据,不能发;但也别丢:用户确认位置后补发
	p := New()
	got := collect(t, p)
	p.HandleResponse(1, map[byte]any{
		paramOperationCode: byte(p.Codes.AuctionGetOffers), 0: []string{offerJSON, requestJSON}})
	if len(*got) != 0 || p.HeldOrders() != 2 {
		t.Fatalf("不知道位置时不该产出挂单、要扣着:产出 %d 条、扣着 %d 条", len(*got), p.HeldOrders())
	}
}

// 客户端开在游戏中途(2026-10-26 实况:重开客户端后一直在市场里点,一条都没传):
// 用户确认「我就在 X」,扣着的挂单补上位置发出去,之后翻到的照常带这个位置
func TestAssumeLocation_确认位置后补发扣着的挂单(t *testing.T) {
	p := New()
	got := collect(t, p)
	var who string
	p.OnIdentity = func(_, _, l string) { who = l }
	p.HandleResponse(1, map[byte]any{
		paramOperationCode: byte(p.Codes.AuctionGetOffers), 0: []string{offerJSON, requestJSON}})
	n, ok := p.AssumeLocation("1002")
	if !ok || n != 2 || len(*got) != 2 || (*got)[0].LocationID != "1002" || (*got)[1].LocationID != "1002" {
		t.Fatalf("要补发 2 条、位置是 1002:ok=%v n=%d %+v", ok, n, *got)
	}
	if p.HeldOrders() != 0 || p.Location() != "1002" || who != "1002" {
		t.Fatalf("确认后不再扣着、位置就是它:扣着 %d、位置 %q、回调 %q", p.HeldOrders(), p.Location(), who)
	}
	p.HandleResponse(1, map[byte]any{
		paramOperationCode: byte(p.Codes.AuctionGetOffers), 0: []string{offerJSON}})
	if len(*got) != 3 || (*got)[2].LocationID != "1002" {
		t.Fatalf("之后翻到的照常带位置:%+v", *got)
	}
}

func TestAssumeLocation_抓到切区后不覆盖_扣着的扔掉(t *testing.T) {
	p := New()
	got := collect(t, p)
	p.HandleResponse(1, map[byte]any{
		paramOperationCode: byte(p.Codes.AuctionGetOffers), 0: []string{offerJSON}})
	// 切区:扣着的那条是切区之前、不知道在哪翻到的,不能算到新区头上
	p.HandleRequest(1, map[byte]any{
		paramOperationCode: byte(p.Codes.GetGameServerByCluster), 0: "1000"})
	if p.HeldOrders() != 0 || len(*got) != 0 {
		t.Fatalf("切区后扣着的要扔掉:扣着 %d、产出 %d", p.HeldOrders(), len(*got))
	}
	if _, ok := p.AssumeLocation("1002"); ok || p.Location() != "1000" {
		t.Fatalf("位置已经抓到了,界面确认不该覆盖:%q", p.Location())
	}
}

func TestHandleResponse_自带位置的不被覆盖(t *testing.T) {
	// 走私贩巢穴/休息区的挂单自带 LocationId(形如 xxxx@yyyy)
	const smuggler = `{"Id":9,"ItemTypeId":"T5_CLOTH","LocationId":"3005@0",` +
		`"QualityLevel":1,"UnitPriceSilver":900,"Amount":3,"AuctionType":"offer"}`
	p := New()
	got := collect(t, p)
	p.HandleResponse(1, map[byte]any{paramOperationCode: byte(p.Codes.Join), 8: "Martlock"})
	p.HandleResponse(1, map[byte]any{
		paramOperationCode: byte(p.Codes.AuctionGetOffers), 0: []string{smuggler}})

	if (*got)[0].LocationID != "3005@0" {
		t.Fatalf("自带的位置被覆盖了: %q", (*got)[0].LocationID)
	}
}

func TestHandleRequest_切区更新位置(t *testing.T) {
	p := New()
	var loc string
	p.OnIdentity = func(_, _, l string) { loc = l }
	p.HandleRequest(1, map[byte]any{
		paramOperationCode: byte(p.Codes.GetGameServerByCluster), 0: "Thetford"})
	if loc != "Thetford" || p.Location() != "Thetford" {
		t.Fatalf("切区没更新位置,得到 %q", p.Location())
	}
}

func TestIntParam_兼容多种整数类型(t *testing.T) {
	// Photon 反序列化出来的整数宽度不固定,取决于实际数值大小
	for _, v := range []any{byte(81), int16(81), int32(81), int64(81), int(81)} {
		got, ok := intParam(map[byte]any{paramOperationCode: v}, paramOperationCode)
		if !ok || got != 81 {
			t.Fatalf("%T 没认出来", v)
		}
	}
	// 超过 255 的码原样返回,不能被截窄
	for _, v := range []any{int16(555), int32(555), int64(555), int(555)} {
		if got, ok := intParam(map[byte]any{paramOperationCode: v}, paramOperationCode); !ok || got != 555 {
			t.Fatalf("%T(555) 得到 %d", v, got)
		}
	}
	if _, ok := intParam(map[byte]any{}, paramOperationCode); ok {
		t.Fatal("缺失的参数应该返回 false")
	}
}

// 以前操作码截成 byte:258(LaborerTakeJobLoot)& 0xFF = 2 = Join,
// 它的参数被当成角色 id / 角色名 / 所在地,把位置改写成垃圾,
// 之后翻到的城市挂单全记到错的地方
func TestHandleResponse_操作码258不能被截成Join(t *testing.T) {
	p := New()
	var called bool
	p.OnIdentity = func(_, _, _ string) { called = true }
	p.HandleResponse(1, map[byte]any{paramOperationCode: int16(258),
		1: "垃圾", 2: "垃圾", 8: "垃圾"})
	if called || p.Location() != "" {
		t.Fatalf("258 被当成了 Join:位置 %q", p.Location())
	}
}

// 337 & 0xFF = 81 = AuctionGetOffers。截窄的话它的 params[0] 会被当挂单 JSON 解
func TestHandleResponse_操作码337不能被当成挂单(t *testing.T) {
	p := New()
	got := collect(t, p)
	p.HandleResponse(1, map[byte]any{paramOperationCode: byte(p.Codes.Join), 8: "Martlock"})
	p.HandleResponse(1, map[byte]any{paramOperationCode: int16(337),
		0: []string{offerJSON}})
	if len(*got) != 0 {
		t.Fatalf("337 被当成了挂单,产出 %d 条", len(*got))
	}
}

// 请求那条路一样:17 + 256 = 273 不能被当成切区
func TestHandleRequest_大于255的码不能被截成切区(t *testing.T) {
	p := New()
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(273), 0: "Thetford"})
	if p.Location() != "" {
		t.Fatalf("273 被当成了切区,位置变成 %q", p.Location())
	}
}

func TestStringSlice_兼容两种数组形态(t *testing.T) {
	if got, ok := stringSlice([]string{"a", "b"}); !ok || len(got) != 2 {
		t.Fatal("[]string 没认出来")
	}
	if got, ok := stringSlice([]any{"a", "b"}); !ok || len(got) != 2 {
		t.Fatal("[]any 没认出来")
	}
	if _, ok := stringSlice(42); ok {
		t.Fatal("非数组不该通过")
	}
}

// 市场挂单走的是一条没有参数表的路,这条路以前是死的。
//
// photon.dispatchResponse 命中 []string 时,把挂单数组包成 params[0]
// 就直接回调,**里面没有 253**;而 HandleResponse 第一件事就是取 253,
// 取不到直接 return。handleOrders 读的恰恰就是 params[0] ——
// 也就是说唯一能拿到挂单的那条路被自己堵死了。
// 症状是"日志一切正常、库里一条挂单都没有",很难从现象反推。
func TestHandleResponse_没有参数表时也要认挂单(t *testing.T) {
	p := New()
	p.HandleResponse(1, map[byte]any{8: "Martlock", 1: "guid", 2: "Tessaria",
		253: byte(2)}) // 先 Join 一下,否则城市为空挂单会被丢掉

	var got []model.MarketOrder
	p.OnOrders = func(o []model.MarketOrder) { got = append(got, o...) }

	// photon 层的 []string 分支产出的就是这个形状:只有 params[0]
	p.HandleResponse(byte(p.Codes.AuctionGetOffers), map[byte]any{
		0: []string{
			`{"Id":11,"ItemTypeId":"T5_CLOTH","QualityLevel":1,"EnchantmentLevel":0,` +
				`"UnitPriceSilver":12340000,"Amount":7,"AuctionType":"offer","LocationId":""}`,
			`{"Id":12,"ItemTypeId":"T5_CLOTH","QualityLevel":1,"EnchantmentLevel":0,` +
				`"UnitPriceSilver":11110000,"Amount":3,"AuctionType":"request","LocationId":""}`,
		},
	})

	if len(got) != 2 {
		t.Fatalf("应该解出 2 条挂单,得到 %d 条", len(got))
	}
	if got[0].Side != model.SideOffer || got[1].Side != model.SideRequest {
		t.Fatalf("方向该由 JSON 里的 AuctionType 判:%v / %v", got[0].Side, got[1].Side)
	}
	if got[0].LocationID != "Martlock" {
		t.Fatalf("空 LocationId 该用当前所在地补,得到 %q", got[0].LocationID)
	}
}

// 操作码在参数表里时照旧走参数表,别因为加了兜底就把正常路径改坏。
func TestHandleResponse_有参数表时仍按253路由(t *testing.T) {
	p := New()
	var called bool
	p.OnIdentity = func(_, _, _ string) { called = true }
	// 253 说这是 Join,即使 photonOp 传的是别的东西
	p.HandleResponse(99, map[byte]any{253: p.Codes.Join, 1: "guid", 2: "Tessaria", 8: "Lymhurst"})
	if !called {
		t.Fatal("有 253 时应该按它路由到 Join")
	}
}

// 离线回放时挂单的观测时间是包上的时间,不是回放那一刻
func TestSetClock_挂单用解析器的时钟(t *testing.T) {
	p := New()
	got := collect(t, p)
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	p.SetClock(func() time.Time { return at })
	p.HandleResponse(1, map[byte]any{8: "Martlock", 1: "guid", 2: "Tessaria",
		paramOperationCode: byte(p.Codes.Join)})
	p.HandleResponse(1, map[byte]any{0: []string{offerJSON}})
	if len(*got) != 1 || !(*got)[0].ObservedAt.Equal(at) {
		t.Fatalf("观测时间应该是 %v,得到 %+v", at, *got)
	}
}
