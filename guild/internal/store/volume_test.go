package store

import (
	"context"
	"reflect"
	"testing"
	"time"

	"albion-guild/internal/aodp"
)

// 精炼页的日成交量:只取请求的物品、那一座城、品质 1、日期范围内的;
// 同一个桶抓包和 AODP 各一条时只留抓包那条(同一份服务端成交,不能相加)
func TestDailyVolume只取请求的物品且同桶优先抓包(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	day := func(d int) time.Time { return time.Date(2026, 9, d, 0, 0, 0, 0, time.UTC) }
	series := func(item, city string, q int, pts ...any) aodp.HistorySeries {
		s := aodp.HistorySeries{ItemID: item, Location: city, Quality: q}
		for i := 0; i+1 < len(pts); i += 2 {
			s.Data = append(s.Data, aodp.HistoryPoint{Timestamp: aodp.Stamp{T: pts[i].(time.Time)},
				ItemCount: int64(pts[i+1].(int)), AvgPrice: 3_000_000})
		}
		return s
	}
	const bar = "T8_METALBAR_LEVEL4@4"
	if _, err := st.WriteHistory(ctx, []aodp.HistorySeries{
		series(bar, "Thetford", 1, day(9), 7, day(10), 10, day(11), 20, day(12), 30), // day(9) 在范围外
		series(bar, "Martlock", 1, day(10), 99),                                      // 别的城
		series(bar, "Thetford", 2, day(10), 88),                                      // 别的品质
		series("T7_METALBAR_LEVEL4@4", "Thetford", 1, day(10), 77),                   // 没请求的物品
	}, SourceAODP); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteHistory(ctx, []aodp.HistorySeries{
		series(bar, "Thetford", 1, day(11), 25), // 同桶,抓包胜
	}, SourceCapture); err != nil {
		t.Fatal(err)
	}

	got, err := st.DailyVolume(ctx, []string{bar, "T6_METALBAR_LEVEL4@4"}, "Thetford", 1, day(10), day(12))
	if err != nil {
		t.Fatal(err)
	}
	want := []ItemDay{
		{ItemID: bar, City: "Thetford", Day: day(10), ItemCount: 10, AvgPrice: 3_000_000, Source: SourceAODP},
		{ItemID: bar, City: "Thetford", Day: day(11), ItemCount: 25, AvgPrice: 3_000_000, Source: SourceCapture},
		{ItemID: bar, City: "Thetford", Day: day(12), ItemCount: 30, AvgPrice: 3_000_000, Source: SourceAODP},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("得到 %+v\n想要 %+v", got, want)
	}
	if none, err := st.DailyVolume(ctx, nil, "Thetford", 1, day(10), day(12)); err != nil || len(none) != 0 {
		t.Fatalf("不给物品应返回空: %v %v", none, err)
	}
}
