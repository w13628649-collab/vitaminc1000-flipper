package bm

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

const tax = 0.04

func total(fills []Fill) (qty, profit int64) {
	for _, f := range fills {
		qty += f.Qty
		profit += f.Unit * f.Qty
	}
	return qty, profit
}

func TestNet_只扣销售税(t *testing.T) {
	if got := Net(100_000, tax); got != 96_000 {
		t.Fatalf("10 万卖进黑市到手 %d,想要 96000", got)
	}
	if got := Net(13, tax); got != 12 { // 0.52 四舍五入成 1
		t.Fatalf("得到 %d", got)
	}
}

// 先满足最贵的单、配最便宜的货:普通单(10 万)拿走那件便宜的杰出(1 万),
// 杰出单(9 万)就没货了,总共 8.6 万。对的分法是普通单配普通货、杰出单配杰出货
func TestSolve_贪心会错的那种(t *testing.T) {
	asks := []Ask{{City: "Martlock", Quality: 4, Price: 10_000, Qty: 1}, {City: "Lymhurst", Quality: 1, Price: 20_000, Qty: 1}}
	bids := []Bid{{Quality: 1, Price: 100_000, Qty: 1}, {Quality: 4, Price: 90_000, Qty: 1}}
	fills := Solve(asks, bids, tax)
	qty, profit := total(fills)
	want := (Net(100_000, tax) - 20_000) + (Net(90_000, tax) - 10_000)
	if qty != 2 || profit != want {
		t.Fatalf("得到 %d 件、%d,想要 2 件、%d: %+v", qty, profit, want, fills)
	}
	for _, f := range fills {
		if f.Bid.Quality == 4 && f.Ask.Quality != 4 {
			t.Fatalf("杰出单该配杰出货: %+v", f)
		}
	}
}

// 低品质的货进不了高品质的单
func TestSolve_低品质卖不进高品质的单(t *testing.T) {
	asks := []Ask{{City: "Martlock", Quality: 2, Price: 1_000, Qty: 5}}
	bids := []Bid{{Quality: 3, Price: 100_000, Qty: 5}}
	if fills := Solve(asks, bids, tax); fills != nil {
		t.Fatalf("不该成交: %+v", fills)
	}
}

// 黑市一档要 3 件,两座城各有 2 件:先吃便宜的,合计只做 3 件——同一张单不能被两条路线各算一遍
func TestSolve_黑市单只算一次(t *testing.T) {
	asks := []Ask{
		{City: "Martlock", Quality: 1, Price: 50_000, Qty: 2},
		{City: "Lymhurst", Quality: 1, Price: 60_000, Qty: 2},
	}
	bids := []Bid{{Quality: 1, Price: 100_000, Qty: 3}}
	fills := Solve(asks, bids, tax)
	qty, profit := total(fills)
	if qty != 3 || profit != 2*(96_000-50_000)+(96_000-60_000) {
		t.Fatalf("得到 %d 件、%d: %+v", qty, profit, fills)
	}
}

// 不赚钱的档不碰:主城价刚好等于扣税后的黑市价,也不做
func TestSolve_不赚钱不做(t *testing.T) {
	asks := []Ask{{City: "Martlock", Quality: 1, Price: 96_000, Qty: 5}}
	bids := []Bid{{Quality: 1, Price: 100_000, Qty: 5}}
	if fills := Solve(asks, bids, tax); fills != nil {
		t.Fatalf("不该成交: %+v", fills)
	}
	if Solve(nil, bids, tax) != nil || Solve(asks, nil, tax) != nil {
		t.Fatal("一边为空应该是 nil")
	}
}

// 0 件、0 价的档当不存在
func TestSolve_空档跳过(t *testing.T) {
	asks := []Ask{{City: "Martlock", Quality: 1, Price: 0, Qty: 5}, {City: "Lymhurst", Quality: 1, Price: 10, Qty: 0}}
	bids := []Bid{{Quality: 1, Price: 100_000, Qty: 5}}
	if fills := Solve(asks, bids, tax); fills != nil {
		t.Fatalf("不该成交: %+v", fills)
	}
}

