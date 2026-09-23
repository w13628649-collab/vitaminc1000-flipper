package flip

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/model"
	"albion-guild/internal/scan"
	"albion-guild/internal/screen"
)

// rvMarket 是假 AODP 上一个物品在 Lymhurst 的价和成交。
type rvMarket struct {
	sell, buy      int64
	histQty, histP int64
}

var rvMarkets = map[string]rvMarket{
	"T5_CLOTH": {1450, 1000, 600, 1200},
	"T5_WOOD":  {1120, 1000, 50_000, 1050},
	"T5_ORE":   {40_000, 1000, 50_000, 1050}, // troll 卖单,上不了榜
}

// rvAODP 只回请求到的物品,时间戳按真实时钟算(Service 用 time.Now)。
type rvAODP struct {
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []string
	fail atomic.Bool // 为 true 时一律 500
}

func newRVAODP(t *testing.T) *rvAODP {
	t.Helper()
	a := &rvAODP{}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.reqs = append(a.reqs, r.URL.Path)
		a.mu.Unlock()
		if a.fail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		ids := strings.Split(strings.TrimSuffix(path.Base(r.URL.Path), ".json"), ",")
		now := time.Now().UTC()
		var payload []map[string]any
		for _, id := range ids {
			m, ok := rvMarkets[id]
			if !ok {
				continue
			}
			if strings.Contains(r.URL.Path, "/history/") {
				var data []map[string]any
				for n := 1; n <= 5; n++ {
					data = append(data, map[string]any{
						"timestamp":  now.AddDate(0, 0, -n).Format("2006-01-02T00:00:00"),
						"item_count": m.histQty, "avg_price": m.histP,
					})
				}
				payload = append(payload, map[string]any{
					"location": "Lymhurst", "item_id": id, "quality": 1, "data": data,
				})
				continue
			}
			at := now.Add(-time.Hour).Format("2006-01-02T15:04:05")
			payload = append(payload, map[string]any{
				"item_id": id, "city": "Lymhurst", "quality": 1,
				"sell_price_min": m.sell, "sell_price_min_date": at,
				"buy_price_max": m.buy, "buy_price_max_date": at,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *rvAODP) requests() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.reqs...)
}

// rvBooks 是可以在测试中途改的抓包来源。
type rvBooks struct {
	mu    sync.Mutex
	data  map[model.QuoteKey]scan.CapturedSide
	items []scan.CapturedItem
}

func (b *rvBooks) CaptureBooks(_ context.Context, keys []model.QuoteKey, _ time.Time) (map[model.QuoteKey]scan.CapturedSide, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[model.QuoteKey]scan.CapturedSide{}
	for _, k := range keys {
		if cs, ok := b.data[k]; ok {
			out[k] = cs
		}
	}
	return out, nil
}

func (b *rvBooks) CapturedItems(context.Context, []string, []int, time.Time) ([]scan.CapturedItem, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]scan.CapturedItem(nil), b.items...), nil
}

func (b *rvBooks) setAsk(item string, price, qty int64, seen time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.data == nil {
		b.data = map[model.QuoteKey]scan.CapturedSide{}
	}
	b.data[model.QuoteKey{ItemID: item, LocationID: "Lymhurst", Quality: 1, Side: model.SideOffer}] = scan.CapturedSide{
		Levels: []depth.Level{{Price: price, Qty: qty}}, LevelSeen: []time.Time{seen},
		Newest: seen, QtyTotal: qty, LevelCount: 1,
	}
}

func (b *rvBooks) setItems(items ...scan.CapturedItem) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = items
}

func rvConfig() conf.Config {
	cfg := conf.Default()
	cfg.Cities = []string{"Lymhurst"}
	cfg.Items.Patterns = []string{"T5_CLOTH"}
	cfg.Capture.Enabled = true
	cfg.API.MaxRetries = 1 // 失败用例不要等退避
	return cfg
}

func newRVService(t *testing.T, cfg conf.Config) (*Service, *rvAODP, *rvBooks) {
	t.Helper()
	a := newRVAODP(t)
	books := &rvBooks{}
	s := New(nil, cfg)
	s.aodp = aodp.New(a.srv.URL, cfg.API)
	s.bookSrc = books
	s.cat.Store(catalog.New([]catalog.Item{
		{ItemID: "T5_CLOTH", NameZH: "精布"}, {ItemID: "T5_WOOD", NameZH: "杉木"}, {ItemID: "T5_ORE", NameZH: "钛矿石"},
	}, nil, ""))
	return s, a, books
}

func rvOpp(res *scan.Result, item string) *screen.Opportunity {
	for i := range res.Opportunities {
		if res.Opportunities[i].ItemID == item {
			return &res.Opportunities[i]
		}
	}
	return nil
}

