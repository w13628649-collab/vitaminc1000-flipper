package scan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"slices"
	"strconv"
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

// qMarket 是假 AODP 上一个 (物品, 品质) 在 Lymhurst 的价和成交。
type qMarket struct {
	sell, buy      int64
	histQty, histP int64
}

// qualityAODP 和真 AODP 一样按 qualities 参数回 物品 × 品质 的笛卡尔积:
// 表里有的给价,没有的回一行全 0(AODP 对没数据的组合就是这样);历史只回有数据的。
type qualityAODP struct {
	srv     *httptest.Server
	mu      sync.Mutex
	reqs    []*url.URL
	markets map[string]qMarket // "物品@品质"
}

func newQualityAODP(t *testing.T, markets map[string]qMarket) *qualityAODP {
	t.Helper()
	a := &qualityAODP{markets: markets}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.reqs = append(a.reqs, r.URL)
		a.mu.Unlock()
		ids := strings.Split(strings.TrimSuffix(path.Base(r.URL.Path), ".json"), ",")
		var quals []int
		for _, s := range strings.Split(r.URL.Query().Get("qualities"), ",") {
			q, _ := strconv.Atoi(s)
			quals = append(quals, q)
		}
		f := fresh()
		var payload []map[string]any
		for _, id := range ids {
			for _, q := range quals {
				m, ok := a.markets[id+"@"+strconv.Itoa(q)]
				if strings.Contains(r.URL.Path, "/history/") {
					if !ok {
						continue
					}
					h := history(id, int(m.histQty), int(m.histP))
					h["quality"] = q
					payload = append(payload, h)
					continue
				}
				row := map[string]any{"item_id": id, "city": "Lymhurst", "quality": q,
					"sell_price_min": 0, "sell_price_min_date": "0001-01-01T00:00:00",
					"buy_price_max": 0, "buy_price_max_date": "0001-01-01T00:00:00"}
				if ok {
					row["sell_price_min"], row["sell_price_min_date"] = m.sell, f
					row["buy_price_max"], row["buy_price_max_date"] = m.buy, f
				}
				payload = append(payload, row)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *qualityAODP) requests() []*url.URL {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]*url.URL(nil), a.reqs...)
}

