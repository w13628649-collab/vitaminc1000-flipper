package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

type fakeCapture struct {
	saved    []model.StationObs
	reporter string
	fees     []store.StationFee
	since    time.Time
	samples  []model.CaptureSample
	query    []any
}

func (f *fakeCapture) SaveStations(_ context.Context, reporter string, obs []model.StationObs) error {
	f.reporter = reporter
	f.saved = append(f.saved, obs...)
	return nil
}

func (f *fakeCapture) StationFees(_ context.Context, since time.Time) ([]store.StationFee, error) {
	f.since = since
	return f.fees, nil
}

func (f *fakeCapture) SaveSamples(_ context.Context, reporter string, s []model.CaptureSample) error {
	f.reporter = reporter
	f.samples = append(f.samples, s...)
	return nil
}

func (f *fakeCapture) Samples(_ context.Context, kind string, code, limit int) ([]store.SampleRow, error) {
	f.query = []any{kind, code, limit}
	return nil, nil
}

// 原始样本跟着上传走:时间纠偏,样本没带版本时用批次的
func TestUpload_原始样本(t *testing.T) {
	fc := &fakeCapture{}
	s := New(nil, nil, nil, time.Minute, nil)
	s.Capture = fc
	body, _ := json.Marshal(model.UploadBatch{Reporter: "甲", ClientVersion: "9.9",
		Samples: []model.CaptureSample{{Code: 153, Kind: model.SampleEvent,
			Params: json.RawMessage(`[]`), ObservedAt: time.Now().Add(time.Hour)}}})
	out := postUpload(t, s, string(body))
	if out["samples"] != 1 || len(fc.samples) != 1 {
		t.Fatalf("返回 %v,存了 %d 条", out, len(fc.samples))
	}
	if fc.samples[0].ClientVersion != "9.9" || fc.samples[0].ObservedAt.After(time.Now()) {
		t.Fatalf("样本: %+v", fc.samples[0])
	}
}

func TestSamples_参数(t *testing.T) {
	fc := &fakeCapture{}
	s := New(nil, nil, nil, time.Minute, nil)
	s.Capture = fc
	get := func(url string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
		return rec
	}
	rec := get("/api/samples?code=197&kind=op_response&limit=5")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Fatalf("状态 %d: %s", rec.Code, rec.Body.String())
	}
	if fc.query[0] != "op_response" || fc.query[1] != 197 || fc.query[2] != 5 {
		t.Fatalf("查询参数: %v", fc.query)
	}
	get("/api/samples")
	if fc.query[0] != "" || fc.query[1] != -1 || fc.query[2] != 50 {
		t.Fatalf("缺省参数: %v", fc.query)
	}
	for _, bad := range []string{"code=-3", "code=x", "kind=request", "limit=0", "limit=500"} {
		if rec := get("/api/samples?" + bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s 应是 400,得到 %d", bad, rec.Code)
		}
	}
}

func postUpload(t *testing.T, s *Server, body string) map[string]int {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/upload", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]int
	_ = json.NewDecoder(rec.Body).Decode(&out)
	return out
}

// 只开了地图没翻市场:只有建筑的批次要能存,而且不进 ingest
// (Ingestor 是 nil,走进去就 panic——这条同时钉住"没挂单不调 Submit")
func TestUpload_只有建筑的批次(t *testing.T) {
	fc := &fakeCapture{}
	s := New(nil, nil, nil, time.Minute, nil)
	s.Capture = fc

	sent := time.Now().Add(5 * time.Minute) // 客户端钟快 5 分钟
	obs := sent.Add(-2 * time.Second)
	body, _ := json.Marshal(model.UploadBatch{Reporter: "甲", SentAt: sent,
		Stations: []model.StationObs{{ZoneID: "0000", BuildingType: 1, ObservedAt: obs}}})
	out := postUpload(t, s, string(body))

	if out["stations"] != 1 || len(fc.saved) != 1 || fc.reporter != "甲" {
		t.Fatalf("返回 %v,存了 %d 条", out, len(fc.saved))
	}
	// 时间按挂单同一套口径纠偏:平移回服务端时钟,不能留在未来
	if d := time.Until(fc.saved[0].ObservedAt); d > time.Second || d < -5*time.Second {
		t.Fatalf("没纠偏,观测时间离现在 %v", d)
	}
}

// 新客户端连老服务端:老服务端不认识的字段要能被忽略。
// 这里反过来钉住新服务端也不开 DisallowUnknownFields——将来的客户端再加字段照样能传
func TestUpload_不认识的字段忽略(t *testing.T) {
	fc := &fakeCapture{}
	s := New(nil, nil, nil, time.Minute, nil)
	s.Capture = fc
	out := postUpload(t, s, `{"reporter":"甲","orders":[],"future_field":[1,2,3]}`)
	if out["stations"] != 0 || len(fc.saved) != 0 {
		t.Fatalf("老格式不该存建筑: %v", out)
	}
}

func TestStations_筛城市和年龄(t *testing.T) {
	now := time.Now()
	fee := 500.0
	fc := &fakeCapture{fees: []store.StationFee{
		{StationObs: model.StationObs{ZoneID: "0000", BuildingType: 1, PublicFee: &fee,
			ObservedAt: now.Add(-90 * time.Minute)}, City: "Thetford", BuildingKey: "1@1,2"},
		{StationObs: model.StationObs{ZoneID: "1000", BuildingType: 2, ObservedAt: now},
			City: "Lymhurst", BuildingKey: "2#0"},
	}}
	s := New(nil, nil, nil, time.Minute, nil)
	s.Capture = fc

	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stations?city=thetford&max_age_hours=24", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		MaxAge   int `json:"max_age_hours"`
		Stations []struct {
			ZoneID    string   `json:"zone_id"`
			City      string   `json:"city"`
			PublicFee *float64 `json:"public_fee"`
			AgeHours  float64  `json:"age_hours"`
			Key       string   `json:"building_key"`
		} `json:"stations"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Stations) != 1 || out.Stations[0].City != "Thetford" || *out.Stations[0].PublicFee != 500 ||
		out.Stations[0].Key != "1@1,2" {
		t.Fatalf("得到 %+v", out)
	}
	if a := out.Stations[0].AgeHours; a < 1.49 || a > 1.6 {
		t.Fatalf("数据年龄应约 1.5 小时,得到 %v", a)
	}
	if d := now.Add(-24 * time.Hour).Sub(fc.since); d > time.Minute || d < -time.Minute {
		t.Fatalf("since 应是 24 小时前,差了 %v", d)
	}

	for _, bad := range []string{"0", "-1", "99999"} {
		rec := httptest.NewRecorder()
		s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stations?max_age_hours="+bad, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("max_age_hours=%s 应是 400,得到 %d", bad, rec.Code)
		}
	}
}

// 没接存储时查询给 503,不 panic
func TestStations_没有存储(t *testing.T) {
	s := New(nil, nil, nil, time.Minute, nil)
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stations", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("得到 %d", rec.Code)
	}
}
