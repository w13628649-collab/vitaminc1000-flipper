package api

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/book"
	"albion-guild/internal/conf"
	"albion-guild/internal/flip"
	"albion-guild/internal/model"
	"albion-guild/internal/refine"
	"albion-guild/internal/store"
)

var updateShapes = flag.Bool("update", false, "重写 testdata 里精炼接口的 JSON 形状快照")

// refineSrc 是 handler 测试用的假数据源(flip.RefineSource):不连库、不联网。
// 数据都是这里造的,不是用户的抓包。
type refineSrc struct {
	mu     sync.Mutex
	orders []store.LiveOrder
	prices []aodp.PriceRecord
	days   []store.ItemDay
	calls  int
}

func (f *refineSrc) BookOrders(_ context.Context, keys []model.QuoteKey, since time.Time) ([]store.LiveOrder, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	type scope struct {
		item, city string
		side       model.Side
	}
	want := map[scope]bool{}
	for _, k := range keys {
		want[scope{k.ItemID, k.LocationID, k.Side}] = true
	}
	var in []store.LiveOrder
	for _, o := range f.orders {
		if want[scope{o.ItemID, o.City, o.Side}] && o.LastSeen.After(since) {
			in = append(in, o)
		}
	}
	return book.WithPages(in), nil
}

func (f *refineSrc) AODPPrices(context.Context, []string, []string) ([]aodp.PriceRecord, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	return f.prices, nil
}

func (f *refineSrc) DailyVolume(_ context.Context, items []string, city string, _ int,
	first, last time.Time) ([]store.ItemDay, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	var out []store.ItemDay
	for _, d := range f.days {
		if slices.Contains(items, d.ItemID) && d.City == city && !d.Day.Before(first) && !d.Day.After(last) {
			out = append(out, d)
		}
	}
	return out, nil
}

func (f *refineSrc) add(item, city string, side model.Side, seen time.Time, pairs ...int64) {
	for i := 0; i+1 < len(pairs); i += 2 {
		f.orders = append(f.orders, store.LiveOrder{ItemID: item, City: city, Quality: 1, Side: side,
			Price: pairs[i], Amount: pairs[i+1], FirstSeen: seen, LastSeen: seen})
	}
}

// fullSrc 给矿线 T2~T8 在 Thetford 造一套完整的盘口(价格大致按 tier / 附魔翻倍,
// 买单第三档是断崖),T3 条故意不造抓包、只给 AODP 价(深度未知),T5.1 矿另有一条
// 比抓包新、价不同的 AODP,再加成交量:各种字段都能在响应里出现。
func fullSrc(now time.Time) *refineSrc {
	f := &refineSrc{}
	l := refine.Lines[0]
	n := 0
	for t := refine.MinTier; t <= refine.MaxTier; t++ {
		for _, e := range refine.Enchants(t) {
			raw := 40 * math.Pow(2.2, float64(t-4)) * math.Pow(2.6, float64(e))
			for _, it := range []struct {
				id   string
				fair float64
			}{{l.RawID(t, e), raw}, {l.RefinedID(t, e), raw * 3.2}} {
				if it.id == "T3_METALBAR" {
					continue
				}
				// 每个物品各是一次响应(时间戳错开),不然几十个物品挤在同一个时间戳里
				// 会被 book.Build 数成一个满页、全判成截断
				n++
				seen := now.Add(-time.Hour - time.Duration(n)*time.Second)
				p := int64(math.Max(20, math.Round(it.fair)))
				f.add(it.id, "Thetford", model.SideOffer, seen, p, 30, p+p/20+1, 60, p+p/10+2, 400)
				f.add(it.id, "Thetford", model.SideRequest, seen, p*8/10, 40, p*7/10, 80, p/10, 500)
			}
		}
	}
	f.add(l.Heart, "Thetford", model.SideOffer, now.Add(-90*time.Minute), 2, 500)
	f.prices = []aodp.PriceRecord{
		{ItemID: "T3_METALBAR", City: "Thetford", Quality: 1,
			SellPriceMin: 30, SellPriceMinDate: aodp.Stamp{T: now.Add(-3 * time.Hour)}},
		{ItemID: "T5_ORE_LEVEL1@1", City: "Thetford", Quality: 1,
			SellPriceMin: 90, SellPriceMinDate: aodp.Stamp{T: now.Add(-5 * time.Minute)}},
	}
	first, _ := rank14(now)
	for i := 0; i < 14; i += 2 {
		for _, target := range l.Targets() {
			f.days = append(f.days, store.ItemDay{ItemID: target, City: "Thetford",
				Day: first.AddDate(0, 0, i), ItemCount: 12, AvgPrice: 1000, Source: store.SourceAODP})
		}
	}
	return f
}