// 用户原话:"用户抓到的装备良好~不凡品质挂单进不了机会板"。清单里的火法杖配置只扫普通,
// 成员在 Lymhurst 翻到了它的杰出品质挂单,还翻到了清单外一件长袍的良好品质:
// 这两个组合都要向 AODP 拉价和历史、读簿、上机会板;没抓到的组合(长袍普通、法杖良好)不评估
func TestRunWithCapture_抓到的别的品质并进扫描(t *testing.T) {
	markets := map[string]qMarket{
		"T5_CLOTH@1":            {1120, 1000, 50_000, 1050},
		"T6_MAIN_FIRESTAFF@1":   {11_200, 10_000, 5_000, 10_500},
		"T6_MAIN_FIRESTAFF@2":   {12_200, 11_000, 5_000, 11_500}, // 没抓到:AODP 顺带回了也不评估
		"T6_MAIN_FIRESTAFF@4":   {33_600, 30_000, 5_000, 31_500},
		"T6_ARMOR_CLOTH_SET2@1": {9_000, 8_000, 5_000, 8_500}, // 同上
		"T6_ARMOR_CLOTH_SET2@2": {11_200, 10_000, 5_000, 10_500},
	}
	a := newQualityAODP(t, markets)
	cfg := captureCfg()
	cfg.Items.Patterns = []string{"T5_CLOTH", "T6_MAIN_FIRESTAFF"}
	cat := catalog.New([]catalog.Item{
		{ItemID: "T5_CLOTH", NameZH: "精布"}, {ItemID: "T6_MAIN_FIRESTAFF", NameZH: "火焰法杖", MaxQuality: 5},
		{ItemID: "T6_ARMOR_CLOTH_SET2", NameZH: "学者长袍", MaxQuality: 5},
	}, nil, "")
	staff4 := model.QuoteKey{ItemID: "T6_MAIN_FIRESTAFF", LocationID: "Lymhurst", Quality: 4, Side: model.SideOffer}
	books := &fakeBooks{
		items: []CapturedItem{
			{"T6_MAIN_FIRESTAFF", 4, 30}, {"T6_MAIN_FIRESTAFF", 1, 12},
			{"T6_ARMOR_CLOTH_SET2", 2, 9},
		},
		data: map[model.QuoteKey]CapturedSide{
			staff4: side(now.Add(-10*time.Minute), depth.Level{Price: 33_100, Qty: 40}),
		},
	}

	res, err := RunWithCapture(context.Background(), aodp.New(a.srv.URL, cfg.API), cfg, cat, books, now)
	if err != nil {
		t.Fatal(err)
	}

	// AODP:品质正好是配置那一套的一组(精布),其余一组带并集 1,2,4;prices、history 各两批
	reqs := a.requests()
	if len(reqs) != 4 || res.RequestCount != 4 {
		t.Fatalf("应为 2 组 × 2 个端点 = 4 次请求,得到 %d", len(reqs))
	}
	var groups []string
	for _, u := range reqs {
		groups = append(groups, path.Base(u.Path)+"?"+u.Query().Get("qualities"))
	}
	want := []string{"T5_CLOTH.json?1", "T6_MAIN_FIRESTAFF,T6_ARMOR_CLOTH_SET2.json?1,2,4",
		"T5_CLOTH.json?1", "T6_MAIN_FIRESTAFF,T6_ARMOR_CLOTH_SET2.json?1,2,4"}
	if !slices.Equal(groups, want) {
		t.Fatalf("请求分组应为 %v,得到 %v", want, groups)
	}

	// 评估集合:精布@1、法杖@1、法杖@4、长袍@2
	if strings.Join(res.ItemIDs, ",") != "T5_CLOTH,T6_MAIN_FIRESTAFF,T6_ARMOR_CLOTH_SET2" ||
		strings.Join(res.ExtraItemIDs, ",") != "T6_ARMOR_CLOTH_SET2" {
		t.Fatalf("物品集不对,得到 %v / extra %v", res.ItemIDs, res.ExtraItemIDs)
	}
	if res.Pairs != 4 || res.Capture.ExtraPairs != 2 || res.PriceRows != 4 {
		t.Fatalf("应评估 4 个组合、其中 2 个因抓包并入、AODP 只留 4 行,得到 %d / %d / %d",
			res.Pairs, res.Capture.ExtraPairs, res.PriceRows)
	}
	evaluated := map[string]bool{}
	for _, o := range res.Opportunities {
		evaluated[o.ItemID+"@"+strconv.Itoa(o.Quality)] = true
	}
	for _, r := range res.Rejected {
		evaluated[r.ItemID+"@"+strconv.Itoa(r.Quality)] = true
	}
	for _, k := range []string{"T6_MAIN_FIRESTAFF@2", "T6_ARMOR_CLOTH_SET2@1"} {
		if evaluated[k] {
			t.Fatalf("%s 没抓到,AODP 顺带回了也不该评估", k)
		}
	}
	for _, k := range []string{"T5_CLOTH@1", "T6_MAIN_FIRESTAFF@1", "T6_MAIN_FIRESTAFF@4", "T6_ARMOR_CLOTH_SET2@2"} {
		if !evaluated[k] {
			t.Fatalf("%s 应在评估里(机会或被拒),得到 机会 %+v / 拒绝 %+v", k, res.Opportunities, res.Rejected)
		}
	}

	// 读簿按每个物品自己的品质表:法杖读 q1、q4,长袍只读 q2
	if len(books.lastKeys) != 4*2 || !slices.Contains(books.lastKeys, staff4) {
		t.Fatalf("读簿应为 4 个组合 × 2 边,含法杖 q4 卖单,得到 %v", books.lastKeys)
	}
	var staff *screen.Opportunity
	for i, o := range res.Opportunities {
		if o.ItemID == "T6_MAIN_FIRESTAFF" && o.Quality == 4 {
			staff = &res.Opportunities[i]
		}
	}
	if staff == nil || staff.Ask.Source != screen.SourceCapture || staff.SellPrice != 33_100 {
		t.Fatalf("法杖杰出应上榜且卖价取抓包 33100,得到 %+v(拒绝 %v)", staff, res.RejectCounts)
	}
	if res.Capture.AsksUsed != 1 || res.Capture.BookSides != 1 {
		t.Fatalf("抓包汇总应有 1 个卖方用上,得到 %+v", res.Capture)
	}
	// 覆盖率只数评估的组合:4 行 × 两侧,没抓到的那两个组合不算
	if cov := res.Coverage[0]; cov.WithData != 8 {
		t.Fatalf("Lymhurst 应有 8 个报价点,得到 %+v", cov)
	}
}

