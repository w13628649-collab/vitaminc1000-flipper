package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"albion-guild/internal/arb"
	"albion-guild/internal/catalog"
	"albion-guild/internal/portfolio"
	"albion-guild/internal/rank"
	"albion-guild/internal/store"
)

// registerFlip 挂上倒爷工具那几个接口。Flip 为 nil 时整组不注册,
// 服务端就退化成纯粹的行情中转。
func (s *Server) registerFlip(mux *http.ServeMux) {
	if s.Flip == nil {
		return
	}
	mux.HandleFunc("GET /api/menu", s.handleMenu)
	mux.HandleFunc("GET /api/items", s.handleItems)
	mux.HandleFunc("GET /api/icon/{item}", s.handleIcon)
	mux.HandleFunc("GET /api/scan", s.handleScan)
	mux.HandleFunc("POST /api/scan", s.handleScanRefresh)
	mux.HandleFunc("GET /api/rank", s.handleRank)
	mux.HandleFunc("GET /api/lookup", s.handleLookup)
	mux.HandleFunc("GET /api/coverage", s.handleCoverage)
	mux.HandleFunc("POST /api/catalog/sync", s.handleCatalogSync)
	mux.HandleFunc("GET /api/arb", s.handleArb)
	mux.HandleFunc("GET /api/portfolio", s.handlePortfolio)
	mux.HandleFunc("GET /api/depth", s.handleDepth)
	mux.HandleFunc("GET /api/trades", s.handleTrades)
	mux.HandleFunc("POST /api/trades", s.handleOpenTrade)
	mux.HandleFunc("POST /api/trades/{id}/close", s.handleCloseTrade)
	mux.HandleFunc("DELETE /api/trades/{id}", s.handleDeleteTrade)
	mux.HandleFunc("GET /api/calibration", s.handleCalibration)
	s.registerLookup(mux)
}

