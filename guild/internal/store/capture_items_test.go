package store

import (
	"context"
	"testing"
	"time"

	"albion-guild/internal/model"
)

func TestCapturedItems(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	sd := &seeder{t: t, st: st}
	key := func(item, city string, q int16, side model.Side) model.QuoteKey {
		return model.QuoteKey{ItemID: item, LocationID: city, Quality: q, Side: side}
	}

	// 两座城、两边加起来 60 件
	sd.put(key("T7_LEATHER", "Martlock", 1, model.SideOffer), fx{5000, 40, T}, fx{5010, 5, T.Add(-time.Hour)})
	sd.put(key("T7_LEATHER", "Thetford", 1, model.SideRequest), fx{4000, 15, T.Add(-2 * time.Hour)})
	// 件数少但也要列出来
	sd.put(key("T8_CLOTH", "Lymhurst", 1, model.SideOffer), fx{9000, 3, T})
	// 同件数按 id 排
	sd.put(key("T4_STONEBLOCK", "Bridgewatch", 1, model.SideOffer), fx{50, 3, T})
	// 下面这些都不该出现:黑市/没收敛的地点、品质不在范围里、窗口外、0 件、0 价(解析出错)
	sd.put(key("T6_BLACKMARKET", "3003", 1, model.SideRequest), fx{1000, 999, T})
	sd.put(key("T6_Q2", "Martlock", 2, model.SideOffer), fx{1000, 999, T})
	sd.put(key("T6_OLD", "Martlock", 1, model.SideOffer), fx{1000, 999, T.Add(-7 * time.Hour)})
	sd.put(key("T6_EMPTY", "Martlock", 1, model.SideOffer), fx{1000, 0, T})
	sd.put(key("T6_ZERO", "Martlock", 1, model.SideOffer), fx{0, 999, T})
	// T7_LEATHER 在范围外那一张不算进件数
	sd.put(key("T7_LEATHER", "Brecilien", 1, model.SideOffer), fx{5000, 1000, T})

	got, err := st.CapturedItems(ctx,
		[]string{"Martlock", "Thetford", "Lymhurst", "Bridgewatch"}, []int{1}, T.Add(-6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		id     string
		qty    int64
		orders int
	}
	want := []row{{"T7_LEATHER", 60, 3}, {"T4_STONEBLOCK", 3, 1}, {"T8_CLOTH", 3, 1}}
	if len(got) != len(want) {
		t.Fatalf("应为 %v,得到 %+v", want, got)
	}
	for i, w := range want {
		g := got[i]
		if g.ItemID != w.id || g.Qty != w.qty || g.Orders != w.orders {
			t.Fatalf("第 %d 行应为 %+v,得到 %+v", i, w, g)
		}
	}
	if !got[0].LastSeen.Equal(T) {
		t.Fatalf("LastSeen 应为最近那张单的时刻 T,得到 %v", got[0].LastSeen)
	}

	if none, err := st.CapturedItems(ctx, nil, []int{1}, T.Add(-6*time.Hour)); err != nil || len(none) != 0 {
		t.Fatalf("没给城市时应返回空,得到 %v / %v", none, err)
	}
}
