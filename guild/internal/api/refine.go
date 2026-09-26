package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"albion-guild/internal/flip"
	"albion-guild/internal/refine"
)

// 精炼页的三个接口(设计 §4.4)。个人参数(专精 FCE、今日专注、自己看到的站费、实测的 k)
// 由客户端存本地、每次请求带上;服务端是全公会共用的,只放默认值(conf.Refining)。
//
//	POST /api/refine/plan     请求体是 refine.Params 的 JSON(见 refine/params.go 的 json 标签)
//	GET  /api/refine/matrix   一条线 25 个档位各取最好的一条
//	GET  /api/refine/recipe   配方树和转化格子

const (
	// refineTimeout 是 handler 最多等多久。一次计算要读库、最多等 AODP 12 秒(lookupAODPWait)、
	// 再算几十毫秒;矩阵是 25 次。到点先回 504,计算留在后台跑完进缓存,过两秒再点就有了
	refineTimeout = 30 * time.Second
	// refineMaxBody:请求体就是一份参数,几百字节;挡一下误传的大文件
	refineMaxBody = 64 << 10
)

func (s *Server) registerRefine(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/refine/plan", s.handleRefinePlan)
	mux.HandleFunc("GET /api/refine/matrix", s.handleRefineMatrix)
	mux.HandleFunc("GET /api/refine/recipe", s.handleRefineRecipe)
}

