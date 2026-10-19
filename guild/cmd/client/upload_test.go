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

// 建筑观测整次扫描一起走:积压丢旧的、分批发都不从一次扫描中间切开。
// 服务端拿「最新那次里没有」判站拆了,存下半次扫描会把另一半当成没了(审查 finding)
func TestStations_按整次扫描丢和分批(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	scan := func(zone string, at time.Time, n int) []model.StationObs {
		out := make([]model.StationObs, n)
		for i := range out {
			out[i] = model.StationObs{ZoneID: zone, Index: i, ObservedAt: at}
		}
		return out
	}
	// 积压超上限:丢掉最旧的整次扫描(Thetford 176 台),不是只丢它的前 27 台
	var st []model.StationObs
	st = append(st, scan("0007", t0, 176)...)
	for i := range 49 {
		st = append(st, scan("1000", t0.Add(time.Duration(i+1)*time.Minute), 99)...)
	}
	got := trimStations(st, maxPendingStations)
	for _, o := range got {
		if o.ZoneID == "0007" {
			t.Fatalf("超上限时 Thetford 那次要整次丢掉,还剩下标 %d", o.Index)
		}
	}
	if len(got) != 49*99 {
		t.Fatalf("剩 %d 条,想要 %d", len(got), 49*99)
	}
	// 切点落在一次扫描中间:退到这次扫描开头;一次扫描本身就比切点长:挪到它结尾
	two := append(scan("0007", t0, 5), scan("1000", t0.Add(time.Minute), 5)...)
	for n, want := range map[int]int{3: 5, 5: 5, 7: 5, 10: 10, 0: 0} {
		if got := scanCut(two, n); got != want {
			t.Errorf("scanCut(%d) = %d,想要 %d", n, got, want)
		}
	}
	if got := scanCut(scan("0007", t0, 8), 3); got != 8 {
		t.Errorf("单次扫描比切点长时整次带上,得到 %d", got)
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

// 个人账本(成交、银币、专注)整批带走,只有它们也要发;失败原样退回队列
func TestFlush_个人账本(t *testing.T) {
	var got model.UploadBatch
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	u := &uploader{server: srv.URL, client: srv.Client()}
	u.addTrade(model.MyTrade{Key: "mail:1", Kind: model.TradeOrderSold})
	u.addTrade(model.MyTrade{Key: "instant:2:3", Kind: model.TradeInstantBuy})
	u.addWallet(model.WalletObs{Silver: 100})
	u.addFocus(model.FocusObs{Focus: 30000})
	u.flush(context.Background())
	if len(got.Trades) != 2 || len(got.Wallet) != 1 || len(got.Focus) != 1 ||
		len(u.trades)+len(u.wallet)+len(u.focus) != 0 {
		t.Fatalf("服务端收到 %+v,队列里还剩 %d/%d/%d", got, len(u.trades), len(u.wallet), len(u.focus))
	}

	down := &uploader{server: "http://127.0.0.1:1", client: &http.Client{Timeout: time.Second}}
	down.addTrade(model.MyTrade{Key: "mail:1"})
	down.addWallet(model.WalletObs{Silver: 1})
	down.flush(context.Background())
	if len(down.trades) != 1 || len(down.wallet) != 1 || down.lastErr == "" {
		t.Fatalf("发失败应该退回队列:成交 %d、银币 %d、错误 %q", len(down.trades), len(down.wallet), down.lastErr)
	}
}
