// Package api 提供 HTTP 上传/查询接口和 WebSocket 行情推送。
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"albion-guild/internal/flip"
	"albion-guild/internal/hub"
	"albion-guild/internal/ingest"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
	"albion-guild/internal/webui"
)

const (
	// 冷门物品可能几分钟没有推送,不发心跳会被 LB 的空闲超时静默掐掉
	pingInterval = 30 * time.Second
	writeTimeout = 10 * time.Second
	readTimeout  = 70 * time.Second // 要大于 pingInterval
)

type Server struct {
	Store    *store.Store
	Ingestor *ingest.Ingestor
	Hub      *hub.Hub
	Fresh    time.Duration // 多久没再看到的挂单不算数
	// Flip 是倒爷工具那一套(目录/扫描/榜单)。为 nil 时那组接口不注册。
	Flip *flip.Service

	// ReleaseDir 是客户端二进制放哪。空字符串则不提供下载和更新检查。
	ReleaseDir string
	// ReleaseVersion 是发布目录里那个客户端的版本号。
	ReleaseVersion string
	// MinClientVersion 是服务端接受的最低客户端版本,低于它就必须更新。
	MinClientVersion string

	releases releaseCache
	upgrader websocket.Upgrader
}

func New(st *store.Store, ing *ingest.Ingestor, h *hub.Hub,
	fresh time.Duration, fl *flip.Service) *Server {
	return &Server{
		Store: st, Ingestor: ing, Hub: h, Fresh: fresh, Flip: fl,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 4096,
			// 桌面客户端的 webview 不带 Origin,这里不做同源检查;
			// 真正的准入控制走 CDK token(第一版还没接)
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("POST /api/diag", s.handleDiagUpload)
	mux.HandleFunc("GET /api/diag", s.handleDiagList)
	mux.HandleFunc("GET /api/book", s.handleBook)
	mux.HandleFunc("GET /api/quotes", s.handleQuotes)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /ws", s.handleWS)
	s.registerFlip(mux)
	s.registerRelease(mux)
	// 界面主要由客户端本地伺服(资源编在客户端里)。服务端这份是给
	// 开发和排查用的:浏览器直接打开就能看,不用起客户端
	mux.Handle("GET /", http.FileServerFS(webui.FS()))
	return mux
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	var batch model.UploadBatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&batch); err != nil {
		http.Error(w, "请求体解析失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	now := time.Now()
	for i := range batch.Orders {
		if batch.Orders[i].ObservedAt.IsZero() {
			batch.Orders[i].ObservedAt = now
		}
	}
	changed, touched := s.Ingestor.Submit(batch)
	writeJSON(w, map[string]int{"changed": changed, "touched": touched})
}

// handleDiagUpload 收客户端报上来的警告和错误。
//
// 这是远程排查的唯一通道:成员在自己家里的 Windows 上跑,
// 出了问题不可能一个个去看屏幕。
func (s *Server) handleDiagUpload(w http.ResponseWriter, r *http.Request) {
	var batch model.DiagBatch
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&batch); err != nil {
		http.Error(w, "请求体解析失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.Store.WriteDiag(r.Context(), batch); err != nil {
		slog.Error("写入诊断失败", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, e := range batch.Entries {
		// 同时打到服务端日志,这样 tail -f 就能实时看到客户端在报什么
		slog.Log(r.Context(), levelOf(e.Level), "[客户端] "+e.Message,
			"client", batch.ClientID, "character", batch.Character,
			"version", batch.Version, "os", batch.OS)
	}
	writeJSON(w, map[string]int{"accepted": len(batch.Entries)})
}

func (s *Server) handleDiagList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(orDefault(r.URL.Query().Get("limit"), "100"))
	rows, err := s.Store.RecentDiag(r.Context(), r.URL.Query().Get("level"), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []store.DiagRow{}
	}
	writeJSON(w, rows)
}

func levelOf(s string) slog.Level {
	switch s {
	case "error":
		return slog.LevelError
	case "warn":
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

func (s *Server) handleBook(w http.ResponseWriter, r *http.Request) {
	k, err := keyFromQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	levels, err := s.Store.Book(r.Context(), k, s.Fresh, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if levels == nil {
		levels = []store.BookLevel{}
	}
	writeJSON(w, map[string]any{"key": k.String(), "levels": levels})
}

// handleQuotes 是重连后补齐用的快照接口。
//
// 只重订阅不拉快照,断线那几秒变的价格永远补不回来,
// 界面显示旧值却看着"实时"——这是这类系统最常见的 bug。
func (s *Server) handleQuotes(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("keys")
	if raw == "" {
		writeJSON(w, []model.Quote{})
		return
	}
	var keys []model.QuoteKey
	for _, part := range strings.Split(raw, ",") {
		k, err := parseKey(part)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		keys = append(keys, k)
	}
	quotes, err := s.Store.BestQuotes(r.Context(), keys, s.Fresh)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if quotes == nil {
		quotes = []model.Quote{}
	}
	writeJSON(w, quotes)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"clients": s.Hub.ClientCount(),
		"dropped": s.Hub.DroppedCount(), // 不能直接读字段,Fanout 在并发改它
	})
}

type wsCommand struct {
	Op   string   `json:"op"`
	Keys []string `json:"keys"`
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade 内部已经写过响应了
	}
	client := hub.NewClient(256)
	s.Hub.Add(client)

	go s.wsWriter(conn, client)
	s.wsReader(conn, client) // 读循环占住这个 goroutine,返回即断开
}

func (s *Server) wsReader(conn *websocket.Conn, client *hub.Client) {
	defer func() {
		s.Hub.Remove(client)
		_ = conn.Close()
	}()
	conn.SetReadLimit(64 << 10)
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(readTimeout))
	})

	for {
		var cmd wsCommand
		if err := conn.ReadJSON(&cmd); err != nil {
			return
		}
		switch cmd.Op {
		case "sub":
			client.Subscribe(cmd.Keys)
		case "unsub":
			client.Unsubscribe(cmd.Keys)
		default:
			slog.Debug("未知 WS 指令", "op", cmd.Op)
		}
	}
}