// rank14 是成交量窗口的第一天(和 rank.Window(now, 14) 一样:不含今天,往前数 14 天)。
func rank14(now time.Time) (time.Time, time.Time) {
	last := now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	return last.AddDate(0, 0, -13), last
}

func refineServer(src *refineSrc, opts ...func(*flip.Service)) *httptest.Server {
	fl := flip.New(nil, conf.Default())
	fl.RefineSrc = src
	for _, o := range opts {
		o(fl)
	}
	return httptest.NewServer(New(nil, nil, nil, 30*time.Minute, fl).Routes())
}

// someConflicts 让几个盘口边「最近串过城」。
type someConflicts map[model.QuoteKey]bool

// ConflictCoverageSince:和 *ingest.Ingestor 一样报串城判定覆盖到多早(books.conflicts_since)。
func (c someConflicts) ConflictCoverageSince(now time.Time) time.Time {
	return now.Add(-24 * time.Hour)
}

func (c someConflicts) ConflictedSince(keys []model.QuoteKey, since time.Time) map[model.QuoteKey]time.Time {
	out := map[model.QuoteKey]time.Time{}
	for _, k := range keys {
		if c[k] {
			out[k] = since.Add(time.Minute)
		}
	}
	return out
}

func postPlan(t *testing.T, srv *httptest.Server, body string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/refine/plan", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return resp, readBody(t, resp)
}

