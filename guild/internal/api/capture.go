package api

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"albion-guild/internal/ingest"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

// 抓包里除挂单以外的东西:op 197 的建筑 / 制造站费率。
// 鉴权和其他 /api/* 一致(目前都不验,CDK token 第一版还没接)。

// captureStore 是这组接口用到的存储方法。抽成接口是为了测试不用起库
type captureStore interface {
	SaveStations(ctx context.Context, reporter string, obs []model.StationObs) error
	StationFees(ctx context.Context, since time.Time) ([]store.StationFee, error)
}

const (
	defaultStationAgeHours = 7 * 24 // 费率是房主手设的,变得慢,默认看一周
	maxStationAgeHours     = 90 * 24
)

func (s *Server) registerCapture(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stations", s.handleStations)
}

// saveCaptureExtras 存一批上传里挂单以外的部分。时间先按挂单同一套口径纠偏。
// 返回存下的建筑条数;失败只记日志——挂单那部分已经进去了,不能因为这部分让整批报错重传
func (s *Server) saveCaptureExtras(ctx context.Context, batch model.UploadBatch) (stations int) {
	if len(batch.Stations) == 0 || s.Capture == nil {
		return 0
	}
	now := time.Now()
	obs := make([]model.StationObs, len(batch.Stations))
	for i, o := range batch.Stations {
		o.ObservedAt = ingest.ShiftObserved(o.ObservedAt, batch.SentAt, now)
		obs[i] = o
	}
	if err := s.Capture.SaveStations(ctx, batch.Reporter, obs); err != nil {
		slog.Error("写入工作站观测失败", "err", err, "count", len(obs), "reporter", batch.Reporter)
		return 0
	}
	return len(obs)
}

// stationView 是 /api/stations 的一行:最新观测 + 数据年龄。
type stationView struct {
	store.StationFee
	AgeHours float64 `json:"age_hours"`
}

// handleStations:GET /api/stations?city=&max_age_hours=
//
// 返回每个 (区, 建筑) 最新的一次观测。city 按 world.ClusterCity 的城市名筛
// (不是城区的就是原始区 id);max_age_hours 默认 168,上限 2160。
func (s *Server) handleStations(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	city := strings.TrimSpace(q.Get("city"))
	age := intOr(q.Get("max_age_hours"), defaultStationAgeHours)
	if age <= 0 || age > maxStationAgeHours {
		http.Error(w, "max_age_hours 只能是 1~2160", http.StatusBadRequest)
		return
	}
	if s.Capture == nil {
		http.Error(w, "没有存储", http.StatusServiceUnavailable)
		return
	}
	now := time.Now()
	fees, err := s.Capture.StationFees(r.Context(), now.Add(-time.Duration(age)*time.Hour))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := make([]stationView, 0, len(fees))
	for _, f := range fees {
		if city != "" && !strings.EqualFold(f.City, city) {
			continue
		}
		out = append(out, stationView{StationFee: f,
			AgeHours: roundTo(now.Sub(f.ObservedAt).Hours(), 2)})
	}
	writeJSON(w, map[string]any{
		"generated_at":  now.UTC().Format(time.RFC3339),
		"city":          city,
		"max_age_hours": age,
		"stations":      out,
		// 两条口径提醒原样带上,免得下游当成定论
		"notes": []string{
			"key33=associate_fee、key34=public_fee 只有社区一个来源,待游戏内截图确认",
			"费率标度线上没定:raw > 1000 按 /10000,否则原值;*_raw 是原值",
		},
	})
}

func roundTo(v float64, digits int) float64 {
	p := math.Pow10(digits)
	return math.Round(v*p) / p
}
