package screen

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
	"albion-guild/internal/histagg"
)

// 挂单腿带宽钳制(econ.QuoteBanded)在同城这一侧的回归测试。
// 数字取自用户截图(2026-09-26 机会页,「机会这里没对上吧」)。

// cityPrice 造一条指定物品、城市的快照,两侧 1 小时前
func cityPrice(item, city string, quality int, buy, sell int64) aodp.PriceRecord {
	at := aodp.Stamp{T: now.Add(-time.Hour)}
	return aodp.PriceRecord{ItemID: item, City: city, Quality: quality,
		SellPriceMin: sell, SellPriceMinDate: at, BuyPriceMax: buy, BuyPriceMaxDate: at}
}

func cityStats(item, city string, avg7d, dailyQty float64) *histagg.Stats {
	s := makeStats(statsOpt{avg7d: avg7d, dailyQty: dailyQty})
	s.ItemID, s.City = item, city
	return s
}

func noBand(cfg conf.Config) conf.Config {
	cfg.Filters.MakerPriceBand = 0
	return cfg
}

// 特异细布.3 @ Martlock:买一 5,401、卖一 10,700,7 日成交均价约 1 万(截图备注"买价偏离 0.54x")。
// 以前挂买挂卖 5,402 → 10,699、毛利 80.7% 上榜。成交价在 1 万附近,没人会把货砸到 5,402 上:
// 挂买价抬到 10,000 × 0.8 = 8,000,毛利剩 22%
func TestClamp_特异细布3同城挂买价抬到8000(t *testing.T) {
	rec := cityPrice("T4_CLOTH_LEVEL3@3", "Martlock", 1, 5401, 10700)
	st := cityStats("T4_CLOTH_LEVEL3@3", "Martlock", 10_000, 500)

	// 改之前(band = 0)就是截图上那条
	old, rej := Evaluate(rec, st, noBand(config()), names, now)
	if rej != nil {
		t.Fatalf("前提:band=0 时应上榜,却被 %s 拒了", rej.Reason)
	}
	if old.MyBid != 5402 || old.MyAsk != 10699 || math.Abs(old.Margin-0.807) > 0.001 || old.Clamp != nil {
		t.Fatalf("前提:band=0 应是 5,402 → 10,699、毛利 80.7%%,得到 %d → %d / %.4f / %+v",
			old.MyBid, old.MyAsk, old.Margin, old.Clamp)
	}

	opp, rej := Evaluate(rec, st, config(), names, now)
	if rej != nil {
		t.Fatalf("封过之后还赚钱(22%%),不该被拒,却被 %s 拒了: %s", rej.Reason, rej.Detail)
	}
	if opp.Mode != "maker-maker" || opp.MyBid != 8000 || opp.MyAsk != 10699 {
		t.Fatalf("应挂 8,000 收、10,699 卖,得到 %s %d → %d", opp.Mode, opp.MyBid, opp.MyAsk)
	}
	want := 10699*0.935/(8000*1.025) - 1
	if math.Abs(opp.Margin-want) > 1e-12 || math.Abs(opp.Margin-0.2200) > 0.0001 {
		t.Fatalf("毛利应是 22.00%%,得到 %.4f", opp.Margin)
	}
	if opp.DailyROI != opp.Margin || math.Abs(opp.ProfitPerUnit-(10699*0.935-8000*1.025)) > 1e-9 {
		t.Fatalf("单件利润、日回报都按封过的价算,得到 %+v", opp)
	}
	c := opp.Clamp
	if c == nil || !c.BuyClamped || c.SellClamped || c.RawBuyPrice != 5402 || c.RawSellPrice != 10699 ||
		c.BuyRef != 10_000 || math.Abs(c.RawMargin-old.Margin) > 1e-15 {
		t.Fatalf("clamp 应说明挂买从 5,402 抬到 8,000,得到 %+v", c)
	}
	// 日收益按新的单件利润重算,件数上限不变
	if opp.Qty != old.Qty || math.Abs(opp.DailyProfit-old.DailyProfit/old.ProfitPerUnit*opp.ProfitPerUnit) > 1e-6 {
		t.Fatalf("日收益应按封过的单件利润重算,得到 %v(原 %v)", opp.DailyProfit, old.DailyProfit)
	}
	if !anyContains(opp.Warnings, "挂买价按 7 日成交均价 10,000 下浮 20% 抬到 8,000:买一 5,401 只有均价的 0.54 倍") {
		t.Fatalf("警告里要有一句人话说明挂买价为什么不是 5,402,得到 %v", opp.Warnings)
	}
	// 选中模式的 ModeQuote 和机会本身是同一份账
	for _, m := range opp.Modes {
		if m.Mode == opp.Mode && (m.BuyPrice != 8000 || m.Clamp == nil || !reflect.DeepEqual(*m.Clamp, *c)) {
			t.Fatalf("选中模式的报价应和机会一致,得到 %+v", m)
		}
	}
	// 对外 JSON 看得出被钳了
	raw, _ := json.Marshal(opp)
	for _, k := range []string{`"clamp":{`, `"buy_clamped":true`, `"raw_buy_price":5402`, `"buy_ref":10000`} {
		if !strings.Contains(string(raw), k) {
			t.Fatalf("JSON 里应有 %s,得到 %s", k, raw)
		}
	}
}