func readBody(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func get(t *testing.T, srv *httptest.Server, path string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	return resp, readBody(t, resp)
}

// ── 参数校验 ───────────────────────────────────────────────────────────

func TestRefine接口参数不合法回400且不读数据(t *testing.T) {
	src := fullSrc(time.Now())
	srv := refineServer(src)
	defer srv.Close()

	for _, body := range []string{
		`{`,
		`{}`,
		`{"target":" "}`,
		`{"target":"T8_ORE_LEVEL4@4"}`,   // 原料不是精炼产物
		`{"target":"T8_MAIN_FIRESTAFF"}`, // 装备以后再说
		`{"target":"T6_STONEBLOCK"}`,     // 石头线本期不做
		`{"target":"T3_METALBAR"}`,       // T3 只当链底的输入
		`{"target":"T8_METALBAR_LEVEL4@4","qty":0}`,
		`{"target":"T8_METALBAR_LEVEL4@4","qtty":3}`, // 字段名拼错不能悄悄用默认值
		`{"target":"T8_METALBAR_LEVEL4@4","city":"Atlantis"}`,
		`{"target":"T8_METALBAR_LEVEL4@4","sell":{"city":"Atlantis"}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","focus":{"fce":{"ORE":{"4":0}}}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","focus":{"fce":{"8":50000}}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","focus":{"budget":40000}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","fees":{"transmute_factor":115.4}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","fees":{"station_per100":1200}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","sources":{"max_age_hours":100}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","sell":{"modes":["hold"]}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","leftover":"sell"}`,
		// 游戏里没有的附魔、不规范的 id(审查 finding 7)
		`{"target":"T8_METALBAR_LEVEL5@5"}`,
		`{"target":"T8_METALBAR_LEVEL9@9"}`,
		`{"target":"T8_METALBAR_LEVEL0@0"}`,
		// 数值边界(审查 finding 9)
		`{"target":"T8_METALBAR_LEVEL4@4","fees":{"undercut":-9000000000000000000}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","fees":{"outbid":-1}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","focus":{"fce":{"99":1000}}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","daily":{"turnover_days":1e300}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","sources":{"bid_max_qty":-1}}`,
	} {
		resp, raw := postPlan(t, srv, body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: 状态 %d,期望 400(%s)", body, resp.StatusCode, raw)
		}
	}
	for _, path := range []string{
		"/api/refine/matrix",
		"/api/refine/matrix?line=ROCK",
		"/api/refine/matrix?line=ORE&qty=abc",
		"/api/refine/matrix?line=ORE&qty=0",
		"/api/refine/matrix?line=ORE&fce=4:x",
		"/api/refine/matrix?line=ORE&fce=9:100",
		"/api/refine/matrix?line=ORE&focus=custom",
		"/api/refine/matrix?line=ORE&sell=hold",
		"/api/refine/matrix?line=ORE&island=2",
		"/api/refine/matrix?line=ORE&city=Atlantis",
		"/api/refine/matrix?line=ORE&k=115.4",
		// ParseFloat 认 Inf / NaN:+Inf 以前混过校验,在缓存键的 json.Marshal 上变成 500
		"/api/refine/matrix?line=ORE&capital=Inf",
		"/api/refine/matrix?line=ORE&capital=NaN",
		"/api/refine/matrix?line=ORE&budget=Inf",
		"/api/refine/matrix?line=ORE&fce=Inf",
		"/api/refine/matrix?line=ORE&fce=8:NaN",
		"/api/refine/matrix?line=ORE&max_age_hours=Inf",
		"/api/refine/recipe",
		"/api/refine/recipe?item=T8_MAIN_FIRESTAFF",
		"/api/refine/recipe?item=T8_ORE&k=abc",
		"/api/refine/recipe?item=T8_ORE&k=115.4",
		"/api/refine/recipe?item=T8_ORE&k=Inf",
		"/api/refine/recipe?item=T8_ORE_LEVEL7@7",
		"/api/refine/recipe?item=T8_ORE_LEVEL0@0",
	} {
		resp, raw := get(t, srv, path)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: 状态 %d,期望 400(%s)", path, resp.StatusCode, raw)
		}
	}
	if src.calls != 0 {
		t.Fatalf("参数不合法不该读数据,调了 %d 次", src.calls)
	}
	// 方法不对。GET 落到 "GET /" 的界面文件服务上是 404,所以这里用 PUT / POST 测
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/refine/plan", strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("plan 只接受 POST,得到 %d", resp.StatusCode)
	}
	resp, err = http.Post(srv.URL+"/api/refine/matrix?line=ORE", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("matrix 只接受 GET,得到 %d", resp.StatusCode)
	}
}

