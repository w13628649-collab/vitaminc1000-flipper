package model

import "testing"

func TestQuoteKey_String(t *testing.T) {
	k := QuoteKey{ItemID: "T5_METALBAR", LocationID: "Martlock", Quality: 1, Side: SideRequest}
	if got := k.String(); got != "T5_METALBAR|Martlock|1|1" {
		t.Fatalf("得到 %q", got)
	}
}

func TestQuoteKey_附魔物品ID里的at符号不影响拼key(t *testing.T) {
	k := QuoteKey{ItemID: "T5_ARMOR_CLOTH_SET1@2", LocationID: "Fort Sterling", Quality: 3, Side: SideOffer}
	if got := k.String(); got != "T5_ARMOR_CLOTH_SET1@2|Fort Sterling|3|0" {
		t.Fatalf("得到 %q", got)
	}
}

func TestMarketOrder_Key只取盘口维度(t *testing.T) {
	// 价格和数量不进 key:同一个盘口上的所有挂单共享一个 key,
	// 这样 conflation 才能把它们合并成一次重算
	a := MarketOrder{ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1,
		Side: SideOffer, UnitPrice: 1000, Amount: 5}
	b := a
	b.UnitPrice, b.Amount = 2000, 99
	if a.Key() != b.Key() {
		t.Fatal("同一盘口的不同挂单应该得到相同的 key")
	}
}

func TestPriceScale(t *testing.T) {
	// 协议里货币是 ×10000 的定点整数,客户端负责还原
	if PriceScale != 10000 {
		t.Fatalf("PriceScale 应为 10000,得到 %d", PriceScale)
	}
}