// 流动性好、盘口紧贴均价的正常品种:两条挂单腿都在带宽里(均价 290,带宽 232 ~ 348),
// 结果和不钳逐位相同
func TestClamp_盘口紧贴均价的正常品种不受影响(t *testing.T) {
	rec := cityPrice("T4_METALBAR", "Thetford", 1, 270, 312)
	st := cityStats("T4_METALBAR", "Thetford", 290, 200_000)
	on, rej := Evaluate(rec, st, config(), names, now)
	if rej != nil {
		t.Fatalf("正常品种应上榜,却被 %s 拒了: %s", rej.Reason, rej.Detail)
	}
	off, _ := Evaluate(rec, st, noBand(config()), names, now)
	if !reflect.DeepEqual(on, off) || on.Clamp != nil {
		t.Fatalf("带宽内的盘口不该有任何变化\n开 %+v\n关 %+v", on, off)
	}
	a, _ := json.Marshal(on)
	b, _ := json.Marshal(off)
	if string(a) != string(b) || strings.Contains(string(a), "clamp") {
		t.Fatalf("JSON 应逐字相同且不带 clamp\n%s\n%s", a, b)
	}
}

// band = 0 时和改之前逐位一致:价、账、周转全按盘口价压一档,不带 clamp、不多警告。
// "改之前"的算法就是 econ.Quote + econ.Turnover,这里直接拿它们对
func TestClamp_带宽为0时和改之前逐位一致(t *testing.T) {
	cases := []struct {
		name     string
		rec      aodp.PriceRecord
		st       *histagg.Stats
		wantMode string
	}{
		{"特异细布.3", cityPrice("T4_CLOTH_LEVEL3@3", "Martlock", 1, 5401, 10700), cityStats("T4_CLOTH_LEVEL3@3", "Martlock", 10_000, 500), "maker-maker"},
		{"栗木板条", cityPrice("T3_PLANKS", "Lymhurst", 1, 172, 359), cityStats("T3_PLANKS", "Lymhurst", 287, 20_000), "maker-maker"},
		{"正常", cityPrice("T4_METALBAR", "Thetford", 1, 270, 312), cityStats("T4_METALBAR", "Thetford", 290, 200_000), "maker-maker"},
	}
	cfg := noBand(config())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opp, rej := Evaluate(tc.rec, tc.st, cfg, names, now)
			if rej != nil {
				t.Fatalf("band=0 应上榜,却被 %s 拒了: %s", rej.Reason, rej.Detail)
			}
			book := econ.Book{SellMin: tc.rec.SellPriceMin, BuyMax: tc.rec.BuyPriceMax}
			for _, m := range econ.Modes {
				u := econ.Quote(book, book, m, cfg.Economics)
				_, turns, daily := econ.Turnover(u, tc.st.DailyVolumeQty*cfg.Sizing.AbsorbRatio, cfg.Capital,
					m.HoursPerRound(0, cfg.Sizing.FillHours))
				var q *ModeQuote
				for i := range opp.Modes {
					if opp.Modes[i].Mode == m.Key() {
						q = &opp.Modes[i]
					}
				}
				if q == nil || q.BuyPrice != u.MyBid || q.SellPrice != u.MyAsk || q.ProfitPerUnit != u.ProfitPerUnit ||
					q.Margin != u.Margin || q.TurnsPerDay != turns || q.DailyProfit != daily || q.Clamp != nil {
					t.Fatalf("%s:应和 Quote + Turnover 逐位一致,得到 %+v,应为 %+v / %v / %v", m.Label(), q, u, turns, daily)
				}
				if m.Key() == tc.wantMode && (opp.MyBid != u.MyBid || opp.MyAsk != u.MyAsk || opp.Margin != u.Margin ||
					opp.DailyProfit != daily || opp.Clamp != nil) {
					t.Fatalf("选中的 %s 应和 Quote 逐位一致,得到 %+v", m.Label(), opp)
				}
			}
			if anyContains(opp.Warnings, "挂买价按") || anyContains(opp.Warnings, "挂卖价按") {
				t.Fatalf("band=0 不该有钳制说明,得到 %v", opp.Warnings)
			}
		})
	}
}

