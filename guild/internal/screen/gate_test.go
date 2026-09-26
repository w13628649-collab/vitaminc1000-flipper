package screen

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"albion-guild/internal/aodp"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/econ"
)

// T6_METALBAR_LEVEL4@4 @ Martlock 买方实抓阶梯(docs/flipper.md 那张):
// 9 档 2,026 件,最优价 5% 以内只有 7 档 11 件,再往下一档就掉到 53,001(−79.6%),
// 底下是 1 银的占位单。纸面价差 38%,挂买单进去只会一直挂着
var t6BidLadder = []depth.Level{
	{Price: 260066, Qty: 1}, {Price: 260065, Qty: 2}, {Price: 260064, Qty: 1},
	{Price: 260060, Qty: 2}, {Price: 260050, Qty: 2}, {Price: 260040, Qty: 1},
	{Price: 260000, Qty: 2}, {Price: 53001, Qty: 15}, {Price: 1, Qty: 2000},
}

// 同一物品同城的卖方:卖一 12 件、近价 413 件(CLAUDE.md 实测那一行)
var t6AskLadder = []depth.Level{
	{Price: 357999, Qty: 12}, {Price: 358500, Qty: 100}, {Price: 370000, Qty: 301},
}

// capSide 造一个来自抓包、深度可信的盘口边,深度统计用真的 depth.Analyze 算。
func capSide(levels ...depth.Level) Side {
	return Side{
		Source: SourceCapture, Price: levels[0].Price, AgeHours: 0.1,
		Depth:  &DepthView{Support: depth.Analyze(levels, 0.05)},
		Levels: levels,
	}
}

func t6Price() aodp.PriceRecord { return makePrice(priceOpt{buy: 260066, sell: 357999}) }
func t6Stats() statsOpt         { return statsOpt{avg7d: 355000, dailyQty: 50} }

func TestT6夹具_深度统计和实测一致(t *testing.T) {
	s := capSide(t6BidLadder...)
	d := s.Depth
	if d.QtyTotal != 2026 || len(t6BidLadder) != 9 || d.QtyNear != 11 || d.LevelsNear != 7 ||
		math.Abs(d.GapAfterNear-0.7962) > 1e-4 {
		t.Fatalf("前提变了:T6 买方应为 9 档 2026 件、近价 7 档 11 件、断崖 79.6%%,得到 %+v", d.Support)
	}
	if a := capSide(t6AskLadder...).Depth; a.QtyAtBest != 12 || a.QtyNear != 413 {
		t.Fatalf("前提变了:T6 卖方应为卖一 12 件、近价 413 件,得到 %+v", a.Support)
	}
}

// 纸面价差 38%、价格过滤全过,只有近价件数分得开。买方 5% 内 11 件 → no_bid_side
func TestEvaluateSides_T6买方薄被no_bid_side拒(t *testing.T) {
	sides := Sides{Ask: capSide(t6AskLadder...), Bid: capSide(t6BidLadder...)}
	st := t6Stats()

	// 先确认拦它的只有深度:同样的价格走纯 AODP 是能上榜的
	if _, rej := Evaluate(t6Price(), makeStats(st), config(), names, now); rej != nil {
		t.Fatalf("前提变了:纯 AODP 时应能通过,却被 %s 拒了", rej.Reason)
	}
	opp, rej := EvaluateSides(t6Price(), sides, makeStats(st), config(), names, now)
	if rej == nil {
		t.Fatalf("买方近价只有 11 件,不该上榜: %+v", opp)
	}
	if rej.Reason != "no_bid_side" {
		t.Fatalf("拒绝原因 = %s,想要 no_bid_side", rej.Reason)
	}
	// 价按千分位写,和界面格子里的数字一个样
	for _, want := range []string{"买一 260,066", "11 件", "7 档", "79.6%", "创建费"} {
		if !strings.Contains(rej.Detail, want) {
			t.Fatalf("理由应含 %q,得到 %q", want, rej.Detail)
		}
	}
	if rej.AskSource != SourceCapture || rej.BidSource != SourceCapture {
		t.Fatalf("拒绝应带两边来源,得到 %+v", rej)
	}
}

