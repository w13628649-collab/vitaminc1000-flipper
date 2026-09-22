// Package api 提供 HTTP 上传/查询接口和 WebSocket 行情推送。
package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ludy/albion-guild/internal/hub"
	"github.com/ludy/albion-guild/internal/ingest"
	"github.com/ludy/albion-guild/internal/model"
	"github.com/ludy/albion-guild/internal/store"
)

const (
	// 冷门物品可能几分钟没有推送,不发心跳会被 LB 的空闲超时静默掐掉
	pingInterval = 30 * time.Second
	writeTimeout = 10 * time.Second
	readTimeout  = 70 * time.Second // 要大于 pingInterval
)

type Server struct {
	Store     *store.Store
	Ingestor  *ingest.Ingestor
	Hub       *hub.Hub
	Fresh     time.Duration // 多久没再看到的挂单不算数
	upgrader  websocket.Upgrader
}

func New(st *store.Store, ing *ingest.Ingestor, h *hub.Hub, fresh time.Duration) *Server {
	return &Server{
		Store: st, Ingestor: ing, Hub: h, Fresh: fresh,
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
	mux.HandleFunc("GET /api/book", s.handleBook)
	mux.HandleFunc("GET /api/quotes", s.handleQuotes)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /ws", s.handleWS)
	mux.Handle("GET /", http.FileServer(http.Dir("web")))
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
		"dropped": s.Hub.Dropped,
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
		case msg, ok := <-client.Out():
			if !ok {
				return // Hub 把这个客户端摘掉了
			}
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
