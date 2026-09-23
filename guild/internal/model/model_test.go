package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 新旧客户端要能混跑:老客户端不带 sent_at/client_version,解码后必须是零值
// (服务端据此不纠偏);新客户端没填时也不能序列化出 0001-01-01 这种假时间
func TestUploadBatch_新字段对老客户端兼容(t *testing.T) {
	var b UploadBatch
	if err := json.Unmarshal([]byte(`{"reporter":"甲","orders":[]}`), &b); err != nil {
		t.Fatal(err)
	}
	if !b.SentAt.IsZero() || b.ClientVersion != "" {
		t.Fatalf("老格式解码后应为零值,得到 %+v", b)
	}
	raw, err := json.Marshal(UploadBatch{Reporter: "甲"})
	if err != nil {
		t.Fatal(err)
	}
	if s := string(raw); strings.Contains(s, "sent_at") || strings.Contains(s, "client_version") {
		t.Fatalf("零值不该序列化出来,得到 %s", s)
	}
	at := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	raw, _ = json.Marshal(UploadBatch{SentAt: at, ClientVersion: "1.2.0"})
	if s := string(raw); !strings.Contains(s, `"sent_at":"2026-09-23T12:00:00Z"`) || !strings.Contains(s, `"client_version":"1.2.0"`) {
		t.Fatalf("非零值要带上,得到 %s", s)
	}
}

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
