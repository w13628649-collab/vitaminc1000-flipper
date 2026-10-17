package main

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
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

// 服务端回 4xx/5xx 不算上传成功:退回队列、记错误。以前 400 也算成功,
// 整批丢掉、lastErr 为空、顶栏显示已上传
func TestFlush_非2xx退回队列(t *testing.T) {
	for _, code := range []int{http.StatusBadRequest, http.StatusRequestEntityTooLarge,
		http.StatusInternalServerError, http.StatusUnauthorized} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "不要", code)
		}))
		u := &uploader{server: srv.URL, client: srv.Client()}
		u.add([]model.MarketOrder{{OrderID: 1}, {OrderID: 2}})
		u.addStations([]model.StationObs{{ZoneID: "0000"}})
		u.flush(context.Background())
		st := u.snapshot()
		if len(u.pending) != 2 || len(u.stations) != 1 || st.OrdersUploaded != 0 ||
			st.StationsUploaded != 0 || st.ServerOK || !strings.Contains(st.LastError, "不要") {
			t.Errorf("%d:挂单 %d、建筑 %d、状态 %+v", code, len(u.pending), len(u.stations), st)
		}
		srv.Close()
	}
}

// 400/413 时拆小重发,直到能过;只剩一条还被拒就丢掉那条,不能永远堵在队列头
func TestFlush_被拒时拆小重发(t *testing.T) {
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b model.UploadBatch
		_ = json.NewDecoder(r.Body).Decode(&b)
		if len(b.Orders) > 3 {
			http.Error(w, "太大", http.StatusBadRequest)
			return
		}
		sizes = append(sizes, len(b.Orders))
		_, _ = w.Write([]byte(`{"changed":0}`))
	}))
	defer srv.Close()
	u := &uploader{server: srv.URL, client: srv.Client()}
	for i := range 10 {
		u.add([]model.MarketOrder{{OrderID: int64(i)}})
	}
	for range 10 { // 每次 tick 一次 flush
		u.flush(context.Background())
	}
	total := 0
	for _, n := range sizes {
		total += n
	}
	if total != 10 || len(u.pending) != 0 || u.snapshot().OrdersUploaded != 10 {
		t.Fatalf("应拆小后全部传完:各批 %v、剩 %d", sizes, len(u.pending))
	}

	// 单条也被拒:丢掉,后面的照常
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "坏的", http.StatusBadRequest)
	}))
	defer bad.Close()
	u2 := &uploader{server: bad.URL, client: bad.Client()}
	u2.add([]model.MarketOrder{{OrderID: 1}})
	u2.flush(context.Background())
	if len(u2.pending) != 0 || !strings.Contains(u2.snapshot().LastError, "单条被拒") {
		t.Fatalf("单条被拒应丢弃并报错:剩 %d、%q", len(u2.pending), u2.snapshot().LastError)
	}
}

// 积压按批发:每批不超过条数上限,一次 flush 连发几批补完
func TestFlush_积压按批发(t *testing.T) {
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b model.UploadBatch
		_ = json.NewDecoder(r.Body).Decode(&b)
		sizes = append(sizes, len(b.Orders))
	}))
	defer srv.Close()
	u := &uploader{server: srv.URL, client: srv.Client()}
	orders := make([]model.MarketOrder, maxBatchOrders*2+5)
	for i := range orders {
		orders[i].OrderID = int64(i)
	}
	u.add(orders)
	u.flush(context.Background())
	if len(sizes) != 3 || sizes[0] != maxBatchOrders || sizes[2] != 5 || len(u.pending) != 0 {
		t.Fatalf("各批 %v,剩 %d", sizes, len(u.pending))
	}
}

// 体积超过 maxBatchBytes 时把后一半退回队列,每个请求都不超限
func TestFlush_按字节封顶(t *testing.T) {
	var sizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) > maxBatchBytes {
			t.Errorf("请求体 %d 字节超过上限", len(body))
		}
		sizes = append(sizes, len(body))
	}))
	defer srv.Close()
	u := &uploader{server: srv.URL, client: srv.Client()}
	long := strings.Repeat("T8_LONG_ITEM_", 30) // 一条约 500 字节
	orders := make([]model.MarketOrder, 12_000)
	for i := range orders {
		orders[i] = model.MarketOrder{OrderID: int64(i), ItemID: long}
	}
	u.add(orders)
	u.flush(context.Background())
	if len(sizes) < 2 || len(u.pending) != 0 || u.snapshot().OrdersUploaded != int64(len(orders)) {
		t.Fatalf("应拆成多批传完:各批字节 %v、剩 %d", sizes, len(u.pending))
	}
}

// 序列化失败(比如坐标里混进 NaN)不能整批丢:摘掉建筑和样本,挂单照发。
// 以前 json.Marshal 报错就 return,队列已经清空,挂单一起没了
func TestFlush_序列化失败时挂单照发(t *testing.T) {
	var got model.UploadBatch
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	u := &uploader{server: srv.URL, client: srv.Client()}
	u.add([]model.MarketOrder{{OrderID: 1}, {OrderID: 2}})
	u.addStations([]model.StationObs{{ZoneID: "0000", Pos: []float64{math.NaN(), 1}}})
	u.flush(context.Background())
	if len(got.Orders) != 2 || len(got.Stations) != 0 || u.snapshot().OrdersUploaded != 2 {
		t.Fatalf("服务端收到挂单 %d、建筑 %d", len(got.Orders), len(got.Stations))
	}
}

// 建筑条数用服务端确认存下的:老服务端不回 stations 字段,不计
func TestFlush_建筑按服务端确认计数(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"changed":1,"touched":0}`))
	}))
	defer srv.Close()
	u := &uploader{server: srv.URL, client: srv.Client()}
	u.addStations([]model.StationObs{{ZoneID: "0000"}, {ZoneID: "0000", Index: 1}})
	u.flush(context.Background())
	if st := u.snapshot(); st.StationsUploaded != 0 || st.LastError != "" {
		t.Fatalf("服务端没确认就不该计: %+v", st)
	}
}

func TestTrimOldest_超上限丢最旧的(t *testing.T) {
	got := trimOldest([]int{1, 2, 3, 4, 5}, 3)
	if len(got) != 3 || got[0] != 3 {
		t.Fatalf("得到 %v", got)
	}
}
