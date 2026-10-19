package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"albion-guild/internal/flip"
)

// 黑市页(docs/black-market.md)。
//
//	GET /api/blackmarket?bm_age=45&city_age=180&cities=Martlock,Caerleon
//
// bm_age / city_age 是两侧数据龄上限(分钟),不给用默认 45 / 180;cities 不给是除黑市外的全部城市

// blackMarketTimeout 包住读库和最多 8 秒的现取 AODP(flip.bmAODPWait)
const blackMarketTimeout = 20 * time.Second

func (s *Server) handleBlackMarket(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var p flip.BlackMarketParams
	for _, f := range []struct {
		name string
		dst  *time.Duration
	}{{"bm_age", &p.BMMaxAge}, {"city_age", &p.CityMaxAge}} {
		v := strings.TrimSpace(q.Get(f.name))
		if v == "" {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, f.name+" 要是正整数(分钟)", http.StatusBadRequest)
			return
		}
		*f.dst = time.Duration(n) * time.Minute
	}
	for _, c := range strings.Split(q.Get("cities"), ",") {
		if c = strings.TrimSpace(c); c != "" {
			p.Cities = append(p.Cities, c)
		}
	}
	ctx, cancel := contextWithTimeout(r, blackMarketTimeout)
	defer cancel()
	plan, err := s.Flip.BlackMarket(ctx, p)
	if err != nil {
		var pe *flip.ParamError
		if errors.As(err, &pe) {
			http.Error(w, pe.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, plan)
}