// 四种模式各过一遍闸门:只拦挂买的两个,秒买的两个不受买方深度影响
func TestLegGate_T6四种模式(t *testing.T) {
	bid, ask := capSide(t6BidLadder...), capSide(t6AskLadder...)
	f := conf.Default().Filters
	want := map[string]string{
		"taker-taker": "", "taker-maker": "", "maker-taker": "no_bid_side", "maker-maker": "no_bid_side",
	}
	for _, m := range econ.Modes {
		blocked, detail, edge, _ := LegGate(m, bid, ask, f)
		if blocked != want[m.Key()] {
			t.Fatalf("%s:blocked = %q,想要 %q", m.Key(), blocked, want[m.Key()])
		}
		if blocked != "" && (detail == "" || edge) {
			t.Fatalf("%s:被拦时应有理由、不再标 edge,得到 %q / %v", m.Key(), detail, edge)
		}
	}
}

// 被拦的模式照样进 modes、带着 Blocked,只是不参与挑选。同城能让它和一个
// 可做的模式同时出现的只有交叉盘(秒买秒卖才赚钱),正好也验一下 fill_position 算不出来
func TestEvaluateSides_被拦的模式照样出现在modes里(t *testing.T) {
	cfg := config()
	cfg.Filters.RejectCrossedBook = false
	rec := makePrice(priceOpt{buy: 1300, sell: 1200})
	sides := Sides{Bid: capSide(depth.Level{Price: 1300, Qty: 2}, depth.Level{Price: 900, Qty: 100})}
	opp, rej := EvaluateSides(rec, sides, makeStats(statsOpt{avg7d: 1250}), cfg, names, now)
	if rej != nil {
		t.Fatalf("交叉盘放行时秒买秒卖应能上榜,却被 %s 拒了: %s", rej.Reason, rej.Detail)
	}
	if opp.Mode != "taker-taker" {
		t.Fatalf("前提变了:应选秒买秒卖,得到 %s", opp.Mode)
	}
	for _, m := range opp.Modes {
		wantBlocked := ""
		if strings.HasPrefix(m.Mode, "maker-") {
			wantBlocked = "no_bid_side"
		}
		if m.Blocked != wantBlocked {
			t.Fatalf("%s:Blocked = %q,想要 %q", m.Mode, m.Blocked, wantBlocked)
		}
	}
	if opp.HasFillPosition || opp.FillPosition != 0 {
		t.Fatalf("买一 ≥ 卖一时位置没有意义,得到 %v / %v", opp.FillPosition, opp.HasFillPosition)
	}
	// 秒买秒卖吃的 ask 是 AODP 的:没核过深度
	if opp.DepthChecked {
		t.Fatal("秒买那一边不是抓包,不该算核过深度")
	}
}

// T4_METALBAR_LEVEL4@4 那种:买方近价 60 件,过得了 min_bid_depth=20,
// 但不到 thin_bid_edge_qty=100 → 上榜、降为 low
func TestEvaluateSides_买方近价60件通过但降为low(t *testing.T) {
	sides := Sides{Bid: capSide(depth.Level{Price: 1000, Qty: 31}, depth.Level{Price: 990, Qty: 29}, depth.Level{Price: 828, Qty: 500})}
	opp, rej := EvaluateSides(makePrice(priceOpt{}), sides, makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("近价 60 件应能通过,却被 %s 拒了: %s", rej.Reason, rej.Detail)
	}
	if opp.Confidence != Low {
		t.Fatalf("置信度 = %s,想要 low", opp.Confidence)
	}
	if !anyContains(opp.Warnings, "60 件") {
		t.Fatalf("警告应说明近价只有 60 件,得到 %v", opp.Warnings)
	}
	// 断崖 17.2% 没到 20%,不该再多一条断崖警告
	if anyContains(opp.Warnings, "断崖") {
		t.Fatalf("断崖没到阈值,得到 %v", opp.Warnings)
	}
}