// 缺价、深度不够都不是错误:200,原因写在 warnings 里
func TestRefine接口缺价和深度不够回200带警告(t *testing.T) {
	now := time.Now()
	src := &refineSrc{}
	seen := now.Add(-2 * time.Hour)
	src.add("T8_METALBAR_LEVEL4@4", "Thetford", model.SideOffer, seen, 3_000_000, 5)
	src.add("T8_ORE_LEVEL4@4", "Thetford", model.SideOffer, seen, 1_000_000, 2) // 要 23 个,只有 2 个
	srv := refineServer(src)
	defer srv.Close()

	resp, raw := postPlan(t, srv, `{"target":"T8_METALBAR_LEVEL4@4","qty":10}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态 %d: %s", resp.StatusCode, raw)
	}
	var res struct {
		Warnings   []refine.Warning `json:"warnings"`
		Data       refine.DataInfo  `json:"data"`
		Candidates []struct {
			Filled   bool             `json:"filled"`
			Warnings []refine.Warning `json:"warnings"`
		} `json:"candidates"`
		Books flip.RefineBooks `json:"books"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, w := range res.Warnings {
		codes[w.Code] = true
	}
	for _, c := range res.Candidates {
		if c.Filled {
			t.Fatal("料不够,不该有拿满的候选")
		}
		for _, w := range c.Warnings {
			codes[w.Code] = true
		}
	}
	for _, want := range []string{refine.WarnDepthShort, refine.WarnMissing, refine.WarnNoVolume, refine.WarnNoSellPrice} {
		if !codes[want] {
			t.Errorf("应有 %s 警告,得到 %v", want, codes)
		}
	}
	if len(res.Data.Missing) == 0 || res.Books.Capture != 2 {
		t.Fatalf("data %+v books %+v", res.Data, res.Books)
	}
}

// 请求里写了的字段照写的算,零值也一样;没写的用服务端默认值
func TestRefine接口个人参数照请求算(t *testing.T) {
	srv := refineServer(fullSrc(time.Now()))
	defer srv.Close()
	resp, raw := postPlan(t, srv, `{"target":"T6_METALBAR_LEVEL2@2","city":"Martlock",
		"fees":{"station_per100":0},"heart_returns":false,"focus":{"fce":{"6":10000},"budget":20000}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态 %d: %s", resp.StatusCode, raw)
	}
	var res struct {
		ParamsUsed refine.ParamsUsed `json:"params_used"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	p := res.ParamsUsed
	if p.City != "Martlock" || p.Sell.City != "Martlock" || !slices.Equal(p.Sources.Cities, []string{"Martlock"}) {
		t.Fatalf("城市: %+v %+v %+v", p.City, p.Sell, p.Sources)
	}
	if p.Fees.StationPer100 != 0 || p.HeartReturns || p.Focus.FCE[6] != 10000 || p.Focus.Budget != 20000 {
		t.Fatalf("写了的零值不能被换成默认值: %+v heart=%v %+v", p.Fees, p.HeartReturns, p.Focus)
	}
	if p.Fees.TransmuteFactor != 1.154 || p.Qty != 10 || p.Sources.MaxAgeHours != 72 {
		t.Fatalf("没写的用默认值: %+v qty=%d %+v", p.Fees, p.Qty, p.Sources)
	}
	for _, u := range p.Unverified {
		if u.Key == "fees.station_per100" && u.IsDefault {
			t.Fatal("站费改成 0 了,不该标成默认值")
		}
	}
}

// ── JSON 形状快照 ───────────────────────────────────────────────────────

// 界面照着这些字段写。形状 = 每个字段的路径和类型(数组元素合并成 []),不管具体数值,
// 所以价格年龄随钟变也不影响。改了响应的字段就会在这里露出来:确认是有意的,再用
//
//	go test ./internal/api -run TestRefine接口JSON形状 -update
//
// 重写快照,并同步告诉界面线。
func TestRefine接口JSON形状(t *testing.T) {
	now := time.Now()
	srv := refineServer(fullSrc(now), func(fl *flip.Service) {
		fl.Conflicts = someConflicts{{ItemID: "T5_METALBAR_LEVEL1@1", LocationID: "Thetford", Quality: 1, Side: model.SideOffer}: true}
	})
	defer srv.Close()

	// plan 的形状取几种请求的并集:数据齐全(挂买单收料、串城、AODP 更新)、吃买单出货撞断崖、
	// 数据全过期、整条线没数据 —— 可选字段都能露出来
	planShape := map[string]map[string]bool{}
	for _, body := range []string{
		`{"target":"T5_METALBAR_LEVEL1@1","qty":10,"sources":{"bid_orders":true}}`,
		`{"target":"T8_METALBAR_LEVEL4@4","qty":600,"sell":{"modes":["take","post"]},"leftover":"bid","daily":{"capital":0}}`,
		`{"target":"T6_METALBAR_LEVEL4@4","qty":10,"sources":{"max_age_hours":0.5}}`,
		`{"target":"T8_LEATHER_LEVEL4@4","qty":10}`,
	} {
		resp, raw := postPlan(t, srv, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: 状态 %d %s", body, resp.StatusCode, raw)
		}
		addShape(t, planShape, raw)
	}
	checkShape(t, "refine_plan.shape", planShape)

	resp, raw := get(t, srv, "/api/refine/matrix?line=ORE&city=Thetford&qty=10&fce=4:0,8:10000&budget=12000")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("matrix: 状态 %d %s", resp.StatusCode, raw)
	}
	var m flip.RefineMatrix
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Cells) != 25 || m.ParamsUsed.Focus.FCE[8] != 10000 || m.ParamsUsed.Focus.Budget != 12000 {
		t.Fatalf("矩阵: %d 格,参数 %+v", len(m.Cells), m.ParamsUsed.Focus)
	}
	matrixShape := map[string]map[string]bool{}
	addShape(t, matrixShape, raw)
	checkShape(t, "refine_matrix.shape", matrixShape)

	resp, raw = get(t, srv, "/api/refine/recipe?item=T5_METALBAR_LEVEL1@1&k=1.0")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("recipe: 状态 %d %s", resp.StatusCode, raw)
	}
	recipeShape := map[string]map[string]bool{}
	addShape(t, recipeShape, raw)
	checkShape(t, "refine_recipe.shape", recipeShape)
}

