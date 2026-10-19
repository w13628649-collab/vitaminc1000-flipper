package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"albion-guild/internal/ingest"
	"albion-guild/internal/model"
	"albion-guild/internal/protocol"
	"albion-guild/internal/store"
)

// 抓包里除挂单以外的东西:op 197 的建筑 / 制造站费率。
// 鉴权和其他 /api/* 一致(目前都不验,CDK token 第一版还没接)。

// captureStore 是这组接口用到的存储方法。抽成接口是为了测试不用起库
type captureStore interface {
	SaveStations(ctx context.Context, reporter string, obs []model.StationObs) error
	StationFees(ctx context.Context, since time.Time) ([]store.StationFee, error)
	SaveSamples(ctx context.Context, reporter string, samples []model.CaptureSample) error
	Samples(ctx context.Context, kind string, code, limit int) ([]store.SampleRow, error)
}

const (
	defaultStationAgeHours = 7 * 24 // 费率是房主手设的,变得慢,默认看一周
	maxStationAgeHours     = 90 * 24
)

func (s *Server) registerCapture(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/stations", s.handleStations)
	mux.HandleFunc("GET /api/samples", s.handleSamples)
}

// saveCaptureExtras 存一批上传里挂单以外的部分(建筑、原始样本)。时间先按挂单同一套口径纠偏。
// 返回各存下几条;失败只记日志——挂单那部分已经进去了,不能因为这部分让整批报错重传
func (s *Server) saveCaptureExtras(ctx context.Context, batch model.UploadBatch) (stations, samples int) {
	if s.Capture == nil {
		return 0, 0
	}
	now := time.Now()
	if len(batch.Stations) > 0 {
		obs := make([]model.StationObs, 0, len(batch.Stations))
		rejected := 0
		for _, o := range batch.Stations {
			// 「只收城区本体」在服务端再判一次:/api/upload 不鉴权,老版本、开发版、
			// 改过的客户端都能往这里塞野外区、个人岛的建筑和坐标
			if !protocol.CityZone(o.ZoneID) {
				rejected++
				continue
			}
			o.ObservedAt = ingest.ShiftObserved(o.ObservedAt, batch.SentAt, now)
			o.Owner = stripNUL(o.Owner)
			o.Key23, o.Key31 = jsonNoNUL(o.Key23), jsonNoNUL(o.Key31)
			obs = append(obs, o)
		}
		if rejected > 0 {
			slog.Warn("上传的建筑不在城区本体,丢弃", "count", rejected, "reporter", batch.Reporter,
				"client_version", batch.ClientVersion)
		}
		if len(obs) > 0 {
			if err := s.Capture.SaveStations(ctx, batch.Reporter, obs); err != nil {
				slog.Error("写入工作站观测失败", "err", err, "count", len(obs), "reporter", batch.Reporter)
			} else {
				stations = len(obs)
			}
		}
	}
	if len(batch.Samples) > 0 {
		sm := make([]model.CaptureSample, 0, len(batch.Samples))
		rejected := 0
		for _, x := range batch.Samples {
			if why := sampleRejected(x); why != "" {
				rejected++
				slog.Debug("上传的样本不合规,丢弃", "kind", x.Kind, "code", x.Code, "why", why)
				continue
			}
			x.ObservedAt = ingest.ShiftObserved(x.ObservedAt, batch.SentAt, now)
			if x.ClientVersion == "" {
				x.ClientVersion = batch.ClientVersion
			}
			x.Params = jsonNoNUL(x.Params)
			sm = append(sm, x)
		}
		if rejected > 0 {
			slog.Warn("上传的样本不合规,丢弃", "count", rejected, "reporter", batch.Reporter,
				"client_version", batch.ClientVersion)
		}
		if len(sm) == 0 {
			return stations, 0
		}
		if err := s.Capture.SaveSamples(ctx, batch.Reporter, sm); err != nil {
			slog.Error("写入原始样本失败", "err", err, "count", len(sm), "reporter", batch.Reporter)
		} else {
			samples = len(sm)
		}
	}
	return stations, samples
}

// sampleRejected 校验一条上传的样本,不合规返回原因。
//
// 2026-10-19 起明文全抓,样本不再按白名单收,但 /api/upload 不鉴权,形状还是要卡:
// 方向只能是那三种、码不超过 protocol.MaxCode——SaveSamples 按 (kind, code) 各留
// store.SamplesPerCode 条,换着码传表就无限增长,卡住码的范围表才有上限
func sampleRejected(x model.CaptureSample) string {
	switch x.Kind {
	case model.SampleOpRequest, model.SampleOpResponse, model.SampleEvent:
	default:
		return "方向不认识"
	}
	if x.Code < 0 || x.Code > protocol.MaxCode {
		return fmt.Sprintf("码 %d 超出 0..%d", x.Code, protocol.MaxCode)
	}
	var params []struct {
		K int `json:"k"`
	}
	if err := json.Unmarshal(x.Params, &params); err != nil {
		return "params 不是参数表数组"
	}
	return ""
}

// Postgres 的 TEXT 不收 NUL 字节、jsonb 不收 \u0000:一个 NUL 就让整批 CopyFrom 失败,
// 这一批的建筑或样本全丢(只记日志)。换成 U+FFFD,看得出这里原来有东西
func stripNUL(s string) string { return strings.ReplaceAll(s, "\x00", "�") }

func jsonNoNUL(raw json.RawMessage) json.RawMessage {
	if raw == nil || !bytes.Contains(raw, []byte(`\u0000`)) {
		return raw
	}
	return bytes.ReplaceAll(raw, []byte(`\u0000`), []byte(`�`))
}

// handleSamples:GET /api/samples?code=&kind=&limit=
//
// 导出原始样本,给人照着写解码器。code 不填 = 全部码;kind 是 op_response / event;
// limit 默认 50,上限 200(库里每个码也只留 200 条)。
func (s *Server) handleSamples(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := intOr(q.Get("code"), -1)
	if q.Get("code") != "" && code < 0 {
		http.Error(w, "code 要是非负整数", http.StatusBadRequest)
		return
	}
	kind := strings.TrimSpace(q.Get("kind"))
	if kind != "" && kind != model.SampleOpRequest && kind != model.SampleOpResponse && kind != model.SampleEvent {
		http.Error(w, "kind 只能是 op_response 或 event", http.StatusBadRequest)
		return
	}
	limit := intOr(q.Get("limit"), 50)
	if limit <= 0 || limit > store.SamplesPerCode {
		http.Error(w, "limit 只能是 1~200", http.StatusBadRequest)
		return
	}
	if s.Capture == nil {
		http.Error(w, "没有存储", http.StatusServiceUnavailable)
		return
	}
	rows, err := s.Capture.Samples(r.Context(), kind, code, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []store.SampleRow{}
	}
	writeJSON(w, rows)
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
