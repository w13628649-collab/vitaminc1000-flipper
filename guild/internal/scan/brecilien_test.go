package scan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"albion-guild/internal/aodp"
	"albion-guild/internal/arb"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
	"albion-guild/internal/screen"
)

func historyAt(itemID, city string, qty, price int) map[string]any {
	h := history(itemID, qty, price)
	h["location"] = city
	return h
}

// 用默认配置(不改 cities)跑一次扫描:Brecilien 要进 AODP 请求的 locations,
// 同城机会照常出,跨城路线也出,但一端是 Brecilien 的带 mists 标记、置信度最高 medium
func TestRun_默认城市里有Brecilien同城跨城都参与(t *testing.T) {
	f := fresh()
	var mu sync.Mutex
	var locations []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		locations = append(locations, r.URL.Query().Get("locations"))
		mu.Unlock()
		var payload any = []map[string]any{
			{"item_id": "T5_WOOD", "city": "Lymhurst", "quality": 1,
				"sell_price_min": 1120, "sell_price_min_date": f, "buy_price_max": 1000, "buy_price_max_date": f},
			{"item_id": "T5_WOOD", "city": conf.Brecilien, "quality": 1,
				"sell_price_min": 1450, "sell_price_min_date": f, "buy_price_max": 1300, "buy_price_max_date": f},
		}
		if strings.Contains(r.URL.Path, "/history/") {
			payload = []map[string]any{
				historyAt("T5_WOOD", "Lymhurst", 50_000, 1050),
				historyAt("T5_WOOD", conf.Brecilien, 50_000, 1350),
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)

	cfg := conf.Default()
	cfg.Items.Patterns = []string{"T5_WOOD"}
	cat := catalog.New([]catalog.Item{{ItemID: "T5_WOOD", NameZH: "杉木"}}, nil, "")
	res, err := Run(context.Background(), aodp.New(srv.URL, cfg.API), cfg, cat, now)
	if err != nil {
		t.Fatal(err)
	}

	if len(locations) == 0 {
		t.Fatal("一次请求都没打")
	}
	for _, l := range locations {
		if !slices.Contains(strings.Split(l, ","), conf.Brecilien) {
			t.Fatalf("AODP 请求的 locations 里没有 Brecilien: %q", l)
		}
	}

	// 同城:Brecilien 的机会和皇家城市一样算,没有额外的风险处理
	var same *screen.Opportunity
	for i, o := range res.Opportunities {
		if o.City == conf.Brecilien {
			same = &res.Opportunities[i]
		}
	}
	if same == nil {
		t.Fatalf("Brecilien 同城应有机会,得到 %+v / 拒绝 %v", res.Opportunities, res.RejectCounts)
	}

	// 跨城:Lymhurst 秒买运进迷雾秒卖,价差够,路线照出,但带标记、压到 medium
	var route *arb.Route
	for i, r := range res.Routes {
		if r.FromCity == "Lymhurst" && r.ToCity == conf.Brecilien {
			route = &res.Routes[i]
		}
		if r.FromCity == conf.Brecilien || r.ToCity == conf.Brecilien {
			if !slices.Equal(r.RiskTags, []string{arb.RiskMists}) || r.Confidence == screen.High {
				t.Fatalf("一端是 Brecilien 的路线都应带 mists、最高 medium,得到 %v / %s", r.RiskTags, r.Confidence)
			}
		}
	}
	if route == nil {
		t.Fatalf("应有 Lymhurst → Brecilien 的路线,得到 %+v", res.Routes)
	}
	if route.Confidence != screen.Medium || route.TravelHours != cfg.Sizing.BrecilienTravelHours {
		t.Fatalf("数据 1h、无其他风险时应为 medium、单程按 Brecilien 专用时间,得到 %s / %v",
			route.Confidence, route.TravelHours)
	}

	var cov *CityCoverage
	for i := range res.Coverage {
		if res.Coverage[i].City == conf.Brecilien {
			cov = &res.Coverage[i]
		}
	}
	if cov == nil || cov.WithData != 2 {
		t.Fatalf("覆盖率里应有 Brecilien 一行、两侧都有价,得到 %+v", cov)
	}
}

// 抓包那边读簿的 key 也由 cfg.Cities 拼,默认配置下 Brecilien 的盘口会被读
func TestCaptureKeys_默认城市含Brecilien(t *testing.T) {
	cfg := conf.Default()
	keys := captureKeys([]string{"T5_WOOD"}, cfg.Cities, cfg.Qualities)
	n := 0
	for _, k := range keys {
		if k.LocationID == conf.Brecilien {
			n++
		}
	}
	if n != 2*len(cfg.Qualities) {
		t.Fatalf("Brecilien 每个品质两边都要读,得到 %d 个 key", n)
	}
}