// dynamicMaps 是键本身是数据的对象(物品 id、tier):形状里写成 *。
var dynamicMaps = []string{".raw_sources", ".fce", "].value", ".daily_bonus", ".station_fee_by_city"}

func addShape(t *testing.T, out map[string]map[string]bool, raw []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("响应不是 JSON: %v\n%s", err, raw)
	}
	walkShape(v, "$", out)
}

func walkShape(v any, path string, out map[string]map[string]bool) {
	note := func(typ string) {
		if out[path] == nil {
			out[path] = map[string]bool{}
		}
		out[path][typ] = true
	}
	switch x := v.(type) {
	case map[string]any:
		note("object")
		dynamic := false
		for _, suffix := range dynamicMaps {
			dynamic = dynamic || strings.HasSuffix(path, suffix)
		}
		for k, e := range x {
			if dynamic {
				k = "*"
			}
			walkShape(e, path+"."+k, out)
		}
	case []any:
		note("array")
		for _, e := range x {
			walkShape(e, path+"[]", out)
		}
	case string:
		note("string")
	case float64:
		note("number")
	case bool:
		note("bool")
	case nil:
		note("null")
	}
}

func renderShape(shape map[string]map[string]bool) string {
	paths := make([]string, 0, len(shape))
	for p := range shape {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var b strings.Builder
	for _, p := range paths {
		types := make([]string, 0, len(shape[p]))
		for t := range shape[p] {
			types = append(types, t)
		}
		sort.Strings(types)
		fmt.Fprintf(&b, "%s: %s\n", p, strings.Join(types, "|"))
	}
	return b.String()
}

func checkShape(t *testing.T, name string, shape map[string]map[string]bool) {
	t.Helper()
	got := renderShape(shape)
	file := filepath.Join("testdata", name)
	if *updateShapes {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("读快照 %s: %v(第一次跑用 -update 生成)", file, err)
	}
	want := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if got != want {
		gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
		var diff []string
		for _, l := range gl {
			if !slices.Contains(wl, l) {
				diff = append(diff, "+ "+l)
			}
		}
		for _, l := range wl {
			if !slices.Contains(gl, l) {
				diff = append(diff, "- "+l)
			}
		}
		t.Fatalf("%s 的 JSON 形状变了(+ 新出现 / - 不见了):\n%s", name, strings.Join(diff, "\n"))
	}
}
