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
	// 两边都封:这 36% 是带宽推出来的,要压到 low 并说一句
	if opp.Confidence != Low || !anyContains(opp.Warnings, "按带宽推出来的") {
		t.Fatalf("两边都封应是 low 并带说明,得到 %s / %v", opp.Confidence, opp.Warnings)
	}
}

// 审查复现:两边盘口都离均价超过带宽时,两个挂价都不是盘口给的,毛利只由带宽决定
// (同城 band 0.2 时恒约 37%)。松木 T4_WOOD @ Fort Sterling(影子库):买一 120、卖一 187、
// 7 日均价 152,各自离均价 0.79 / 1.23 倍,不贴任何阈值,以前照样 high 排进榜单。
// 现在压到 low;只封一边的不因此降级
func TestClamp_两边都封时可信度封顶low(t *testing.T) {
	rec := cityPrice("T4_WOOD", "Fort Sterling", 1, 120, 187)
	st := cityStats("T4_WOOD", "Fort Sterling", 152, 50_000)
	old, rej := Evaluate(rec, st, noBand(config()), names, now)
	if rej != nil || old.Confidence != High {
		t.Fatalf("前提:band 0 应上榜、high,得到 %+v / %+v", old, rej)
	}
	opp, rej := Evaluate(rec, st, config(), names, now)
	if rej != nil {
		t.Fatalf("不该被拒: %+v", rej)
	}
	if !opp.Clamp.Both() || opp.MyBid != 122 || opp.MyAsk != 182 {
		t.Fatalf("应挂 122 收、182 卖,两边都封,得到 %d → %d / %+v", opp.MyBid, opp.MyAsk, opp.Clamp)
	}
	if opp.Confidence != Low || !anyContains(opp.Warnings, "两边盘口都离参考价超过 ±20%:挂买价、挂卖价都是按带宽推出来的") {
		t.Fatalf("两边都封应压到 low 并说明,得到 %s / %v", opp.Confidence, opp.Warnings)
	}

	// 只封买一侧(买一 750 是均价 0.75 倍,卖一 1,150 在带里):不降级
	one, rej := Evaluate(cityPrice("T5_CLOTH", "Lymhurst", 1, 750, 1150), cityStats("T5_CLOTH", "Lymhurst", 1000, 50_000), config(), names, now)
	if rej != nil || one.Clamp == nil || !one.Clamp.BuyClamped || one.Clamp.SellClamped || one.Confidence != High ||
		anyContains(one.Warnings, "按带宽推出来的") {
		t.Fatalf("只封一边不该降级,得到 %+v / %+v", one, rej)
	}
}

// 审查复现:卖一已经跌破 均价 × 0.8 时,按均价抬挂买价会越过卖一。
// T4_CLOTH_LEVEL1@1 @ Bridgewatch(真实库):买一 417、卖一 450、7 日均价 587,以前下沿 470
// 高过卖一——进游戏照抄等于挂一张比秒买还贵的买单。现在参考价取 min(均价, 卖一) = 450,
// 下沿 360,买一 418 在带里,不钳
func TestClamp_钳过的挂买价不越过卖一(t *testing.T) {
	rec := cityPrice("T4_CLOTH_LEVEL1@1", "Bridgewatch", 1, 417, 450)
	st := cityStats("T4_CLOTH_LEVEL1@1", "Bridgewatch", 587, 50_000)
	on, rej1 := Evaluate(rec, st, config(), names, now)
	off, rej2 := Evaluate(rec, st, noBand(config()), names, now)
	if !reflect.DeepEqual(on, off) || !reflect.DeepEqual(rej1, rej2) {
		t.Fatalf("盘口整体在均价下面、价差 7.9%%,不该钳\n开 %+v / %+v\n关 %+v / %+v", on, rej1, off, rej2)
	}

	// 盘口跌到均价下面、买卖价差又宽:下沿按卖一算。买 5,000 / 卖 7,900 / 均价 10,000,
	// 以前挂买价抬到 8,000 高过卖一、整条报 unprofitable;现在按卖一 7,900 下浮到 6,320
	rec = cityPrice("T5_CLOTH", "Lymhurst", 1, 5000, 7900)
	st = cityStats("T5_CLOTH", "Lymhurst", 10_000, 5000)
	opp, rej := Evaluate(rec, st, config(), names, now)
	if rej != nil {
		t.Fatalf("按卖一定下沿之后还赚,不该被拒: %s %s", rej.Reason, rej.Detail)
	}
	if opp.MyBid != 6320 || opp.MyBid >= opp.SellPrice || opp.Clamp == nil || opp.Clamp.BuyRefFrom != econ.RefSellMin {
		t.Fatalf("挂买价应按卖一 7,900 下浮到 6,320,得到 %d / %+v", opp.MyBid, opp.Clamp)
	}
	if !anyContains(opp.Warnings, "挂买价按卖一 7,900 下浮 20% 抬到 6,320:整本盘口已跌到 7 日成交均价以下") {
		t.Fatalf("说明要写清按卖一抬,得到 %v", opp.Warnings)
	}
}