// 栗木板条 T3 @ Lymhurst:买一 172、卖一 359,7 日均价 287(截图"买价偏离 0.60x",毛利 88.8%)。
// 两条腿都出了带宽:挂买抬到 230、挂卖封到 344,毛利剩 36%
func TestClamp_栗木板条两条腿都封(t *testing.T) {
	rec := cityPrice("T3_PLANKS", "Lymhurst", 1, 172, 359)
	st := cityStats("T3_PLANKS", "Lymhurst", 287, 20_000)
	old, _ := Evaluate(rec, st, noBand(config()), names, now)
	if old == nil || math.Abs(old.Margin-0.888) > 0.001 {
		t.Fatalf("前提:band=0 应是毛利 88.8%%,得到 %+v", old)
	}
	opp, rej := Evaluate(rec, st, config(), names, now)
	if rej != nil {
		t.Fatalf("封过还赚 36%%,不该被拒,却被 %s 拒了", rej.Reason)
	}
	if opp.MyBid != 230 || opp.MyAsk != 344 || opp.Clamp == nil || !opp.Clamp.BuyClamped || !opp.Clamp.SellClamped {
		t.Fatalf("应挂 230 收、344 卖,两边都标被封,得到 %d → %d / %+v", opp.MyBid, opp.MyAsk, opp.Clamp)
	}
	if want := 344*0.935/(230*1.025) - 1; math.Abs(opp.Margin-want) > 1e-12 {
		t.Fatalf("毛利应为 %.4f,得到 %.4f", want, opp.Margin)
	}
	if !anyContains(opp.Warnings, "抬到 230") || !anyContains(opp.Warnings, "封到 344:卖一 359 是均价的 1.25 倍") {
		t.Fatalf("两条腿都要有说明,得到 %v", opp.Warnings)
	}
}

// 带宽不能替 troll 单开门:买价 0.45×、卖价 2.4×,封回均价 ±20% 之后毛利只剩 37%,
// 但毛利率上限判的是盘口本身(按不钳的价算 385%),照样拦
func TestClamp_毛利率上限仍按盘口价判(t *testing.T) {
	rec := makePrice(priceOpt{buy: 495, sell: 2640})
	_, rej := Evaluate(rec, makeStats(statsOpt{}), config(), names, now)
	if rej == nil || rej.Reason != "implausible_margin" {
		t.Fatalf("两边都是 troll 单,带宽开着也要拦成 implausible_margin,得到 %+v", rej)
	}
	if !strings.Contains(rej.Detail, "按盘口价算毛利率") || !strings.Contains(rej.Detail, "37%") {
		t.Fatalf("理由要说清判的是盘口价、封过之后是多少,得到 %q", rej.Detail)
	}
}

// 盘口价差够、封回成交价之后不够:判 unprofitable,理由里要写封过的价,
// 否则"盘口价差 60% 还说不赚钱"读不懂
func TestClamp_封过之后不赚钱的理由写封过的价(t *testing.T) {
	// 均价 1000,买一 1150、卖一 1700(1.7×):挂买 1,151 在带宽里不动,挂卖封到 1,200,
	// 价差剩 4.26%,不到挂买挂卖的 9.63%
	rec := makePrice(priceOpt{buy: 1150, sell: 1700})
	_, rej := Evaluate(rec, makeStats(statsOpt{avg7d: 1000}), config(), names, now)
	if rej == nil || rej.Reason != "unprofitable" {
		t.Fatalf("封过之后不赚钱,应判 unprofitable,得到 %+v", rej)
	}
	for _, want := range []string{"封过", "1,151 → 1,200", "9.63%", "盘口价差 47.83%"} {
		if !strings.Contains(rej.Detail, want) {
			t.Fatalf("理由应含 %q,得到 %q", want, rej.Detail)
		}
	}
	// 同一个盘口不钳是赚的——证明拦它的是带宽
	if _, rej := Evaluate(rec, makeStats(statsOpt{avg7d: 1000}), noBand(config()), names, now); rej != nil {
		t.Fatalf("前提:band=0 时应上榜,却被 %s 拒了", rej.Reason)
	}
}