func TestEvaluateSides_买方近价够但断崖30降为low(t *testing.T) {
	sides := Sides{Bid: capSide(depth.Level{Price: 1000, Qty: 200}, depth.Level{Price: 980, Qty: 300}, depth.Level{Price: 700, Qty: 1000})}
	opp, rej := EvaluateSides(makePrice(priceOpt{}), sides, makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("近价 500 件应能通过,却被 %s 拒了", rej.Reason)
	}
	if opp.Confidence != Low || !anyContains(opp.Warnings, "30.0%") {
		t.Fatalf("断崖 30%% 应降为 low 并说明,得到 %s / %v", opp.Confidence, opp.Warnings)
	}
}

// 挂卖腿:卖一是张 2 件的孤单,压一档挂出去的价是空中楼阁 → thin_book。
// 但卖一只有 4 件、近价 230 件(T5_METALBAR_LEVEL4@4 那种)是正常盘口,
// 按 Python 的旧口径(看卖一那一档)会被误降级,这里不该触发
func TestEvaluateSides_挂卖腿看卖方近价件数(t *testing.T) {
	thin := Sides{Ask: capSide(depth.Level{Price: 1200, Qty: 2}, depth.Level{Price: 1500, Qty: 100})}
	_, rej := EvaluateSides(makePrice(priceOpt{}), thin, makeStats(statsOpt{}), config(), names, now)
	if rej == nil || rej.Reason != "thin_book" || !strings.Contains(rej.Detail, "只有 2 件") {
		t.Fatalf("卖方近价 2 件应被 thin_book 拒,得到 %+v", rej)
	}

	ok := Sides{Ask: capSide(depth.Level{Price: 1200, Qty: 4}, depth.Level{Price: 1210, Qty: 100}, depth.Level{Price: 1250, Qty: 126})}
	opp, rej := EvaluateSides(makePrice(priceOpt{}), ok, makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("卖一 4 件、近价 230 件应通过,却被 %s 拒了", rej.Reason)
	}
	if opp.Ask.Depth.QtyAtBest != 4 || opp.Ask.Depth.QtyNear != 230 {
		t.Fatalf("前提变了:%+v", opp.Ask.Depth.Support)
	}
	if opp.Confidence != High || anyContains(opp.Warnings, "卖一") {
		t.Fatalf("卖方不该触发 edge,得到 %s / %v", opp.Confidence, opp.Warnings)
	}
}

// 深度判据只对抓包那一边生效:同样的件数挂在 AODP 名下,等于没有件数
func TestEvaluateSides_AODP那一边不过深度闸门(t *testing.T) {
	thin := capSide(t6BidLadder...)
	thin.Source = SourceAODP
	thin.Price = 1000
	opp, rej := EvaluateSides(makePrice(priceOpt{}), Sides{Bid: thin}, makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("AODP 那一边不该被深度闸门拦,却被 %s 拒了", rej.Reason)
	}
	base, _ := Evaluate(makePrice(priceOpt{}), makeStats(statsOpt{}), config(), names, now)
	if opp.Mode != base.Mode || opp.DailyProfit != base.DailyProfit || opp.Confidence != base.Confidence ||
		!reflect.DeepEqual(opp.Warnings, base.Warnings) || !reflect.DeepEqual(opp.Modes, base.Modes) {
		t.Fatalf("结果应和纯 AODP 相同\n得到 %+v\n应为 %+v", opp, base)
	}
	if opp.DepthChecked {
		t.Fatal("AODP 不给件数,不能算核过深度")
	}
}

// 阈值全设 0 = 深度层整个关掉:T6 那种薄买方也照样上榜,置信度不受影响
func TestEvaluateSides_深度阈值全为0时不触发(t *testing.T) {
	cfg := config()
	cfg.Filters.MinBidDepth, cfg.Filters.ThinBidEdgeQty, cfg.Filters.BidCliffEdgePct = 0, 0, 0
	cfg.Filters.MinBookQty, cfg.Filters.ThinBookEdgeQty = 0, 0
	sides := Sides{Ask: capSide(depth.Level{Price: 357999, Qty: 1}), Bid: capSide(t6BidLadder...)}
	opp, rej := EvaluateSides(t6Price(), sides, makeStats(t6Stats()), cfg, names, now)
	if rej != nil {
		t.Fatalf("阈值全 0 时不该被深度拦,却被 %s 拒了", rej.Reason)
	}
	if opp.Confidence != High || len(opp.Warnings) != 0 {
		t.Fatalf("深度层关掉后不该降级,得到 %s / %v", opp.Confidence, opp.Warnings)
	}
}