// 单边行情:整本盘口都跌到均价下面时,均价是旧的。稀有钢条.2 T4_METALBAR_LEVEL2@2 @ Bridgewatch
// (影子库):买一 1,992、卖一 2,367(价差 18.8%)、7 日均价 2,549、日量 4,447。以前按均价把挂买价
// 抬到 2,039,毛利 8.3% → 5.8%;带宽再窄一点(0.15)直接判 unprofitable。现在下沿按卖一算,
// band 0.2 下不钳、和 band 0 逐位一样;更窄的带宽照样按卖一收紧,但不会再因为旧均价被砍
func TestClamp_单边下跌的正常盘口不被压薄(t *testing.T) {
	rec := cityPrice("T4_METALBAR_LEVEL2@2", "Bridgewatch", 1, 1992, 2367)
	st := cityStats("T4_METALBAR_LEVEL2@2", "Bridgewatch", 2549, 4447)
	off, rej := Evaluate(rec, st, noBand(config()), names, now)
	if rej != nil || math.Abs(off.Margin-0.0829) > 0.0001 {
		t.Fatalf("前提:band 0 应是 8.29%%,得到 %+v / %+v", off, rej)
	}
	on, rej := Evaluate(rec, st, config(), names, now)
	if rej != nil || !reflect.DeepEqual(on, off) {
		t.Fatalf("band 0.2:不该有任何变化,得到 %+v / %+v", on, rej)
	}
	for _, band := range []float64{0.1, 0.15} {
		cfg := config()
		cfg.Filters.MakerPriceBand = band
		on, rej := Evaluate(rec, st, cfg, names, now)
		if rej != nil || on.Clamp == nil || on.Clamp.BuyRefFrom != econ.RefSellMin || on.Margin <= 0 {
			t.Fatalf("band %v:下沿应按卖一 2,367 算、仍上榜,得到 %+v / %+v", band, on, rej)
		}
	}
}

// 审查复现:深度闸门、毛利率上限这两个拒绝原因判的是盘口本身,同一本盘口带宽开着关着、
// 开多宽,都得是同一个结论。以前先按钳过的账判"赚不赚钱":
//   - 买 1,100 / 卖 2,450 / 均价 1,000,盘口毛利 103%:band 0 报 implausible_margin,
//     band 0.2 报 unprofitable("税后每件亏 6.5 银"),看不出是假单
//   - T6 买方薄(近价 11 件):band 0 报 no_bid_side,band 0.05 报 unprofitable
func TestClamp_troll判定不随带宽变(t *testing.T) {
	sides := Sides{Ask: capSide(t6AskLadder...), Bid: capSide(t6BidLadder...)}
	for _, band := range []float64{0, 0.05, 0.1, 0.2, 0.3} {
		cfg := config()
		cfg.Filters.MakerPriceBand = band
		_, rej := Evaluate(makePrice(priceOpt{buy: 1100, sell: 2450}), makeStats(statsOpt{avg7d: 1000}), cfg, names, now)
		if rej == nil || rej.Reason != "implausible_margin" || !strings.Contains(rej.Detail, "103%") {
			t.Fatalf("band %v:盘口毛利 103%%,应判 implausible_margin,得到 %+v", band, rej)
		}
		_, rej = EvaluateSides(t6Price(), sides, makeStats(t6Stats()), cfg, names, now)
		if rej == nil || rej.Reason != "no_bid_side" || !strings.Contains(rej.Detail, "11 件") {
			t.Fatalf("band %v:买方近价只有 11 件,应判 no_bid_side,得到 %+v", band, rej)
		}
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

// 盘口价差够、封回参考价之后不够:判 unprofitable,理由里要写封过的价,
// 否则"盘口价差 48% 还说不赚钱"读不懂。
//
// 同城参考价跟着对面盘口走之后,band 0.2 下挂买挂卖钳过的价差至少约 20%(挂卖上限 ≥ 买一 × 1.2、
// 挂买下沿 ≤ 卖一 × 0.8),钳过之后不会再掉到 9.63% 以下。这里用窄带宽 0.05 造这个情形
func TestClamp_封过之后不赚钱的理由写封过的价(t *testing.T) {
	// 均价 1000,买一 1150、卖一 1700:买一高过均价,卖侧参考价取买一 1,150,上限 1,208;
	// 挂买 1,151 在带宽里不动。价差剩 4.95%,不到挂买挂卖的 9.63%
	rec := makePrice(priceOpt{buy: 1150, sell: 1700})
	cfg := config()
	cfg.Filters.MakerPriceBand = 0.05
	_, rej := Evaluate(rec, makeStats(statsOpt{avg7d: 1000}), cfg, names, now)
	if rej == nil || rej.Reason != "unprofitable" {
		t.Fatalf("封过之后不赚钱,应判 unprofitable,得到 %+v", rej)
	}
	for _, want := range []string{"封过", "±5%", "1,151 → 1,208", "9.63%", "盘口价差 47.83%"} {
		if !strings.Contains(rej.Detail, want) {
			t.Fatalf("理由应含 %q,得到 %q", want, rej.Detail)
		}
	}
	// 同一个盘口不钳是赚的——证明拦它的是带宽;默认带宽 0.2 下也赚(1,151 → 1,380)
	if _, rej := Evaluate(rec, makeStats(statsOpt{avg7d: 1000}), noBand(config()), names, now); rej != nil {
		t.Fatalf("前提:band=0 时应上榜,却被 %s 拒了", rej.Reason)
	}
	if opp, rej := Evaluate(rec, makeStats(statsOpt{avg7d: 1000}), config(), names, now); rej != nil || opp.MyAsk != 1380 {
		t.Fatalf("band 0.2 应按买一 1,150 上浮封到 1,380 并上榜,得到 %+v / %+v", opp, rej)
	}
}
