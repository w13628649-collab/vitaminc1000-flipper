package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
//	POST /api/refine/qtyscan  请求体同上;按一串件数各算一遍,找这一批利润最高的件数
//	GET  /api/refine/matrix   一条线 25 个档位各取最好的一条
//	GET  /api/refine/recipe   配方树和转化格子

const (
	// refineTimeout 是 handler 最多等多久。数据快照热的时候只算引擎(几十毫秒,矩阵约 100 毫秒);
	// 冷启动要读库、最多等 AODP 3 秒(refineSnapColdAODPWait)。到点先回 504,
	// 快照和计算留在后台跑完进缓存,过两秒再点就有了
	refineTimeout = 30 * time.Second
	// refineMaxBody:请求体就是一份参数,几百字节;挡一下误传的大文件
	refineMaxBody = 64 << 10
)

func (s *Server) registerRefine(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/refine/plan", s.handleRefinePlan)
	mux.HandleFunc("POST /api/refine/qtyscan", s.handleRefineQtyScan)
	mux.HandleFunc("GET /api/refine/matrix", s.handleRefineMatrix)
	mux.HandleFunc("GET /api/refine/recipe", s.handleRefineRecipe)
}

// refineRequest 解码精炼计算的请求体(handleRefinePlan、handleRefineQtyScan 共用)。
// 不合法时已经写好了 400,返回 ok=false
func (s *Server) refineRequest(ctx context.Context, w http.ResponseWriter, r *http.Request) (p refine.Params, fee flip.StationFeeInfo, opts flip.RefineOpts, ok bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, refineMaxBody))
	if err != nil {
		http.Error(w, "请求体读取失败: "+err.Error(), http.StatusBadRequest)
		return p, fee, opts, false
	}
	// 站费两项只看「请求里写没写」:写了就以请求为准,没写才轮到抓包 / 配置(见 flip.SettleStationFee)
	var probe struct {
		Target string `json:"target"`
		City   string `json:"city"`
		Fees   struct {
			StationPer100          *float64 `json:"station_per100"`
			TransmuteStationPer100 *float64 `json:"transmute_station_per100"`
		} `json:"fees"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		http.Error(w, "请求体不是合法的 JSON: "+err.Error(), http.StatusBadRequest)
		return p, fee, opts, false
	}
	target := strings.TrimSpace(probe.Target)
	if target == "" {
		http.Error(w, "缺少 target(要做的半成品,例如 T8_METALBAR_LEVEL4@4)", http.StatusBadRequest)
		return p, fee, opts, false
	}
	p, fee = s.Flip.RefineDefaultsLive(ctx, target, strings.TrimSpace(probe.City))
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		http.Error(w, "请求体解析失败: "+err.Error(), http.StatusBadRequest)
		return p, fee, opts, false
	}
	p.Target, p.City = target, strings.TrimSpace(p.City)
	if p.City == "" {
		p.City = s.Flip.RefineDefaults(target, "").City
	}
	flip.SettleStationFee(&p, &fee, probe.Fees.StationPer100 != nil, probe.Fees.TransmuteStationPer100 != nil)
	opts, err = refineOpts(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return p, fee, opts, false
	}
	return p, fee, opts, true
}

// handleRefinePlan:POST /api/refine/plan。
//
// 请求体解码两遍:第一遍只取 target 和 city,拿它们向服务端要打底的参数
// (站费按城市、每日加成按线,见 refine.DefaultsFrom);第二遍把整个请求体解码到打底的参数上。
// 请求里没写的字段保持默认值,写了的——包括零值(站费 0、心不返还)——照写的算。
// 不认识的字段直接 400:参数名拼错时悄悄用了默认值,算出来的数看着对、其实不是你要的
func (s *Server) handleRefinePlan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, refineTimeout)
	defer cancel()
	p, fee, opts, ok := s.refineRequest(ctx, w, r)
	if !ok {
		return
	}
	res, err := s.Flip.RefineWith(ctx, p, opts)
	if err != nil {
		writeRefineError(w, err)
		return
	}
	// 结果是共享缓存:站费来源按这次请求填进副本
	out := *res
	out.StationFee = fee
	writeJSON(w, &out)
}

// handleRefineQtyScan:POST /api/refine/qtyscan,请求体和 /api/refine/plan 一样,qty 当作「现在的件数」。
func (s *Server) handleRefineQtyScan(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, refineTimeout)
	defer cancel()
	p, _, opts, ok := s.refineRequest(ctx, w, r)
	if !ok {
		return
	}
	res, err := s.Flip.RefineQtyScan(ctx, p, opts)
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
//	sell_city                出售城市(sell.city),默认同生产城市
//	confirmed                核对过的待核实参数,逗号分隔(confirmed)
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
	ctx, cancel := contextWithTimeout(r, refineTimeout)
	defer cancel()
	p, fee := s.Flip.RefineDefaultsLive(ctx, line.RefinedID(refine.MaxTier, refine.MaxEnchant), city)
	if err := matrixParams(q, &p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	reqStation := strings.TrimSpace(q.Get("station_per100")) != ""
	reqTransmute := strings.TrimSpace(q.Get("transmute_station_per100")) != ""
	flip.SettleStationFee(&p, &fee, reqStation, reqTransmute)
	opts, err := refineOpts(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 请求没写站费:每格按自己的 tier 取能炼它的最便宜那台。fee 是按 T8 挑的,城里只有低级站时
	// 它退回配置,但 ByTier 里 T4~T7 照样有抓到的费率,不能整张表跟着退(和单个目标的卡片对上)
	if !reqStation && len(fee.ByTier) > 0 {
		opts.StationByTier = fee.ByTier
	}
	if !reqTransmute && s.Flip.Cfg.Refining.StationFeeTransmute && len(fee.ByTier) > 0 {
		opts.TransmuteByTier = fee.ByTier
	}
	res, err := s.Flip.RefineMatrixWith(ctx, line.Key, p, opts)
	if err != nil {
		writeRefineError(w, err)
		return
	}
	out := *res
	out.StationFee = fee
	writeJSON(w, &out)
}

// refineOpts:查询参数 after=<RFC3339> 是界面收到的 WS 推送里的服务端推送时刻(Quote.Pushed,
// 晚于那批单入库)。服务端的数据快照比它旧就重建,推送触发的重算不会拿到推送之前读的那份
// (plan 和 matrix 都认)。
//
// after=now 按服务端的现在:断线重连、resync 没有具体推送,以前界面填浏览器本地时钟,
// 成员机器的钟比服务端慢时「快照比它旧」这个判断就失效了
func refineOpts(q url.Values) (flip.RefineOpts, error) {
	var o flip.RefineOpts
	if v := strings.TrimSpace(q.Get("after")); v == "now" {
		o.After = time.Now()
	} else if v != "" {
		t, err := time.Parse(time.RFC3339Nano, v)
		if err != nil {
			return o, fmt.Errorf("after=%q 不是 RFC3339 时刻", v)
		}
		o.After = t
	}
	return o, nil
}

// parseNum 是查询参数里的一个数。ParseFloat 认 "Inf"、"NaN",这里不认:
// +Inf 能混过「>= 0」这种校验,之后缓存键的 json.Marshal 失败,回的是 500 不是 400(审查 finding 9)
func parseNum(key, v string) (float64, error) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%s=%q 不是有限的数", key, v)
	}
	return f, nil
}

// matrixParams 把查询参数写进 p。写错的直接报错,不悄悄用默认值。
func matrixParams(q url.Values, p *refine.Params) error {
	num := func(key string, dst *float64) error {
		v := strings.TrimSpace(q.Get(key))
		if v == "" {
			return nil
		}
		f, err := parseNum(key, v)
		if err != nil {
			return err
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
		func() error { return num("heart_price", &p.Fees.HeartPrice) },
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
	// 精炼页设了出售城市,矩阵不跟着的话,矩阵里当前目标那一格和上面的卡片就是两套数
	// (界面审查 finding 17)。城市认不认识由 checkRefine 按服务端城市列表查
	if v := strings.TrimSpace(q.Get("sell_city")); v != "" {
		p.Sell.City = v
	}
	if v := strings.TrimSpace(q.Get("confirmed")); v != "" {
		p.Confirmed = nil
		for _, k := range strings.Split(v, ",") {
			if k = strings.TrimSpace(k); k != "" {
				p.Confirmed = append(p.Confirmed, k)
			}
		}
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
		f, err := parseNum("fce", v)
		if err != nil {
			return nil, fmt.Errorf("fce=%q 不是有限的数,也不是 4:0,5:1000 这种写法", v)
		}
		for t := refine.MinEnchantTier; t <= refine.MaxTier; t++ {
			out[t] = f
		}
		return out, nil
	}
	for _, part := range strings.Split(v, ",") {
		ts, fs, ok := strings.Cut(strings.TrimSpace(part), ":")
		t, err1 := strconv.Atoi(strings.TrimSpace(ts))
		f, err2 := parseNum("fce", strings.TrimSpace(fs))
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
		f, err := parseNum(key, v)
		if err != nil {
			return nil, err
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
