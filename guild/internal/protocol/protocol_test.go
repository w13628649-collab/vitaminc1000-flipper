package protocol

import (
	"testing"

	"albion-guild/internal/model"
)

const offerJSON = `{"Id":123,"ItemTypeId":"T5_METALBAR","LocationId":"",` +
	`"QualityLevel":1,"EnchantmentLevel":0,"UnitPriceSilver":1120,"Amount":50,` +
	`"AuctionType":"offer","Expires":"2026-09-30T00:00:00"}`

const requestJSON = `{"Id":124,"ItemTypeId":"T5_METALBAR","LocationId":"",` +
	`"QualityLevel":1,"EnchantmentLevel":0,"UnitPriceSilver":1100,"Amount":30,` +
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

func TestHandleResponse_还不知道位置就先丢掉(t *testing.T) {
	// 位置不明时入库会污染数据,宁可丢——用户切次区就恢复了
	p := New()
	got := collect(t, p)
	p.HandleResponse(1, map[byte]any{
		paramOperationCode: byte(p.Codes.AuctionGetOffers), 0: []string{offerJSON}})
	if len(*got) != 0 {
		t.Fatalf("不知道位置时不该产出挂单,得到 %d 条", len(*got))
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

func TestByteParam_兼容多种整数类型(t *testing.T) {
	// Photon 反序列化出来的整数宽度不固定,取决于实际数值大小
	for _, v := range []any{byte(81), int16(81), int32(81), int64(81), int(81)} {
		got, ok := byteParam(map[byte]any{paramOperationCode: v}, paramOperationCode)
		if !ok || got != 81 {
			t.Fatalf("%T 没认出来", v)
		}
	}
	if _, ok := byteParam(map[byte]any{}, paramOperationCode); ok {
		t.Fatal("缺失的参数应该返回 false")
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