func (s *Server) wsWriter(conn *websocket.Conn, client *hub.Client) {
	ping := time.NewTicker(pingInterval)
	defer func() {
		ping.Stop()
		_ = conn.Close()
	}()
	for {
		select {
		case <-client.Closed():
			return // Hub 把这个客户端摘掉了
		case msg := <-client.Out():
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func keyFromQuery(r *http.Request) (model.QuoteKey, error) {
	q := r.URL.Query()
	quality, _ := strconv.Atoi(orDefault(q.Get("quality"), "1"))
	side, _ := strconv.Atoi(orDefault(q.Get("side"), "0"))
	k := model.QuoteKey{
		ItemID:     q.Get("item"),
		LocationID: q.Get("city"),
		Quality:    int16(quality),
		Side:       model.Side(side),
	}
	if k.ItemID == "" || k.LocationID == "" {
		return k, errBadKey
	}
	return k, nil
}

func parseKey(s string) (model.QuoteKey, error) {
	parts := strings.Split(s, "|")
	if len(parts) != 4 {
		return model.QuoteKey{}, errBadKey
	}
	quality, err1 := strconv.Atoi(parts[2])
	side, err2 := strconv.Atoi(parts[3])
	if err1 != nil || err2 != nil {
		return model.QuoteKey{}, errBadKey
	}
	return model.QuoteKey{
		ItemID: parts[0], LocationID: parts[1],
		Quality: int16(quality), Side: model.Side(side),
	}, nil
}

type keyError string

func (e keyError) Error() string { return string(e) }

const errBadKey = keyError("key 格式应为 item|city|quality|side")

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// contextWithTimeout 给慢接口一个自己的期限,
// 同时保留请求被取消时的传播。
func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
