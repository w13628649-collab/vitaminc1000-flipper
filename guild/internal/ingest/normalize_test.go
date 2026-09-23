package ingest

import (
	"testing"
	"time"

	"albion-guild/internal/model"
)

var t0 = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

func batchAt(sentAt time.Time, observed ...time.Time) model.UploadBatch {
	b := model.UploadBatch{SentAt: sentAt}
	for i, at := range observed {
		b.Orders = append(b.Orders, model.MarketOrder{OrderID: int64(i + 1), ObservedAt: at})
	}
	return b
}

// 成员的 Windows 钟快几分钟很常见。不纠正的话 last_seen 在未来,
// 和服务端时刻比永远"最新",幽灵单判断和新鲜度一起失真
func TestNormalize_客户端钟快5分钟整批平移回来(t *testing.T) {
	b := batchAt(t0.Add(5*time.Minute), t0.Add(5*time.Minute-2*time.Second))
	skew, clamped := Normalize(&b, t0)
	if skew != -5*time.Minute {
		t.Fatalf("skew 应为 -5m,得到 %v", skew)
	}
	if clamped != 0 {
		t.Fatalf("纠偏后不在未来,不该钳位,得到 clamped=%d", clamped)
	}
	if got, want := b.Orders[0].ObservedAt, t0.Add(-2*time.Second); !got.Equal(want) {
		t.Fatalf("ObservedAt 应为 T−2s,得到 %v", got)
	}
}

// 断网攒下的批晚到时,SentAt 是真正发出的时刻,里面的单是十分钟前看到的。
// 不能因为"刚收到"就把它们刷新成新的
func TestNormalize_重传批保留原观测时间(t *testing.T) {
	b := batchAt(t0, t0.Add(-10*time.Minute))
	Normalize(&b, t0)
	if got, want := b.Orders[0].ObservedAt, t0.Add(-10*time.Minute); !got.Equal(want) {
		t.Fatalf("应仍是 T−10m,得到 %v", got)
	}
}

// 老客户端不带 sent_at,钟快了只能靠钳位兜住
func TestNormalize_没有SentAt时未来时间钳到now(t *testing.T) {
	b := batchAt(time.Time{}, t0.Add(time.Hour))
	skew, clamped := Normalize(&b, t0)
	if skew != 0 || clamped != 1 {
		t.Fatalf("应 skew=0 clamped=1,得到 skew=%v clamped=%d", skew, clamped)
	}
	if !b.Orders[0].ObservedAt.Equal(t0) {
		t.Fatalf("应钳成 T,得到 %v", b.Orders[0].ObservedAt)
	}
}

func TestNormalize_零值补成now(t *testing.T) {
	// 带 SentAt 的批里有零值,也只补成 now,不能再加一次 skew
	b := batchAt(t0.Add(5*time.Minute), time.Time{})
	_, clamped := Normalize(&b, t0)
	if !b.Orders[0].ObservedAt.Equal(t0) || clamped != 0 {
		t.Fatalf("零值应补成 T 且不计钳位,得到 %v clamped=%d", b.Orders[0].ObservedAt, clamped)
	}
}

func TestCanonicalItemID_附魔补后缀只作防御(t *testing.T) {
	cases := []struct {
		id      string
		enchant int16
		want    string
	}{
		{"T5_2H_FIRESTAFF", 2, "T5_2H_FIRESTAFF@2"},
		{"T6_METALBAR_LEVEL4@4", 4, "T6_METALBAR_LEVEL4@4"}, // 实抓样本本来就带,不能补成 @4@4
		{"T5_CLOTH", 0, "T5_CLOTH"},
	}
	for _, c := range cases {
		if got := canonicalItemID(c.id, c.enchant); got != c.want {
			t.Errorf("canonicalItemID(%q, %d) = %q,应为 %q", c.id, c.enchant, got, c.want)
		}
	}
}

func TestIdentOf_只看盘口四要素(t *testing.T) {
	a := model.MarketOrder{OrderID: 1, ItemID: "T5_CLOTH", LocationID: "Martlock", Quality: 1, Side: model.SideOffer,
		UnitPrice: 100, Amount: 5}
	b := a
	b.UnitPrice, b.Amount, b.ObservedAt = 200, 1, t0 // 价格、数量、时间变了不算换盘口
	if identOf(a) != identOf(b) {
		t.Fatal("价格/数量/时间不该影响身份指纹")
	}
	for name, mut := range map[string]func(*model.MarketOrder){
		"城市": func(o *model.MarketOrder) { o.LocationID = "Thetford" },
		"物品": func(o *model.MarketOrder) { o.ItemID = "T6_CLOTH" },
		"品质": func(o *model.MarketOrder) { o.Quality = 2 },
		"方向": func(o *model.MarketOrder) { o.Side = model.SideRequest },
	} {
		c := a
		mut(&c)
		if identOf(a) == identOf(c) {
			t.Errorf("%s变了,身份指纹应该不同", name)
		}
	}
}
