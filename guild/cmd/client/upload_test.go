package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"albion-guild/internal/model"
)

// 只开了地图没翻市场:只有建筑没有挂单也要发出去。以前挂单为空就直接 return
func TestFlush_只有建筑也要发(t *testing.T) {
	var got model.UploadBatch
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"changed":0,"touched":0,"stations":1}`))
	}))
	defer srv.Close()

	u := &uploader{server: srv.URL, client: srv.Client()}
	fee := int64(500)
	u.addStations([]model.StationObs{{ZoneID: "0000", BuildingType: 1, PublicFeeRaw: &fee,
		ObservedAt: time.Now()}})
	u.flush(context.Background())

	if len(got.Stations) != 1 || got.Stations[0].ZoneID != "0000" || *got.Stations[0].PublicFeeRaw != 500 {
		t.Fatalf("服务端收到 %+v", got)
	}
	if st := u.snapshot(); st.StationsUploaded != 1 || st.LastError != "" {
		t.Fatalf("状态 %+v", st)
	}
}

// 进游戏开专精界面只会产出样本:只有样本也要发
func TestFlush_只有样本也要发(t *testing.T) {
	var got model.UploadBatch
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	u := &uploader{server: srv.URL, client: srv.Client()}
	u.addSample(model.CaptureSample{Code: 153, Kind: model.SampleEvent, Params: json.RawMessage(`[]`)})
	u.flush(context.Background())
	if len(got.Samples) != 1 || got.Samples[0].Code != 153 || len(u.samples) != 0 {
		t.Fatalf("服务端收到 %+v,队列里还剩 %d", got, len(u.samples))
	}
}

// 发失败时建筑和挂单一起退回队列,下次一起补
func TestFlush_失败时建筑也退回队列(t *testing.T) {
	u := &uploader{server: "http://127.0.0.1:1", client: &http.Client{Timeout: time.Second}}
	u.add([]model.MarketOrder{{OrderID: 1}})
	u.addStations([]model.StationObs{{ZoneID: "0000"}})
	u.addSample(model.CaptureSample{Code: 150})
	u.flush(context.Background())
	if len(u.pending) != 1 || len(u.stations) != 1 || len(u.samples) != 1 || u.lastErr == "" {
		t.Fatalf("挂单 %d、建筑 %d、样本 %d、错误 %q", len(u.pending), len(u.stations), len(u.samples), u.lastErr)
	}
}

func TestTrimOldest_超上限丢最旧的(t *testing.T) {
	got := trimOldest([]int{1, 2, 3, 4, 5}, 3)
	if len(got) != 3 || got[0] != 3 {
		t.Fatalf("得到 %v", got)
	}
}
