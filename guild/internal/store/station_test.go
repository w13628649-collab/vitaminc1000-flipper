package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"albion-guild/internal/model"
)

func TestStations存进去每个建筑取最新一条(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	t0 := time.Now().Add(-time.Hour).Truncate(time.Microsecond)

	i64 := func(v int64) *int64 { return &v }
	f64 := func(v float64) *float64 { return &v }
	old := model.StationObs{ZoneID: "0000", Index: 0, BuildingType: 501, Pos: []float64{10.5, -3},
		PublicFeeRaw: i64(300), PublicFee: f64(300), ObservedAt: t0}
	newer := old
	newer.Index = 3 // 下标每次响应都可能不同,连接键靠类型 + 坐标
	newer.PublicFeeRaw, newer.PublicFee = i64(5_000_000), f64(500)
	newer.Durability = i64(0)
	newer.Key23 = json.RawMessage(`{"t":"int64","v":7}`)
	newer.ObservedAt = t0.Add(30 * time.Minute)
	other := model.StationObs{ZoneID: "1205", Index: 0, BuildingType: 7, ObservedAt: t0}

	if err := st.SaveStations(ctx, "甲", []model.StationObs{old, other}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveStations(ctx, "乙", []model.StationObs{newer}); err != nil {
		t.Fatal(err)
	}

	got, err := st.StationFees(ctx, t0.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("两个建筑各一条,得到 %d 条: %+v", len(got), got)
	}
	var th, wild StationFee
	for _, g := range got {
		if g.ZoneID == "0000" {
			th = g
		} else {
			wild = g
		}
	}
	if th.City != "Thetford" || th.Reporter != "乙" || th.Index != 3 || *th.PublicFeeRaw != 5_000_000 ||
		*th.PublicFee != 500 || th.BuildingKey != "501@10.5,-3" || !th.ObservedAt.Equal(newer.ObservedAt) {
		t.Fatalf("Thetford 那台应是最新一条: %+v", th)
	}
	// 观测到的 0 读回来还是 0,没观测到的是 nil
	if th.Durability == nil || *th.Durability != 0 || th.Nutrition != nil || th.AssociateFee != nil {
		t.Fatalf("NULL 和 0 混了: %+v", th)
	}
	var tagged map[string]any
	if err := json.Unmarshal(th.Key23, &tagged); err != nil || tagged["t"] != "int64" {
		t.Fatalf("key23 原样读回: %s", th.Key23)
	}
	if wild.City != "1205" || wild.Pos != nil || wild.BuildingKey != "7#0" {
		t.Fatalf("不是城区的按原始 id 存: %+v", wild)
	}

	// since 之后没有观测的不返回
	got, err = st.StationFees(ctx, t0.Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ZoneID != "0000" {
		t.Fatalf("得到 %+v", got)
	}
}