// handleRefinePlan:POST /api/refine/plan。
//
// 请求体解码两遍:第一遍只取 target 和 city,拿它们向服务端要打底的参数
// (站费按城市、每日加成按线,见 refine.DefaultsFrom);第二遍把整个请求体解码到打底的参数上。
// 请求里没写的字段保持默认值,写了的——包括零值(站费 0、心不返还)——照写的算。
// 不认识的字段直接 400:参数名拼错时悄悄用了默认值,算出来的数看着对、其实不是你要的
func (s *Server) handleRefinePlan(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, refineMaxBody))
	if err != nil {
		http.Error(w, "请求体读取失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	var probe struct {
		Target string `json:"target"`
		City   string `json:"city"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		http.Error(w, "请求体不是合法的 JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	target := strings.TrimSpace(probe.Target)
	if target == "" {
		http.Error(w, "缺少 target(要做的半成品,例如 T8_METALBAR_LEVEL4@4)", http.StatusBadRequest)
		return
	}
	p := s.Flip.RefineDefaults(target, strings.TrimSpace(probe.City))
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		http.Error(w, "请求体解析失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	p.Target, p.City = target, strings.TrimSpace(p.City)
	if p.City == "" {
		p.City = s.Flip.RefineDefaults(target, "").City
	}

	ctx, cancel := contextWithTimeout(r, refineTimeout)
	defer cancel()
	res, err := s.Flip.Refine(ctx, p)
	if err != nil {
		writeRefineError(w, err)
		return
	}
	writeJSON(w, res)
}

// handleRefineMatrix:GET /api/refine/matrix?line=ORE&city=Thetford&qty=10
//
// 可选的个人参数(都用请求体里的同名字段的含义):
//
//	k                        转化实付系数(fees.transmute_factor)
//	station_per100           精炼站费(fees.station_per100)
//	transmute_station_per100 转化主人费率(fees.transmute_station_per100)
//	budget                   今日可用专注(focus.budget)
//	fce                      专精:一个数表示 T4~T8 都是它,或者 4:0,5:1000,… 逐 tier 给
//	unlocked_tier            已解锁精炼到 T 几(focus.unlocked_tier)
//	focus                    专注模式 auto | off | top | all(矩阵不支持 custom)
//	sort                     排序键 daily | unit | per_focus | pct
//	sell                     主出售方式 post | take
//	daily_bonus              每日生产加成(rrr.daily_bonus)
//	island                   1 = 在个人岛上精炼(rrr.island)
//	max_age_hours            数据最大年龄(sources.max_age_hours)
//	capital                  本金(daily.capital)
func (s *Server) handleRefineMatrix(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lineKey := strings.TrimSpace(q.Get("line"))
	if lineKey == "" {
		http.Error(w, "缺少 line(ORE / HIDE / FIBER / WOOD)", http.StatusBadRequest)
		return
	}
	line, ok := refine.LineByKey(lineKey)
	if !ok {
		http.Error(w, fmt.Sprintf("line %q 不认识,只有 ORE / HIDE / FIBER / WOOD", lineKey), http.StatusBadRequest)
		return
	}
	city := strings.TrimSpace(q.Get("city"))
	if city == "" {
		city = line.City
	}
	p := s.Flip.RefineDefaults(line.RefinedID(refine.MaxTier, refine.MaxEnchant), city)
	if err := matrixParams(q, &p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx, cancel := contextWithTimeout(r, refineTimeout)
	defer cancel()
	res, err := s.Flip.RefineMatrix(ctx, line.Key, p)
	if err != nil {
		writeRefineError(w, err)
		return
	}
	writeJSON(w, res)
}

// matrixParams 把查询参数写进 p。写错的直接报错,不悄悄用默认值。
func matrixParams(q url.Values, p *refine.Params) error {
	num := func(key string, dst *float64) error {
		v := strings.TrimSpace(q.Get(key))
		if v == "" {
			return nil
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("%s=%q 不是数", key, v)
		}
		*dst = f
		return nil
	}
	integer := func(key string, dst *int) error {
		v := strings.TrimSpace(q.Get(key))
		if v == "" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s=%q 不是整数", key, v)
		}
		*dst = n
		return nil
	}
	for _, f := range []func() error{
		func() error { return integer("qty", &p.Qty) },
		func() error { return num("k", &p.Fees.TransmuteFactor) },
		func() error { return num("station_per100", &p.Fees.StationPer100) },
		func() error { return num("transmute_station_per100", &p.Fees.TransmuteStationPer100) },
		func() error { return num("budget", &p.Focus.Budget) },
		func() error { return integer("unlocked_tier", &p.Focus.UnlockedTier) },
		func() error { return num("daily_bonus", &p.RRR.DailyBonus) },
		func() error { return num("max_age_hours", &p.Sources.MaxAgeHours) },
		func() error { return num("capital", &p.Daily.Capital) },
	} {
		if err := f(); err != nil {
			return err
		}
	}
	if v := strings.TrimSpace(q.Get("fce")); v != "" {
		fce, err := parseFCE(v)
		if err != nil {
			return err
		}
		p.Focus.FCE = fce
	}
	if v := strings.TrimSpace(q.Get("focus")); v != "" {
		p.Focus.Mode = v
	}
	if v := strings.TrimSpace(q.Get("sort")); v != "" {
		p.Sort = v
	}
	switch v := strings.TrimSpace(q.Get("sell")); v {
	case "":
	case refine.SellPost:
		p.Sell.Modes = []string{refine.SellPost, refine.SellTake}
	case refine.SellTake:
		p.Sell.Modes = []string{refine.SellTake, refine.SellPost}
	default:
		return fmt.Errorf("sell=%q 不认识,只有 post / take", v)
	}
	switch v := strings.TrimSpace(q.Get("island")); v {
	case "", "0", "false":
	case "1", "true":
		p.RRR.Island = true
	default:
		return fmt.Errorf("island=%q 只能是 0 / 1", v)
	}
	return nil
}

// parseFCE:「40000」表示 T4~T8 都是 40000;「4:0,5:1000,8:25000」逐 tier 给,没写的 tier 按 0。
func parseFCE(v string) (map[int]float64, error) {
	out := map[int]float64{}
	if !strings.Contains(v, ":") {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("fce=%q 不是数,也不是 4:0,5:1000 这种写法", v)
		}
		for t := refine.MinEnchantTier; t <= refine.MaxTier; t++ {
			out[t] = f
		}
		return out, nil
	}
	for _, part := range strings.Split(v, ",") {
		ts, fs, ok := strings.Cut(strings.TrimSpace(part), ":")
		t, err1 := strconv.Atoi(strings.TrimSpace(ts))
		f, err2 := strconv.ParseFloat(strings.TrimSpace(fs), 64)
		if !ok || err1 != nil || err2 != nil || t < refine.MinEnchantTier || t > refine.MaxTier {
			return nil, fmt.Errorf("fce 里的 %q 看不懂:要写成 tier:点数,tier 在 4~8", part)
		}
		out[t] = f
	}
	return out, nil
}

// handleRefineRecipe:GET /api/refine/recipe?item=T8_METALBAR_LEVEL4@4[&k=1.154&transmute_station_per100=500]
func (s *Server) handleRefineRecipe(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	item := strings.TrimSpace(q.Get("item"))
	if item == "" {
		http.Error(w, "缺少 item", http.StatusBadRequest)
		return
	}
	opt := func(key string) (*float64, error) {
		v := strings.TrimSpace(q.Get(key))
		if v == "" {
			return nil, nil
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return nil, fmt.Errorf("%s=%q 不是数", key, v)
		}
		return &f, nil
	}
	k, err := opt("k")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	fee, err := opt("transmute_station_per100")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	res, err := s.Flip.RefineRecipe(item, k, fee)
	if err != nil {
		writeRefineError(w, err)
		return
	}
	writeJSON(w, res)
}

// writeRefineError:参数不合法 400;等太久 504(计算还在后台跑);其余 500。
func writeRefineError(w http.ResponseWriter, err error) {
	var pe *flip.ParamError
	switch {
	case errors.As(err, &pe):
		http.Error(w, pe.Error(), http.StatusBadRequest)
	case errors.Is(err, context.DeadlineExceeded):
		http.Error(w, "等了 "+refineTimeout.String()+" 还没算完(多半在等 AODP 限流),计算在后台继续,过几秒再请求一次",
			http.StatusGatewayTimeout)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