func TestReevaluate_没有快照时不算(t *testing.T) {
	s, _, _ := newRVService(t, rvConfig())
	if _, err := s.Reevaluate(context.Background()); !errors.Is(err, errNotReady) {
		t.Fatalf("第一次全量之前应报 errNotReady,得到 %v", err)
	}
}

func TestScan_发布的结果不挂成交历史(t *testing.T) {
	s, _, _ := newRVService(t, rvConfig())
	res, err := s.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.History) == 0 {
		t.Fatal("返回给调用方的结果要带历史(入库用)")
	}
	if pub := s.LastScan(); pub.History != nil || pub.StartedAt != res.StartedAt {
		t.Fatalf("发布的那份不挂历史、其余相同,得到 %d 条", len(pub.History))
	}
	if !s.LastScan().StartedAt.Equal(s.snap.Load().FetchedAt) {
		t.Fatal("结果和快照要对得上")
	}
}

// 用户原话:"我抓包数据更新了,机会页面还是使用的老的数据"。全量扫描 30 分钟一次,
// 两次之间翻到的挂单要在一分钟内上板,而且不打 AODP
func TestReevaluate_用缓存快照配新抓包重算不打AODP(t *testing.T) {
	s, a, books := newRVService(t, rvConfig())
	ctx := context.Background()
	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	before := s.LastScan()
	if o := rvOpp(before, "T5_CLOTH"); o == nil || o.SellPrice != 1450 {
		t.Fatalf("全量后应是 AODP 卖价 1450,得到 %+v", o)
	}

	books.setAsk("T5_CLOTH", 1400, 50, time.Now().UTC().Add(-time.Minute))
	res, err := s.Reevaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastScan() != res {
		t.Fatal("重算结果应替换对外结果")
	}
	o := rvOpp(res, "T5_CLOTH")
	if o == nil || o.SellPrice != 1400 || o.Ask.Source != screen.SourceCapture {
		t.Fatalf("重算应用上抓包卖价 1400,得到 %+v", o)
	}
	if n := len(a.requests()); n != 2 || res.RequestCount != 2 {
		t.Fatalf("重算不该打 AODP,累计请求 %d 次", n)
	}
	if !res.StartedAt.Equal(before.StartedAt) || !res.EvaluatedAt.After(res.StartedAt) {
		t.Fatalf("started_at 保持全量时刻、evaluated_at 是重算时刻,得到 %v / %v", res.StartedAt, res.EvaluatedAt)
	}
	if rvOpp(before, "T5_CLOTH").SellPrice != 1450 {
		t.Fatal("旧结果被原地改了:正在读它的请求会看到半新半旧的数据")
	}
}

func TestReevaluate_新抓到的物品节流补拉(t *testing.T) {
	s, a, books := newRVService(t, rvConfig())
	ctx := context.Background()
	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s.LastScan().ItemIDs, ","); got != "T5_CLOTH" {
		t.Fatalf("还没抓到别的物品,得到 %s", got)
	}

	// 全量之后翻到了 T5_WOOD:第一次重算就补拉,只请求新物品
	books.setItems(scan.CapturedItem{ItemID: "T5_WOOD", Qty: 800})
	res, err := s.Reevaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reqs := a.requests()
	if len(reqs) != 4 {
		t.Fatalf("补拉应多 2 次请求(prices + history),累计 %d: %v", len(reqs), reqs)
	}
	for _, p := range reqs[2:] {
		if !strings.Contains(p, "T5_WOOD") || strings.Contains(p, "T5_CLOTH") {
			t.Fatalf("补拉只请求新物品: %s", p)
		}
	}
	if strings.Join(res.ItemIDs, ",") != "T5_CLOTH,T5_WOOD" || strings.Join(res.ExtraItemIDs, ",") != "T5_WOOD" {
		t.Fatalf("补拉的物品应并进快照,得到 %v / %v", res.ItemIDs, res.ExtraItemIDs)
	}
	if o := rvOpp(res, "T5_WOOD"); o == nil {
		t.Fatalf("T5_WOOD 应上榜,拒绝: %+v", res.Rejected)
	}
	if res.Capture.BackfilledAt.IsZero() || res.Capture.ExtraPending != 0 || res.RequestCount != 4 {
		t.Fatalf("汇总应记补拉时刻、无待补、快照累计 4 次请求,得到 %+v / %d", res.Capture, res.RequestCount)
	}

	// 紧接着又翻到 T5_ORE:5 分钟节流内不补,报成待补
	books.setItems(scan.CapturedItem{ItemID: "T5_WOOD", Qty: 800}, scan.CapturedItem{ItemID: "T5_ORE", Qty: 5})
	res, _ = s.Reevaluate(ctx)
	if len(a.requests()) != 4 || res.Capture.ExtraPending != 1 || len(res.ItemIDs) != 2 {
		t.Fatalf("节流期内不该补拉,应报 1 个待补,得到 请求 %d / %+v", len(a.requests()), res.Capture)
	}

	// 过了 5 分钟
	s.lastBackfill.Store(time.Now().Add(-6 * time.Minute).UnixNano())
	res, _ = s.Reevaluate(ctx)
	if len(a.requests()) != 6 || strings.Join(res.ItemIDs, ",") != "T5_CLOTH,T5_WOOD,T5_ORE" || res.Capture.ExtraPending != 0 {
		t.Fatalf("到点后补拉 T5_ORE,得到 请求 %d / %v / %+v", len(a.requests()), res.ItemIDs, res.Capture)
	}

	// 下一次全量直接把抓到的物品带上,和清单塞进同一批请求
	full, err := s.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.requests()) != 8 || strings.Join(full.ExtraItemIDs, ",") != "T5_WOOD,T5_ORE" || full.RequestCount != 2 {
		t.Fatalf("全量应带上抓包物品、只算自己的 2 次请求,得到 请求 %d / %v / %d",
			len(a.requests()), full.ExtraItemIDs, full.RequestCount)
	}
}

