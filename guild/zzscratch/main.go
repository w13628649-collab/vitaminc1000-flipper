package main

import (
	"fmt"

	"albion-guild/internal/arb"
	"albion-guild/internal/conf"
	"albion-guild/internal/econ"
	"albion-guild/internal/histagg"
	"albion-guild/internal/portfolio"
)

func main() {
	cfg := conf.Default()
	e := cfg.Economics
	mm := econ.Mode{Buy: econ.Maker, Sell: econ.Maker}

	// ---- 1. 本金受限的同城 maker-maker:turns=3,DailyROI 字段却还是单轮毛利率 ----
	book := econ.Book{SellMin: 1200, BuyMax: 1000}
	u := econ.Quote(book, book, mm, e)
	hours := mm.HoursPerRound(0, cfg.Sizing.FillHours)
	absorbable := 100000.0 * cfg.Sizing.AbsorbRatio // 日成交 10 万件
	qty, turns, daily := econ.Turnover(u, absorbable, cfg.Capital, hours)
	capUsed := float64(qty) * u.CostPerUnit
	fmt.Printf("[1] absorbable=%.0f qty=%d turns=%.2f daily=%.0f capUsed=%.0f\n", absorbable, qty, turns, daily, capUsed)
	fmt.Printf("    真实日ROI=daily/capUsed=%.4f  CapitalROI=daily/总本金=%.4f  但 DailyROI 字段=margin=%.4f\n",
		daily/capUsed, daily/float64(cfg.Capital), u.Margin)
	for _, m := range econ.Modes {
		q := econ.Quote(book, book, m, e)
		h := m.HoursPerRound(0, cfg.Sizing.FillHours)
		qq, tt, dd := econ.Turnover(q, absorbable, cfg.Capital, h)
		fmt.Printf("    %-12s profit/u=%8.2f h=%.2f qty=%d turns=%.2f daily=%.0f\n", m.Label(), q.ProfitPerUnit, h, qq, tt, dd)
	}
	// portfolio 对同一条机会算出来的"日收益"
	p1 := portfolio.Build([]portfolio.Candidate{{
		Key: "flip", Kind: "flip", CostPerUnit: u.CostPerUnit,
		MaxQty: int64(absorbable), ProfitPerUnit: u.ProfitPerUnit,
	}}, portfolio.DefaultOptions(cfg.Capital))
	fmt.Printf("    portfolio slice: qty=%d capital=%.0f 日收益=%.0f / 榜单同一条 daily_profit=%.0f\n",
		p1.Slices[0].Qty, p1.Deployed, p1.DailyProfit, daily)

	// ---- 3. 跨城 trips>1:portfolio 只按一趟算 ----
	opt := arb.DefaultOptions(cfg)
	st2 := &histagg.Stats{DailyVolumeQty: 100000, AvgPrice7d: 1000, AvgPrice30d: 1000, DailyVolumeSilver: 1e8}
	f2 := arb.Market{City: "Thetford", Book: econ.Book{SellMin: 1000, BuyMax: 900}, Stats: st2, AgeHours: 1}
	t2 := arb.Market{City: "Lymhurst", Book: econ.Book{SellMin: 1400, BuyMax: 1300}, Stats: st2, AgeHours: 1}
	for _, r := range arb.Find("T5_PLANKS", "木板", 1, []arb.Market{f2, t2}, e, opt) {
		fmt.Printf("[3] %s->%s mode=%s profit/u=%.1f qty=%d trips=%.2f h=%.2f trip=%.0f daily=%.0f capUsed=%.0f capROI=%.4f bn=%s\n",
			r.FromCity, r.ToCity, r.ModeLabel, r.ProfitPerUnit, r.Qty, r.TripsPerDay, r.HoursPerTrip,
			r.TripProfit, r.DailyProfit, r.CapitalUsed, r.CapitalROI, r.Bottleneck)
		for _, m := range r.Modes {
			fmt.Printf("      mode %-12s profit/u=%8.1f h=%.2f trips=%.2f daily=%.0f\n", m.Label, m.ProfitPerUnit, m.HoursPerTrip, m.TripsPerDay, m.DailyProfit)
		}
		p := portfolio.Build([]portfolio.Candidate{{
			Key: "arb", Kind: "arb", CostPerUnit: r.CostPerUnit, MaxQty: r.Qty, ProfitPerUnit: r.ProfitPerUnit,
		}}, portfolio.DefaultOptions(cfg.Capital))
		fmt.Printf("      portfolio: qty=%d deployed=%.0f 日收益=%.0f dailyROI=%.4f (路线自己说 %.0f)\n",
			p.Slices[0].Qty, p.Deployed, p.DailyProfit, p.DailyROI, r.DailyProfit)
	}

	// ---- 6. 累计曲线单调性:λ 让后一条的真实 ROI 更高 ----
	cands := []portfolio.Candidate{
		{Key: "A", Kind: "flip", CostPerUnit: 1000, MaxQty: 2000, ProfitPerUnit: 100, Volatility: 0.0},
		{Key: "B", Kind: "flip", CostPerUnit: 1000, MaxQty: 2000, ProfitPerUnit: 130, Volatility: 0.9},
	}
	p6 := portfolio.Build(cands, portfolio.DefaultOptions(cfg.Capital))
	for _, s := range p6.Slices {
		fmt.Printf("[6] slice=%s roi=%.4f cumCap=%.0f cumProfit=%.0f cumROI=%.4f\n", s.Key, s.ROI, s.CumCapital, s.CumProfit, s.CumROI)
	}
	fmt.Println("   note:", p6.Note, "idle:", p6.Idle)
}
