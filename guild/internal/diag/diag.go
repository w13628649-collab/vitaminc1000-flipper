// Package diag 把客户端的警告和错误报回服务端。
//
// 成员在自己家的 Windows 上跑,出问题不可能一个个去看屏幕——
// 这是远程排查的唯一通道。
package diag

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"albion-guild/internal/model"
)

// Reporter 攒一批再发。
type Reporter struct {
	server   string
	clientID string
	version  string
	client   *http.Client

	mu        sync.Mutex
	character string
	pending   []model.DiagEntry
	maxQueue  int
}

func New(server, version string) *Reporter {
	return &Reporter{
		server:   server,
		clientID: clientID(),
		version:  version,
		client:   &http.Client{Timeout: 15 * time.Second},
		maxQueue: 500, // 服务端连不上时别让内存无限涨
	}
}

func (r *Reporter) ClientID() string { return r.clientID }

func (r *Reporter) SetCharacter(name string) {
	r.mu.Lock()
	r.character = name
	r.mu.Unlock()
}

func (r *Reporter) add(e model.DiagEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pending) >= r.maxQueue {
		// 满了丢最旧的:出问题时往往是同一条错误刷屏,
		// 留最近的比留最早的有用
		r.pending = r.pending[1:]
	}
	r.pending = append(r.pending, e)
}

// Hello 启动时报一次环境,这样即使之后没有任何错误,
// 也能在服务端看到"这个人确实把客户端跑起来了"。
func (r *Reporter) Hello(ctx context.Context, attrs map[string]any) {
	r.add(model.DiagEntry{
		Level: "info", Message: "客户端启动",
		Attrs: attrs, Timestamp: time.Now(),
	})
	r.Flush(ctx)
}

// Run 定时上报,直到 ctx 取消。
//
// **返回之前会把队列里剩下的发完,调用方必须等它返回再退出进程。**
// 不等的话:ctx 一取消,这个 goroutine 就把队列取走了,主流程再调
// Flush 只是空转,而这边的 POST 还没发出去进程就没了——
// 丢掉的恰恰是崩溃前最后那批日志,也就是最想看的那批。
func (r *Reporter) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			r.Flush(context.WithoutCancel(ctx))
			return
		case <-t.C:
			r.Flush(ctx)
		}
	}
}

func (r *Reporter) Flush(ctx context.Context) {
	r.mu.Lock()
	batch := model.DiagBatch{
		ClientID: r.clientID, Character: r.character,
		Version: r.version, OS: runtime.GOOS + "/" + runtime.GOARCH,
		Entries: r.pending,
	}
	r.pending = nil
	r.mu.Unlock()

	if len(batch.Entries) == 0 {
		return
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.server+"/api/diag", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		// 发不出去就退回队列。注意这里**不能**再打日志,
		// 否则 handler 会把它又塞进队列,形成自激循环。
		r.mu.Lock()
		r.pending = append(batch.Entries, r.pending...)
		if len(r.pending) > r.maxQueue {
			r.pending = r.pending[len(r.pending)-r.maxQueue:]
		}
		r.mu.Unlock()
		return
	}
	_ = resp.Body.Close()
}

// Handler 包一层 slog.Handler:日志照常输出,同时把 warn 及以上收进上报队列。
func (r *Reporter) Handler(next slog.Handler) slog.Handler {
	return &handler{next: next, rep: r}
}

type handler struct {
	next slog.Handler
	rep  *Reporter
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

func (h *handler) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelWarn {
		attrs := make(map[string]any, rec.NumAttrs())
		rec.Attrs(func(a slog.Attr) bool {
			attrs[a.Key] = jsonSafe(a.Value.Any())
			return true
		})
		level := "warn"
		if rec.Level >= slog.LevelError {
			level = "error"
		}
		h.rep.add(model.DiagEntry{
			Level: level, Message: rec.Message,
			Attrs: attrs, Timestamp: rec.Time,
		})
	}
	return h.next.Handle(ctx, rec)
}

// jsonSafe 把日志属性转成 JSON 里看得见的东西。
//
// error 直接 Marshal 出来是 {} —— 它没有导出字段。
// 而 err 恰恰是排查时最想看的那一个,必须转成文本。
func jsonSafe(v any) any {
	switch x := v.(type) {
	case nil, bool, string, float32, float64,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		time.Time, time.Duration:
		return v
	case error:
		return x.Error()
	case fmt.Stringer:
		return x.String()
	default:
		// 剩下的结构体多半也 Marshal 不出有用信息,一律转文本
		return fmt.Sprint(v)
	}
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	return &handler{next: h.next.WithAttrs(as), rep: h.rep}
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{next: h.next.WithGroup(name), rep: h.rep}
}

// clientID 取一个稳定的设备标识,首次生成后写到配置目录。
// 拿它把同一台机器报上来的记录串起来。
func clientID() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return randomID()
	}
	dir = filepath.Join(dir, "AlbionFlipper")
	path := filepath.Join(dir, "client_id")

	if b, err := os.ReadFile(path); err == nil && len(b) >= 8 {
		return string(bytes.TrimSpace(b))
	}
	id := randomID()
	if err := os.MkdirAll(dir, 0o755); err == nil {
		_ = os.WriteFile(path, []byte(id), 0o644)
	}
	return id
}

func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	host, _ := os.Hostname()
	if host != "" && len(host) > 12 {
		host = host[:12]
	}
	return host + "-" + hex.EncodeToString(b)
}
