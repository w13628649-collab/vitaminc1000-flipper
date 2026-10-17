package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"albion-guild/internal/model"
)

func TestSamples每个码只留最近200条(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	t0 := time.Now().Add(-time.Hour).Truncate(time.Microsecond)

	mk := func(kind string, code, i int) model.CaptureSample {
		return model.CaptureSample{Kind: kind, Code: code,
			Params:     json.RawMessage(`[{"k":252,"t":"int16","v":153}]`),
			ObservedAt: t0.Add(time.Duration(i) * time.Second), ClientVersion: "1.0"}
	}
	var batch []model.CaptureSample
	for i := range SamplesPerCode + 30 {
		batch = append(batch, mk(model.SampleEvent, 153, i))
	}
	batch = append(batch, mk(model.SampleOpResponse, 153, 0)) // 同码不同方向分开数
	if err := st.SaveSamples(ctx, "甲", batch); err != nil {
		t.Fatal(err)
	}

	got, err := st.Samples(ctx, model.SampleEvent, 153, SamplesPerCode)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != SamplesPerCode {
		t.Fatalf("应留 %d 条,得到 %d", SamplesPerCode, len(got))
	}
	// 留下的是最近的:最新一条是第 229 秒,最旧的是第 30 秒
	if !got[0].ObservedAt.Equal(t0.Add(229*time.Second)) || !got[len(got)-1].ObservedAt.Equal(t0.Add(30*time.Second)) {
		t.Fatalf("留错了:最新 %v 最旧 %v", got[0].ObservedAt, got[len(got)-1].ObservedAt)
	}
	if got[0].Reporter != "甲" || got[0].ClientVersion != "1.0" {
		t.Fatalf("字段: %+v", got[0])
	}
	var params []map[string]any
	if err := json.Unmarshal(got[0].Params, &params); err != nil || params[0]["t"] != "int16" {
		t.Fatalf("params 读回: %s", got[0].Params)
	}

	other, err := st.Samples(ctx, model.SampleOpResponse, -1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].Code != 153 {
		t.Fatalf("另一个方向不该被修剪掉: %+v", other)
	}

	// 坏 JSON 整批拒收
	bad := mk(model.SampleEvent, 150, 0)
	bad.Params = json.RawMessage(`{坏`)
	if err := st.SaveSamples(ctx, "甲", []model.CaptureSample{bad}); err == nil {
		t.Fatal("坏 JSON 应该报错")
	}
}
