package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"albion-guild/internal/clientui"
	"albion-guild/internal/model"
)

// maxPending 是断网时内存里最多堆多少条挂单。
// 一条大约 100 字节,10 万条约 10 MB——成员的机器扛得住,
// 再多就没意义了:市场数据本来就是越新越值钱。
const maxPending = 100_000

// maxPendingStations 是断网时最多堆多少条建筑观测。一个城一次地图几十到几百个建筑,
// 5000 条够攒几十次开图;费率变得慢,丢最旧的没损失
const maxPendingStations = 5_000

// uploader 攒一批再发,不是每解析出一条就发一次。
type uploader struct {
	server string
	token  string
	client *http.Client

	mu        sync.Mutex
	pending   []model.MarketOrder
	stations  []model.StationObs
	character string
	location  string
	seen      int64
	uploaded  int64
	// stationsUploaded 是传上去的建筑观测条数
	stationsUploaded int64
	lastAt           time.Time
	lastErr          string
}

// snapshot 取当前状态给界面看。只填客户端自己知道的那几项,
// 版本、网卡数之类由调用方补。
func (u *uploader) snapshot() clientui.Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	st := clientui.Status{
		Character:        u.character,
		Location:         u.location,
		OrdersSeen:       u.seen,
		OrdersUploaded:   u.uploaded,
		StationsUploaded: u.stationsUploaded,
		Pending:          len(u.pending),
		LastError:        u.lastErr,
		ServerOK:         u.lastErr == "",
	}
	if !u.lastAt.IsZero() {
		st.LastUploadAt = u.lastAt.Format(time.RFC3339)
	}
	return st
}

func (u *uploader) add(orders []model.MarketOrder) {
	u.mu.Lock()
	u.pending = append(u.pending, orders...)
	u.seen += int64(len(orders))
	u.trimLocked()
	u.mu.Unlock()
}

func (u *uploader) addStations(obs []model.StationObs) {
	u.mu.Lock()
	u.stations = append(u.stations, obs...)
	u.trimLocked()
	u.mu.Unlock()
}

// requeue 把发失败的一批退回队列头部,并记下错误给界面看。
func (u *uploader) requeue(b model.UploadBatch, err error) {
	u.mu.Lock()
	u.pending = append(b.Orders, u.pending...)
	u.stations = append(b.Stations, u.stations...)
	u.trimLocked()
	u.lastErr = err.Error()
	u.mu.Unlock()
}

// trimLocked 超上限就丢最旧的。调用方必须持锁。
func (u *uploader) trimLocked() {
	u.pending = trimOldest(u.pending, maxPending)
	u.stations = trimOldest(u.stations, maxPendingStations)
}

func trimOldest[T any](s []T, limit int) []T {
	if n := len(s) - limit; n > 0 {
		return append(s[:0], s[n:]...)
	}
	return s
}

func (u *uploader) setIdentity(name, location string) {
	u.mu.Lock()
	u.character, u.location = name, location
	u.mu.Unlock()
}

func (u *uploader) run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.flush(ctx)
		}
	}
}

func (u *uploader) flush(ctx context.Context) {
	u.mu.Lock()
	// SentAt 让服务端算出这台机器的钟差并整批平移 ObservedAt。服务端的 touch
	// 已改为记观测时间而不是服务端时刻,不带 SentAt 的话钟慢的成员的单会提前过期。
	// 每次发送现填:退回队列重发的那批也要用这一次发出的时刻,里面的旧单才保持旧
	batch := model.UploadBatch{Reporter: u.character, Orders: u.pending,
		Stations: u.stations, SentAt: time.Now(), ClientVersion: version}
	u.pending, u.stations = nil, nil
	u.mu.Unlock()

	// 只有建筑没有挂单也要发:开一次地图不翻市场是常态。
	// 以前这里挂单为空就 return
	if len(batch.Orders) == 0 && len(batch.Stations) == 0 {
		return
	}

	body, err := json.Marshal(batch)
	if err != nil {
		slog.Error("序列化失败", "err", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		u.server+"/api/upload", bytes.NewReader(body))
	if err != nil {
		// 服务端地址配错会走到这。以前这里直接 return,整批挂单
		// 连同后面每一批都被静默吞掉,界面上还一直显示"抓包中"
		u.requeue(batch, err)
		slog.Error("构造上传请求失败,检查 -server 地址", "server", u.server, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		// 网络不好就把这批退回队列,下次一起发。
		// 断网期间数据堆在内存里,连上自动补——这是 HTTP 比 WS 省事的地方
		u.requeue(batch, err)
		slog.Warn("上传失败,已退回队列", "count", len(batch.Orders),
			"stations", len(batch.Stations), "err", err)
		return
	}
	defer resp.Body.Close()

	var out struct{ Changed, Touched, Stations int }
	_ = json.NewDecoder(resp.Body).Decode(&out)

	u.mu.Lock()
	u.uploaded += int64(len(batch.Orders))
	u.stationsUploaded += int64(len(batch.Stations))
	u.lastAt = time.Now()
	u.lastErr = ""
	u.mu.Unlock()
	slog.Info("已上传", "sent", len(batch.Orders), "changed", out.Changed, "touched", out.Touched,
		"stations", len(batch.Stations), "stations_saved", out.Stations)
}