func (s *Server) handleMenu(w http.ResponseWriter, r *http.Request) {
	cat := s.Flip.Catalog()
	if cat == nil {
		http.Error(w, "物品目录还没同步", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, cat.MenuTree())
}

func (s *Server) handleItems(w http.ResponseWriter, r *http.Request) {
	cat := s.Flip.Catalog()
	if cat == nil {
		http.Error(w, "物品目录还没同步", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	limit := intOr(q.Get("limit"), 300)

	var items []catalog.Item
	if needle := q.Get("q"); needle != "" {
		items = cat.Search(needle, limit)
	} else {
		items = cat.Browse(catalog.BrowseQuery{
			Category:    q.Get("category"),
			Subcategory: q.Get("subcategory"),
			Family:      q.Get("family"),
			Tier:        intOr(q.Get("tier"), 0),
			Enchantment: intOr(q.Get("enchant"), -1),
			Limit:       limit,
		})
	}
	if items == nil {
		items = []catalog.Item{}
	}
	writeJSON(w, items)
}

func (s *Server) handleIcon(w http.ResponseWriter, r *http.Request) {
	png, err := s.Flip.Icon(r.Context(), r.PathValue("item"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(png) == 0 {
		http.NotFound(w, r)
		return
	}
	// 图标永不变化,让浏览器和 CDN 放心长缓存
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=2592000, immutable")
	_, _ = w.Write(png)
}

func (s *Server) handleScan(w http.ResponseWriter, r *http.Request) {
	res := s.Flip.LastScan()
	if res == nil {
		http.Error(w, "还没扫过,POST /api/scan 触发一次", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleScanRefresh(w http.ResponseWriter, r *http.Request) {
	res, err := s.Flip.Scan(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleRank(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opt := rank.Options{
		City:       orDefault(q.Get("city"), rank.AllCities),
		WindowDays: intOr(q.Get("window"), 7),
		Limit:      intOr(q.Get("limit"), 50),
		SortBy:     orDefault(q.Get("sort"), "daily_silver"),
		Descending: q.Get("desc") != "0",
	}
	rows, err := s.Flip.Rank(r.Context(), opt, intOr(q.Get("quality"), 1),
		q.Get("category"), q.Get("subcategory"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rows)
}

func (s *Server) handleLookup(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimSpace(r.URL.Query().Get("item"))
	if itemID == "" {
		http.Error(w, "缺少 item 参数", http.StatusBadRequest)
		return
	}
	res, err := s.Flip.Lookup(r.Context(), itemID, s.Fresh)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, res)
}

func (s *Server) handleCoverage(w http.ResponseWriter, r *http.Request) {
	cov, err := s.Store.HistoryCoverage(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cities, err := s.Store.CitiesWithHistory(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if cities == nil {
		cities = []string{}
	}
	out := map[string]any{"history": cov, "cities": cities}
	if cat := s.Flip.Catalog(); cat != nil {
		out["items"] = cat.Len()
		out["catalog_synced_at"] = cat.SyncedAt()
	}
	if s.Ingestor != nil {
		// 串城冲突和时钟钳位是服务端唯一看得见的抓包污染信号,
		// 放在覆盖率里一起看:覆盖率高但冲突在涨,说明数据多而不准
		out["ingest"] = s.Ingestor.Stats()
	}
	if res := s.Flip.LastScan(); res != nil {
		out["scan"] = map[string]any{
			"started_at":    res.StartedAt,
			"evaluated_at":  res.EvaluatedAt,
			"opportunities": len(res.Opportunities),
			"price_rows":    res.PriceRows,
			"coverage":      res.Coverage,
			"reject_counts": res.RejectCounts,
			"capture":       res.Capture,
		}
	}
	writeJSON(w, out)
}

// handleCatalogSync 重新从 ao-bin-dumps 同步物品目录。
// 游戏更新后手动点一次就行,不用重启服务。
func (s *Server) handleCatalogSync(w http.ResponseWriter, r *http.Request) {
	// 上游两份 dump 加起来 40MB,给足时间
	ctx, cancel := contextWithTimeout(r, 10*time.Minute)
	defer cancel()
	if err := s.Flip.SyncCatalog(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"items": s.Flip.Catalog().Len()})
}

func intOr(v string, def int) int {
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func (s *Server) handleArb(w http.ResponseWriter, r *http.Request) {
	res := s.Flip.LastScan()
	if res == nil {
		http.Error(w, "还没扫过,POST /api/scan 触发一次", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	limit := intOr(q.Get("limit"), 60)
	from, to := q.Get("from"), q.Get("to")

	rows := make([]arb.Route, 0, limit)
	for _, route := range res.Routes {
		if from != "" && route.FromCity != from {
			continue
		}
		if to != "" && route.ToCity != to {
			continue
		}
		rows = append(rows, route)
		if len(rows) >= limit {
			break
		}
	}
	writeJSON(w, rows)
}

func (s *Server) handlePortfolio(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opt := portfolio.DefaultOptions(int64(intOr(q.Get("capital"), int(s.Flip.Cfg.Capital))))
	if v := q.Get("max_per_slice"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			opt.MaxPerSlice = f
		}
	}
	if v := q.Get("risk"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			opt.RiskAversion = f
		}
	}
	writeJSON(w, s.Flip.Portfolio(opt))
}

func (s *Server) handleDepth(w http.ResponseWriter, r *http.Request) {
	k, err := keyFromQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fill, err := s.Flip.Depth(r.Context(), k, int64(intOr(r.URL.Query().Get("qty"), 100)), s.Fresh)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, fill)
}

// ── 交易记账 ───────────────────────────────────────────────
// 公会里各记各的,owner 暂时从查询参数走。接了 CDK 之后换成 token 里的身份。

func (s *Server) handleTrades(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.Store.Trades(r.Context(), q.Get("owner"), q.Get("status"),
		intOr(q.Get("limit"), 100))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if rows == nil {
		rows = []store.Trade{}
	}
	writeJSON(w, rows)
}

func (s *Server) handleOpenTrade(w http.ResponseWriter, r *http.Request) {
	var t store.Trade
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&t); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	id, err := s.Store.OpenTrade(r.Context(), t)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]int64{"id": id})
}

func (s *Server) handleCloseTrade(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id 不是数字", http.StatusBadRequest)
		return
	}
	var t store.Trade
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&t); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.Store.CloseTrade(r.Context(), id, t.Owner, t); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteTrade(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "id 不是数字", http.StatusBadRequest)
		return
	}
	if err := s.Store.DeleteTrade(r.Context(), id, r.URL.Query().Get("owner")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// handleCalibration 是这套工具唯一能自我修正的地方:
// 拿真实成交记录反推模型里那个拍脑袋的 absorb_ratio。
func (s *Server) handleCalibration(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.Calibrate(r.Context(), r.URL.Query().Get("owner"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"calibration":    c,
		"current_absorb": s.Flip.Cfg.Sizing.AbsorbRatio,
	})
}