func TestReevaluate_补拉失败报进摘要并照样出结果(t *testing.T) {
	s, a, books := newRVService(t, rvConfig())
	ctx := context.Background()
	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	a.fail.Store(true)
	books.setItems(scan.CapturedItem{ItemID: "T5_WOOD", Qty: 800})
	res, err := s.Reevaluate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Capture.Error, "补拉 AODP") || res.Capture.ExtraPending != 1 {
		t.Fatalf("补拉失败应报进 capture.error、物品仍待补,得到 %+v", res.Capture)
	}
	if rvOpp(res, "T5_CLOTH") == nil {
		t.Fatal("补拉失败不影响用旧快照出结果")
	}
	// 失败也算一次:AODP 挂着的时候不能每分钟去撞
	n := len(a.requests())
	_, _ = s.Reevaluate(ctx)
	if len(a.requests()) != n {
		t.Fatal("失败之后 5 分钟内不该重试")
	}
}

// 全量扫描、若干并发重算、若干读者一起跑:最后对外的结果必须是用当前快照算的
// (发布和换快照在同一把锁里)。在 WSL 里用 -race 跑过
func TestReevaluate_并发下结果和快照对得上(t *testing.T) {
	s, _, books := newRVService(t, rvConfig())
	ctx := context.Background()
	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	books.setAsk("T5_CLOTH", 1400, 50, time.Now().UTC().Add(-time.Minute))

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if r := s.LastScan(); r != nil {
					_, _ = json.Marshal(r)
				}
			}
		}()
	}
	var work sync.WaitGroup
	for i := 0; i < 4; i++ {
		work.Add(1)
		go func() {
			defer work.Done()
			for j := 0; j < 5; j++ {
				_, _ = s.Reevaluate(ctx)
			}
		}()
	}
	work.Add(1)
	go func() {
		defer work.Done()
		_, _ = s.Scan(ctx)
	}()
	work.Wait()
	close(stop)
	wg.Wait()

	if !s.LastScan().StartedAt.Equal(s.snap.Load().FetchedAt) {
		t.Fatalf("对外结果的 started_at %v 和当前快照 %v 对不上", s.LastScan().StartedAt, s.snap.Load().FetchedAt)
	}
}

func TestRunReeval_开关关着或间隔为0时直接返回(t *testing.T) {
	for _, mut := range []func(*conf.Config){
		func(c *conf.Config) { c.Capture.Enabled = false },
		func(c *conf.Config) { c.Capture.ReevalSeconds = 0 },
	} {
		cfg := rvConfig()
		mut(&cfg)
		s, _, _ := newRVService(t, cfg)
		done := make(chan struct{})
		go func() { s.RunReeval(context.Background()); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("应立即返回")
		}
	}
}

func TestRunReeval_按间隔重算(t *testing.T) {
	cfg := rvConfig()
	cfg.Capture.ReevalSeconds = 0.05
	s, _, books := newRVService(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := s.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	first := s.LastScan()
	books.setAsk("T5_CLOTH", 1400, 50, time.Now().UTC().Add(-time.Minute))

	done := make(chan struct{})
	go func() { s.RunReeval(ctx); close(done) }()
	deadline := time.After(5 * time.Second)
	for {
		if r := s.LastScan(); r != first {
			if o := rvOpp(r, "T5_CLOTH"); o == nil || o.SellPrice != 1400 {
				t.Fatalf("定时重算应用上抓包价,得到 %+v", o)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("5 秒内没有重算")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后应退出")
	}
}
