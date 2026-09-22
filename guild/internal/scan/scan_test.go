// 端到端管道测试:假的 AODP 响应 → 排序后的机会列表。
package scan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/catalog"
	"albion-guild/internal/conf"
)

var now = time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)

func fresh() string { return now.Add(-time.Hour).Format("2006-01-02T15:04:05") }

// 厚利少量 vs 薄利多销 vs troll 挂单
func prices() []map[string]any {
	f := fresh()
	return []map[string]any{
		{"item_id": "T5_CLOTH", "city": "Lymhurst", "quality": 1,
			"sell_price_min": 1450, "sell_price_min_date": f,
			"buy_price_max": 1000, "buy_price_max_date": f},
		{"item_id": "T5_WOOD", "city": "Lymhurst", "quality": 1,
			"sell_price_min": 1120, "sell_price_min_date": f,
			"buy_price_max": 1000, "buy_price_max_date": f},
		{"item_id": "T5_ORE", "city": "Lymhurst", "quality": 1,
			"sell_price_min": 40_000, "sell_price_min_date": f, // 1 件货挂 40 倍价
			"buy_price_max": 1000, "buy_price_max_date": f},
	}
}

func history(itemID string, qty, price int) map[string]any {
	var data []map[string]any
	for n := 1; n <= 5; n++ {
		data = append(data, map[string]any{
			"timestamp":  now.AddDate(0, 0, -n).Format("2006-01-02T00:00:00"),
			"item_count": qty, "avg_price": price,
		})
	}
	return map[string]any{
		"location": "Lymhurst", "item_id": itemID, "quality": 1, "data": data,
	}
}

func run(t *testing.T) *Result {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload any = prices()
		if strings.Contains(r.URL.Path, "/history/") {
			payload = []map[string]any{
				history("T5_CLOTH", 600, 1200),   // 日均 600 件 → 可吃 120 件
				history("T5_WOOD", 50_000, 1050), // 日均 5 万件 → 可吃 1 万件
				history("T5_ORE", 50_000, 1050),
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)

	cfg := conf.Default()
	cfg.Cities = []string{"Lymhurst"}
	cfg.Capital = 10_000_000
	cfg.Items.Patterns = []string{"T5_CLOTH"}

	cat := catalog.New([]catalog.Item{
		{ItemID: "T5_CLOTH", NameZH: "精布", NameEN: "Ornate Cloth"},
		{ItemID: "T5_WOOD", NameZH: "杉木", NameEN: "Cedar Logs"},
		{ItemID: "T5_ORE", NameZH: "钛矿石", NameEN: "Titanium Ore"},
	}, nil, "")

	res, err := Run(context.Background(), aodp.New(srv.URL, cfg.API), cfg, cat, now)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestSortKeyIsAbsoluteDailyProfitNotMargin(t *testing.T) {
	res := run(t)
	var got []string
	for _, o := range res.Opportunities {
		got = append(got, o.ItemID)
	}
	if len(got) != 2 || got[0] != "T5_WOOD" || got[1] != "T5_CLOTH" {
		t.Fatalf("榜单 = %v,想要 [T5_WOOD T5_CLOTH]", got)
	}
	thin, fat := res.Opportunities[0], res.Opportunities[1]
	// 薄利那条毛利率更低,却排在前面——因为一天能做的量大得多
	if !(thin.Margin < fat.Margin) {
		t.Fatalf("毛利率 薄利 %v 应该低于 厚利 %v", thin.Margin, fat.Margin)
	}
	if !(thin.DailyProfit > fat.DailyProfit) {
		t.Fatalf("日收益 薄利 %v 应该高于 厚利 %v", thin.DailyProfit, fat.DailyProfit)
	}
}

func TestTrollOrderStaysOffTheBoard(t *testing.T) {
	res := run(t)
	for _, o := range res.Opportunities {
		if o.ItemID == "T5_ORE" {
			t.Fatal("troll 挂单进了主榜")
		}
	}
	if res.RejectCounts["deviation"] != 1 {
		t.Fatalf("偏离度拒绝数 = %d,想要 1", res.RejectCounts["deviation"])
	}
}

func TestEveryOpportunityReportsDataFreshness(t *testing.T) {
	for _, o := range run(t).Opportunities {
		if o.DataAgeHours != 1.0 {
			t.Fatalf("数据年龄 = %v,想要 1.0", o.DataAgeHours)
		}
		if o.BuyAgeHours == 0 || o.SellAgeHours == 0 {
			t.Fatalf("买卖两侧年龄缺失: %v / %v", o.BuyAgeHours, o.SellAgeHours)
		}
	}
}

func TestRequestsAreBatched(t *testing.T) {
	// 3 个物品 × 2 个端点,批量后应该只有 2 次请求
	if got := run(t).RequestCount; got != 2 {
		t.Fatalf("请求次数 = %d,想要 2", got)
	}
}