// 小例子逐件穷举,和流的结果比总利润
func TestSolve_和穷举一样(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	cities := []string{"Martlock", "Lymhurst", "Caerleon"}
	for round := range 400 {
		var asks []Ask
		for range 1 + r.IntN(4) {
			asks = append(asks, Ask{City: cities[r.IntN(3)], Quality: 1 + r.IntN(5),
				Price: int64(10 + r.IntN(90)), Qty: int64(1 + r.IntN(2))})
		}
		var bids []Bid
		for range 1 + r.IntN(3) {
			bids = append(bids, Bid{Quality: 1 + r.IntN(5), Price: int64(20 + r.IntN(100)), Qty: int64(1 + r.IntN(2))})
		}
		_, got := total(Solve(asks, bids, tax))
		if want := bruteForce(asks, bids); got != want {
			t.Fatalf("第 %d 轮:流 %d,穷举 %d\nasks %+v\nbids %+v", round, got, want, asks, bids)
		}
	}
}

// bruteForce 把每档拆成单件,每件黑市单依次选"不卖"或者配一件还没用的货,取最大
func bruteForce(asks []Ask, bids []Bid) int64 {
	var units []Ask
	for _, a := range asks {
		for range a.Qty {
			units = append(units, a)
		}
	}
	var need []Bid
	for _, b := range bids {
		for range b.Qty {
			need = append(need, b)
		}
	}
	used := make([]bool, len(units))
	var best func(i int) int64
	best = func(i int) int64 {
		if i == len(need) {
			return 0
		}
		top := best(i + 1)
		for j, a := range units {
			if used[j] || a.Quality < need[i].Quality {
				continue
			}
			if u := Net(need[i].Price, tax) - a.Price; u > 0 {
				used[j] = true
				top = max(top, u+best(i+1))
				used[j] = false
			}
		}
		return top
	}
	return best(0)
}

// 输入顺序不影响结果
func TestSolve_顺序无关(t *testing.T) {
	asks := []Ask{
		{City: "Martlock", Quality: 2, Price: 40_000, Qty: 3},
		{City: "Lymhurst", Quality: 4, Price: 55_000, Qty: 1},
		{City: "Caerleon", Quality: 1, Price: 38_000, Qty: 2},
	}
	bids := []Bid{{Quality: 1, Price: 70_000, Qty: 2}, {Quality: 2, Price: 80_000, Qty: 2}, {Quality: 4, Price: 90_000, Qty: 1}}
	a := Plan("X", asks, bids, tax)
	slices.Reverse(asks)
	slices.Reverse(bids)
	b := Plan("X", asks, bids, tax)
	if !slices.Equal(a, b) {
		t.Fatalf("顺序变了结果就变:\n%+v\n%+v", a, b)
	}
}

func TestRows_合并成采购单的行(t *testing.T) {
	t0 := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	asks := []Ask{
		{City: "Martlock", Quality: 4, Price: 80_000, Qty: 1, Seen: t0},
		{City: "Martlock", Quality: 4, Price: 82_000, Qty: 2, Seen: t0.Add(-time.Hour)},
		{City: "Lymhurst", Quality: 1, Price: 90_000, Qty: 1, Seen: t0, Est: true},
	}
	bids := []Bid{
		{Quality: 1, Price: 118_000, Qty: 2, Seen: t0.Add(-10 * time.Minute)},
		{Quality: 1, Price: 115_000, Qty: 5, Seen: t0},
	}
	rows := Plan("T6_2H_AXE@1", asks, bids, tax)
	if len(rows) != 2 {
		t.Fatalf("应合成两行(Martlock 杰出→普通、Lymhurst 普通→普通),得到 %+v", rows)
	}
	m := rows[0]
	if m.City != "Martlock" || m.BuyQuality != 4 || m.SellQuality != 1 || m.Qty != 3 {
		t.Fatalf("第一行该是 Martlock 3 件: %+v", m)
	}
	if m.BuyLow != 80_000 || m.BuyHigh != 82_000 || m.SellLow != 115_000 || m.SellHigh != 118_000 {
		t.Fatalf("价位范围: %+v", m)
	}
	if !m.BuySeen.Equal(t0.Add(-time.Hour)) || !m.SellSeen.Equal(t0.Add(-10*time.Minute)) {
		t.Fatalf("数据龄要按最旧的一档: %+v", m)
	}
	if m.Cost != 80_000+2*82_000 || m.Profit != m.Revenue-m.Cost || m.Margin <= 0 || m.BuyEst {
		t.Fatalf("合计: %+v", m)
	}
	l := rows[1]
	if l.City != "Lymhurst" || l.Qty != 1 || !l.BuyEst || l.SellEst {
		t.Fatalf("AODP 那档该标成件数未知: %+v", l)
	}
	if m.Profit < l.Profit {
		t.Fatal("要按这单总赚排")
	}
}