func TestEvaluateSides_价差上限(t *testing.T) {
	cfg := config()
	cfg.Filters.MaxSpreadPct = 0.1
	// 默认夹具 买 1000 / 卖 1200,价差 20%
	_, rej := EvaluateSides(makePrice(priceOpt{}), Sides{}, makeStats(statsOpt{}), cfg, names, now)
	if rej == nil || rej.Reason != "wide_spread" || !strings.Contains(rej.Detail, "20.0%") {
		t.Fatalf("价差 20%% 超过 10%% 应被 wide_spread 拒,得到 %+v", rej)
	}
	if _, rej := EvaluateSides(makePrice(priceOpt{}), Sides{}, makeStats(statsOpt{}), config(), names, now); rej != nil {
		t.Fatalf("默认 0 = 关闭,不该拒,得到 %s", rej.Reason)
	}
}

// fill_position 只展示:(1190 − 1000) / (1200 − 1000) = 0.95,贴着卖价,
// 但置信度和不带它时一样
func TestEvaluateSides_FillPosition只展示(t *testing.T) {
	opp, rej := EvaluateSides(makePrice(priceOpt{}), Sides{}, makeStats(statsOpt{avg7d: 1190}), config(), names, now)
	if rej != nil {
		t.Fatalf("本该通过,却被 %s 拒了", rej.Reason)
	}
	if !opp.HasFillPosition || math.Abs(opp.FillPosition-0.95) > 1e-9 {
		t.Fatalf("fill_position 应为 0.95,得到 %v / %v", opp.FillPosition, opp.HasFillPosition)
	}
	if opp.Confidence != High || len(opp.Warnings) != 0 {
		t.Fatalf("fill_position 不参与判定,得到 %s / %v", opp.Confidence, opp.Warnings)
	}
}

func TestEvaluateSides_两边都来自抓包且够深时算核过深度(t *testing.T) {
	sides := Sides{
		Ask: capSide(depth.Level{Price: 1200, Qty: 300}, depth.Level{Price: 1210, Qty: 800}),
		Bid: capSide(depth.Level{Price: 1000, Qty: 500}, depth.Level{Price: 990, Qty: 1000}),
	}
	opp, rej := EvaluateSides(makePrice(priceOpt{}), sides, makeStats(statsOpt{}), config(), names, now)
	if rej != nil {
		t.Fatalf("两边都深,却被 %s 拒了", rej.Reason)
	}
	if !opp.DepthChecked || opp.Confidence != High {
		t.Fatalf("应核过深度且保持 high,得到 %v / %s", opp.DepthChecked, opp.Confidence)
	}
	// 只有一边来自抓包时不算
	one := sides
	one.Ask = Side{}
	if opp, _ := EvaluateSides(makePrice(priceOpt{}), one, makeStats(statsOpt{}), config(), names, now); opp == nil || opp.DepthChecked {
		t.Fatalf("卖方走 AODP 时不该算核过,得到 %+v", opp)
	}
}

// 截断的阶梯:读到的档全在近价窗口内,近价件数只是下限,不拿它判拒
func TestLegGate_截断的一边不判(t *testing.T) {
	s := capSide(depth.Level{Price: 1000, Qty: 1}, depth.Level{Price: 999, Qty: 1})
	s.Depth.Truncated = true
	mm := econ.Mode{Buy: econ.Maker, Sell: econ.Maker}
	if blocked, _, edge, warns := LegGate(mm, s, Side{}, conf.Default().Filters); blocked != "" || edge || warns != nil {
		t.Fatalf("截断的一边不该判,得到 %q / %v / %v", blocked, edge, warns)
	}
	s.Depth.Truncated = false
	if blocked, _, _, _ := LegGate(mm, s, Side{}, conf.Default().Filters); blocked != "no_bid_side" {
		t.Fatalf("前提变了:不截断时 2 件应被拦,得到 %q", blocked)
	}
}

func anyContains(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