// capture.max_extra_items = 0 是"抓包不扩扫描":别的品质也不并,就是配置清单 × qualities
func TestRunWithCapture_上限0时品质也不扩(t *testing.T) {
	a := newQualityAODP(t, map[string]qMarket{"T6_MAIN_FIRESTAFF@1": {11_200, 10_000, 5_000, 10_500}})
	cfg := captureCfg()
	cfg.Items.Patterns = []string{"T6_MAIN_FIRESTAFF"}
	cfg.Capture.MaxExtraItems = 0
	cat := catalog.New([]catalog.Item{{ItemID: "T6_MAIN_FIRESTAFF"}}, nil, "")
	books := &fakeBooks{items: []CapturedItem{{"T6_MAIN_FIRESTAFF", 4, 30}}}
	res, err := RunWithCapture(context.Background(), aodp.New(a.srv.URL, cfg.API), cfg, cat, books, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Pairs != 1 || res.Capture.ExtraPairs != 0 || books.itemCalls != 0 || len(a.requests()) != 2 {
		t.Fatalf("应只扫法杖普通、不查抓包物品,得到 %d / %d / 查 %d 次 / 请求 %d 次",
			res.Pairs, res.Capture.ExtraPairs, books.itemCalls, len(a.requests()))
	}
}

// 补拉已有物品的新品质:只请求那一档,不重拉已有的普通
func TestFetchExtension_已有物品只拉新品质(t *testing.T) {
	a := newQualityAODP(t, map[string]qMarket{
		"T6_MAIN_FIRESTAFF@1": {11_200, 10_000, 5_000, 10_500},
		"T6_MAIN_FIRESTAFF@4": {33_600, 30_000, 5_000, 31_500},
	})
	cfg := captureCfg()
	pending := Pairs{Items: []string{"T6_MAIN_FIRESTAFF"}, Qualities: QualitySet{"T6_MAIN_FIRESTAFF": {4}}}
	ext, history, err := FetchExtension(context.Background(), aodp.New(a.srv.URL, cfg.API), cfg, pending, now)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range a.requests() {
		if q := u.Query().Get("qualities"); q != "4" {
			t.Fatalf("补拉只该问杰出这一档,得到 qualities=%s", q)
		}
	}
	if len(ext.Prices) != 1 || ext.Prices[0].Quality != 4 || len(history) != 1 || len(ext.Stats) != 1 ||
		!slices.Equal(ext.Qualities["T6_MAIN_FIRESTAFF"], []int{4}) {
		t.Fatalf("补拉结果应只有杰出那一档,得到 %+v / %d 条历史", ext, len(history))
	}
	pending.Qualities["T6_MAIN_FIRESTAFF"] = []int{1}
	if !slices.Equal(ext.Qualities["T6_MAIN_FIRESTAFF"], []int{4}) {
		t.Fatal("Extension 要拷一份品质表,调用方之后改 pending 不该影响它")
	}
}

// 配置里的品质写乱了也要认得出"就是配置那一套":[2,1,1] 和 [1,2] 同组
func TestFetchAODP_配置品质乱序去重仍算同一组(t *testing.T) {
	a := newQualityAODP(t, map[string]qMarket{"T5_CLOTH@1": {1120, 1000, 50_000, 1050}})
	cfg := conf.Default()
	cfg.Cities = []string{"Lymhurst"}
	cfg.Qualities = []int{2, 1, 1}
	items := []string{"T5_CLOTH", "T5_WOOD"}
	p := Pairs{Items: items, Qualities: uniform(items, cfg.Qualities)}
	if _, _, _, err := fetchAODP(context.Background(), aodp.New(a.srv.URL, cfg.API), cfg, p, now); err != nil {
		t.Fatal(err)
	}
	reqs := a.requests()
	if len(reqs) != 2 || reqs[0].Query().Get("qualities") != "1,2" {
		t.Fatalf("应只有一组、qualities=1,2,得到 %d 次 %v", len(reqs), reqs)
	}
}
