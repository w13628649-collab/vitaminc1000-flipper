package api

import (
	"net/http"
	"strings"
	"time"
)

// 查价页的两个接口。旧的 /api/lookup、/api/book、/api/depth 一律不动:
// 旧客户端还在按行级 source 读它们。
//
// Go 1.22 的 mux 里 "GET /api/lookup" 只匹配这条路径本身,
// 和下面两条不冲突。

// lookupTimeout 要比 flip 里等 AODP 的 12 秒长,留出读库和编码的时间。
// AODP 的限流器是阻塞等待,不设期限的话撞上定时扫描能挂一两分钟
const lookupTimeout = 20 * time.Second

func (s *Server) registerLookup(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/lookup/grid", s.handleLookupGrid)
	mux.HandleFunc("GET /api/lookup/book", s.handleLookupBook)
}

// handleLookupGrid:GET /api/lookup/grid?item=ID
func (s *Server) handleLookupGrid(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimSpace(r.URL.Query().Get("item"))
	if itemID == "" {
		http.Error(w, "缺少 item 参数", http.StatusBadRequest)
		return
	}
	ctx, cancel := contextWithTimeout(r, lookupTimeout)
	defer cancel()
	res, err := s.Flip.LookupGrid(ctx, itemID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

// handleLookupBook:GET /api/lookup/book?item=&city=&quality=1[&qty=N]
func (s *Server) handleLookupBook(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	itemID := strings.TrimSpace(q.Get("item"))
	city := strings.TrimSpace(q.Get("city"))
	if itemID == "" || city == "" {
		http.Error(w, "缺少 item 或 city 参数", http.StatusBadRequest)
		return
	}
	quality := intOr(q.Get("quality"), 1)
	if quality < 1 || quality > 5 {
		http.Error(w, "quality 只能是 1~5", http.StatusBadRequest)
		return
	}
	want := intOr(q.Get("qty"), 0)
	if want < 0 {
		want = 0
	}
	ctx, cancel := contextWithTimeout(r, lookupTimeout)
	defer cancel()
	res, err := s.Flip.LookupBook(ctx, itemID, city, quality, int64(want))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}
