package scan

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/model"
	"albion-guild/internal/screen"
)

// fakeBooks 只返回被请求到的 key,和 store.BookSides 的约定一致。
type fakeBooks struct {
	mu        sync.Mutex
	data      map[model.QuoteKey]CapturedSide
	err       error
	calls     int
	lastKeys  []model.QuoteKey
	lastSince time.Time
}

func (f *fakeBooks) CaptureBooks(_ context.Context, keys []model.QuoteKey, since time.Time) (map[model.QuoteKey]CapturedSide, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastKeys, f.lastSince = keys, since
	if f.err != nil {
		return nil, f.err
	}
	out := map[model.QuoteKey]CapturedSide{}
	for _, k := range keys {
		if cs, ok := f.data[k]; ok {
			out[k] = cs
		}
	}
	return out, nil
}

// fakeAODP 和 scan_test 里 run() 用的是同一份响应,另外记下每次请求的路径。
type fakeAODP struct {
	srv   *httptest.Server
	mu    sync.Mutex
	paths []string
}

func newFakeAODP(t *testing.T) *fakeAODP {
	t.Helper()
	f := &fakeAODP{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		var payload any = prices()
		if strings.Contains(r.URL.Path, "/history/") {
			payload = []map[string]any{
				history("T5_CLOTH", 600, 1200),
				history("T5_WOOD", 50_000, 1050),
				history("T5_ORE", 50_000, 1050),
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeAODP) requested() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func captureCfg() conf.Config {
	cfg := conf.Default()
	cfg.Cities = []string{"Lymhurst"}
	cfg.Capital = 10_000_000
	// 三个物品都进扫描清单:假 AODP 本来就三行都回,这样读簿才会请求到 T5_WOOD
	cfg.Items.Patterns = []string{"T5_CLOTH", "T5_WOOD", "T5_ORE"}
	cfg.Capture.Enabled = true
	return cfg
}

func testCatalog() *catalog.Catalog {
	return catalog.New([]catalog.Item{
		{ItemID: "T5_CLOTH", NameZH: "精布", NameEN: "Ornate Cloth"},
		{ItemID: "T5_WOOD", NameZH: "杉木", NameEN: "Cedar Logs"},
		{ItemID: "T5_ORE", NameZH: "钛矿石", NameEN: "Titanium Ore"},
	}, nil, "")
}

func runCapture(t *testing.T, cfg conf.Config, books BookSource) *Result {
	t.Helper()
	f := newFakeAODP(t)
	res, err := RunWithCapture(context.Background(), aodp.New(f.srv.URL, cfg.API), cfg, testCatalog(), books, now)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func woodBooks() *fakeBooks {
	return &fakeBooks{data: map[model.QuoteKey]CapturedSide{
		askKey("T5_WOOD", "Lymhurst"): side(now.Add(-10*time.Minute),
			depth.Level{Price: 1110, Qty: 500}, depth.Level{Price: 1111, Qty: 300}),
	}}
}

func findOpp(res *Result, item string) *screen.Opportunity {
	for i := range res.Opportunities {
		if res.Opportunities[i].ItemID == item {
			return &res.Opportunities[i]
		}
	}
	return nil
}

func TestRunWithCapture_卖方抓包盖到机会上(t *testing.T) {
	books := woodBooks()
	res := runCapture(t, captureCfg(), books)

	o := findOpp(res, "T5_WOOD")
	if o == nil {
		t.Fatalf("T5_WOOD 应在榜上,拒绝: %+v", res.Rejected)
	}
	if o.Ask.Source != screen.SourceCapture || o.SellPrice != 1110 {
		t.Fatalf("卖价应取抓包 1110,得到 %s / %d", o.Ask.Source, o.SellPrice)
	}
	if math.Abs(o.SellAgeHours-1.0/6) > 1e-9 || o.DataAgeHours != 1.0 {
		t.Fatalf("卖方数据龄应 0.167h、整体取较旧的 AODP 买方 1h,得到 %v / %v", o.SellAgeHours, o.DataAgeHours)
	}
	if o.Bid.Source != screen.SourceAODP {
		t.Fatalf("买方仍是 AODP,得到 %+v", o.Bid)
	}
	if res.RequestCount != 2 {
		t.Fatalf("读簿不打 AODP,请求数应仍为 2,得到 %d", res.RequestCount)
	}
	if !books.lastSince.Equal(now.Add(-6*time.Hour)) || len(books.lastKeys) != 3*1*1*2 {
		t.Fatalf("读簿窗口应为 now−6h、6 个 key,得到 %v / %d", books.lastSince, len(books.lastKeys))
	}
	c := res.Capture
	if !c.Enabled || c.RequestedKeys != 6 || c.AsksUsed != 1 || c.BookSides != 1 || c.Error != "" {
		t.Fatalf("抓包汇总不对: %+v", c)
	}
	if cov := res.Coverage[0]; cov.CaptureSides != 1 || cov.CaptureUsed != 1 || !cov.CaptureHasMedian ||
		math.Abs(cov.CaptureMedianAgeHours-1.0/6) > 1e-9 || cov.WithData != 6 {
		t.Fatalf("覆盖率:AODP 口径不变(6 个点),抓包另列 1 边,得到 %+v", cov)
	}
}

// 读簿失败不让整次扫描白跑:报进 capture.error,其余和纯 AODP 完全相同
func TestRunWithCapture_读簿失败退回纯AODP(t *testing.T) {
	cfg := captureCfg()
	failed := runCapture(t, cfg, &fakeBooks{err: errors.New("库挂了")})
	if failed.Capture.Error != "库挂了" || !failed.Capture.Enabled {
		t.Fatalf("应报出读簿错误,得到 %+v", failed.Capture)
	}
	plain := runCapture(t, cfg, nil)
	failed.Capture = CaptureSummary{}
	a, _ := json.Marshal(failed)
	b, _ := json.Marshal(plain)
	if string(a) != string(b) {
		t.Fatalf("除了 capture 段应和纯 AODP 相同\n失败 %s\n纯   %s", a, b)
	}
}

func TestRunWithCapture_开关关着时和Run逐字相同(t *testing.T) {
	cfg := captureCfg()
	cfg.Capture.Enabled = false
	books := woodBooks()
	with := runCapture(t, cfg, books)
	f := newFakeAODP(t)
	plain, err := Run(context.Background(), aodp.New(f.srv.URL, cfg.API), cfg, testCatalog(), now)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(with)
	b, _ := json.Marshal(plain)
	if string(a) != string(b) {
		t.Fatalf("开关关着时应和 Run 相同\nwith %s\nrun  %s", a, b)
	}
	if books.calls != 0 {
		t.Fatalf("开关关着时不该读簿,读了 %d 次", books.calls)
	}
}

// client 是全服务共用的,请求数要报本次的增量,不能把之前的累计进来
func TestRunWithCapture_请求数是本次增量(t *testing.T) {
	cfg := captureCfg()
	f := newFakeAODP(t)
	client := aodp.New(f.srv.URL, cfg.API)
	for i := 0; i < 2; i++ {
		res, err := RunWithCapture(context.Background(), client, cfg, testCatalog(), nil, now)
		if err != nil {
			t.Fatal(err)
		}
		if res.RequestCount != 2 {
			t.Fatalf("第 %d 次扫描请求数应为 2,得到 %d", i+1, res.RequestCount)
		}
	}
}
