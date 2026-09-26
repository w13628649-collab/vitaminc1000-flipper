package book

import (
	"math"
	"testing"
	"time"

	"albion-guild/internal/depth"
	"albion-guild/internal/model"
)

// 这组场景原来是 store.BookSides(SQL 版幽灵剔除)的测试。规则收成 Build 这一份之后
// 原样搬过来,一个不丢;期望值变了的地方都写明了为什么变 —— 以查价页那套
// 「最近一轮」规则为准:它认得续页和截断,SQL 版认不得。
// 窗口过滤(90@T−7h)、0 件过滤和"重复 key 不翻倍"是读库那一步的事,
// 在 store 的 BookOrders 集成测试里。

var T = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

const slack = 120 * time.Second

type fx struct {
	price, qty int64
	at         time.Time
	page       int // 0 = 让 Build 按传进来的单自己数
}

func side(item, city string, q int, s model.Side, rows ...fx) []Order {
	out := make([]Order, 0, len(rows))
	for _, r := range rows {
		out = append(out, Order{ItemID: item, City: city, Quality: q, Side: s,
			Price: r.price, Amount: r.qty, FirstSeen: r.at, LastSeen: r.at, Page: r.page})
	}
	return out
}

type lvl struct{ price, qty int64 }

func levelsOf(ls []Level) []lvl {
	out := make([]lvl, len(ls))
	for i, l := range ls {
		out[i] = lvl{l.Price, l.Qty}
	}
	return out
}

func same(a, b []lvl) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// T6_METALBAR_LEVEL4@4 @ Martlock 的真实买方阶梯:顶上 7 档合计 11 件,
// 然后断崖到 53,001,底下 2,000 件 1 银占位单。同一眼看到
func t6Bids() []fx {
	return []fx{
		{260066, 1, T, 0}, {260065, 2, T, 0}, {260064, 1, T, 0}, {260060, 2, T, 0}, {260050, 2, T, 0},
		{260040, 1, T, 0}, {260000, 2, T, 0}, {53001, 15, T, 0}, {1, 2000, T, 0},
	}
}

func TestBuild_T6买方阶梯原样读出并能算近价(t *testing.T) {
	b := Build(side("T6_METALBAR_LEVEL4@4", "Martlock", 1, model.SideRequest, t6Bids()...),
		model.SideRequest, slack)
	if len(b.Levels) != 9 || b.QtyTotal != 2026 || b.Dropped != 0 || b.Orders != 9 {
		t.Fatalf("应为 9 档 / 2026 件 / 0 残单,得到 %d 档 / %d 件 / %d 残单", len(b.Levels), b.QtyTotal, b.Dropped)
	}
	if b.Levels[0].Price != 260066 || b.Levels[8].Price != 1 {
		t.Fatalf("买方应从高到低,得到 %v", levelsOf(b.Levels))
	}
	if !b.Newest.Equal(T) || !b.Levels[0].Seen.Equal(T) {
		t.Fatalf("Newest/Seen 应为 T,得到 %v / %v", b.Newest, b.Levels[0].Seen)
	}
	dl := make([]depth.Level, len(b.Levels))
	for i, l := range b.Levels {
		dl[i] = depth.Level{Price: l.Price, Qty: l.Qty}
	}
	sup := depth.Analyze(dl, 0.05)
	if sup.QtyNear != 11 || sup.LevelsNear != 7 || math.Abs(sup.GapAfterNear-0.7962) > 1e-4 {
		t.Fatalf("近价应为 11 件 / 7 档 / 断崖 79.62%%,得到 %+v", sup)
	}
}

// 同一阶梯再加一张 20 分钟前的 270,000:比最近一眼的最高买价还高,
// 真还在的话这一眼一定看得到 → 已成交或已撤单
func TestBuild_买方比最高买价还高的旧单被剔除(t *testing.T) {
	rows := append(t6Bids(), fx{270000, 3, T.Add(-20 * time.Minute), 0})
	b := Build(side("T6_METALBAR_LEVEL4@4", "Martlock", 2, model.SideRequest, rows...), model.SideRequest, slack)
	if b.Dropped != 1 || b.DroppedQty != 3 || len(b.Levels) != 9 || b.QtyTotal != 2026 || b.Levels[0].Price != 260066 {
		t.Fatalf("270,000 应被剔除,得到 dropped=%d levels=%v", b.Dropped, levelsOf(b.Levels))
	}
}

// 卖方:最近一轮(T−2m 以内)是 104@T−1m、105@T、105@T−30s,覆盖 104 → 105。
func askRows() []fx {
	return []fx{
		{100, 5, T.Add(-10 * time.Minute), 0}, // 比这一轮的卖一还便宜却没被看到 → 残单
		{104, 4, T.Add(-time.Minute), 0},
		{105, 7, T, 0},
		{105, 6, T.Add(-30 * time.Second), 0}, // 同一轮的同价单:并进 105 那一档
		{105, 2, T.Add(-10 * time.Minute), 0}, // 正好压在这一轮最差价上
		{110, 3, T.Add(-10 * time.Minute), 0}, // 比这一轮最差价还差
		{120, 0, T.Add(30 * time.Second), 0},  // 0 件:不能拿它当"最近一眼"
	}
}

// SQL 版(BookSides)把"压在边上"和"更差"的旧单都当"可能没翻到那一页"保留,
// 于是 105 那档是 7+6(边上那张另算进去就是 15)、还多出 110×3。
// 现在看这一轮是不是满页:不满 = 整本簿都看到了,它们不在里面就是没了
func TestBuild_卖方残单剔除且同一轮同价合并(t *testing.T) {
	b := Build(side("T5_CLOTH", "Lymhurst", 1, model.SideOffer, askRows()...), model.SideOffer, slack)
	if want := []lvl{{104, 4}, {105, 13}}; !same(levelsOf(b.Levels), want) {
		t.Fatalf("应为 %v,得到 %v", want, levelsOf(b.Levels))
	}
	if b.Dropped != 3 || b.DroppedQty != 10 || b.QtyTotal != 17 || b.Truncated || len(b.Stale) != 0 {
		t.Fatalf("应剔 3 张(100、边上的 105、110)/ 17 件 / 没截断,得到 %d / %d / %v / stale %d",
			b.Dropped, b.QtyTotal, b.Truncated, len(b.Stale))
	}
	if l := b.Levels[1]; l.Orders != 2 || !l.Seen.Equal(T) {
		t.Fatalf("105 那一档应是 2 张单、Seen=T,得到 %+v", l)
	}
	if !b.Newest.Equal(T) {
		t.Fatalf("0 件的单不该算进最近一眼,Newest 应为 T,得到 %v", b.Newest)
	}
	if b.Worst != 105 {
		t.Fatalf("Worst 应是这一轮最差的 105,得到 %d", b.Worst)
	}
}

// 同一组单,但最近一轮所在的响应是满页(装备跨品质的一页、或者翻到一半):
// 边上和更差的旧单可能只是这一轮没翻到那么深 → 保留成 stale,不进档、不进合计。
// SQL 版在这里把它们直接并进阶梯,近价件数就把可能已经没了的单也算进去了
func TestBuild_卖方截断时边上和更差的旧单标stale(t *testing.T) {
	rows := askRows()
	for i := range rows {
		if rows[i].at.Equal(T) {
			rows[i].page = PageSize // 105×7@T 那次响应一共 50 张(别的品质占了 49 张)
		}
	}
	b := Build(side("T5_CLOTH", "Lymhurst", 1, model.SideOffer, rows...), model.SideOffer, slack)
	if !b.Truncated {
		t.Fatal("最深那一档来自满页,应判截断")
	}
	if want := []lvl{{104, 4}, {105, 13}}; !same(levelsOf(b.Levels), want) {
		t.Fatalf("当前盘口应为 %v,得到 %v", want, levelsOf(b.Levels))
	}
	if want := []lvl{{105, 2}, {110, 3}}; !same(levelsOf(b.Stale), want) || b.StaleOrders != 2 || b.StaleQty != 5 {
		t.Fatalf("stale 应为 %v(2 张 5 件),得到 %v / %d / %d", want, levelsOf(b.Stale), b.StaleOrders, b.StaleQty)
	}
	if b.Dropped != 1 || b.DroppedQty != 5 || b.QtyTotal != 17 {
		t.Fatalf("只剔 100 那张;合计不含 stale,得到 dropped=%d/%d total=%d", b.Dropped, b.DroppedQty, b.QtyTotal)
	}
}

// 买方镜像(和卖方同物品同城同品质,只差方向,顺带验证两边不串)
func bidRows(page int) []fx {
	return []fx{
		{1000, 2, T, page}, {990, 3, T, page}, // 最近一轮覆盖 1000 → 990
		{1010, 1, T.Add(-20 * time.Minute), 0}, // 比买一还高 → 残单
		{995, 4, T.Add(-20 * time.Minute), 0},  // 在这一轮覆盖的段里没出现 → 残单
		{990, 1, T.Add(-20 * time.Minute), 0},  // 边上
		{980, 5, T.Add(-20 * time.Minute), 0},  // 更差
	}
}

func TestBuild_买方镜像(t *testing.T) {
	// 没截断:整本簿都看到了,边上和更差的也剔(SQL 版保留这两张,阶梯是 1000/990×4/980)
	b := Build(side("T5_CLOTH", "Lymhurst", 1, model.SideRequest, bidRows(0)...), model.SideRequest, slack)
	if want := []lvl{{1000, 2}, {990, 3}}; !same(levelsOf(b.Levels), want) || b.Dropped != 4 {
		t.Fatalf("应为 %v / 剔 4 张,得到 %v / %d", want, levelsOf(b.Levels), b.Dropped)
	}
	// 截断:段内和更高的剔,边上和更差的标 stale
	b = Build(side("T5_CLOTH", "Lymhurst", 1, model.SideRequest, bidRows(PageSize)...), model.SideRequest, slack)
	if want := []lvl{{1000, 2}, {990, 3}}; !same(levelsOf(b.Levels), want) || b.Dropped != 2 {
		t.Fatalf("截断时当前盘口应为 %v / 剔 2 张,得到 %v / %d", want, levelsOf(b.Levels), b.Dropped)
	}
	if want := []lvl{{990, 1}, {980, 5}}; !same(levelsOf(b.Stale), want) {
		t.Fatalf("stale 应从高到低 %v,得到 %v", want, levelsOf(b.Stale))
	}
}

// 两个方向、两个品质、两个物品各是各的盘口边,Group 不能把它们混在一起
func TestGroup_盘口边不串(t *testing.T) {
	var all []Order
	all = append(all, side("T5_CLOTH", "Lymhurst", 1, model.SideOffer, askRows()...)...)
	all = append(all, side("T5_CLOTH", "Lymhurst", 1, model.SideRequest, bidRows(0)...)...)
	all = append(all, side("T5_CLOTH", "Lymhurst", 3, model.SideOffer, fx{50, 99, T, 0})...)
	all = append(all, side("T6_CLOTH", "Lymhurst", 1, model.SideOffer, fx{60, 1, T, 0})...)
	g := Group(all)
	if len(g) != 4 {
		t.Fatalf("应分成 4 个盘口边,得到 %d", len(g))
	}
	ask := g[Key{"T5_CLOTH", "Lymhurst", 1, model.SideOffer}]
	if len(ask) != len(askRows()) {
		t.Fatalf("卖方 q1 应是 %d 张,得到 %d", len(askRows()), len(ask))
	}
	if b := Build(ask, model.SideOffer, slack); b.Levels[0].Price != 104 {
		t.Fatalf("q3 的 50 不能混进 q1 的卖方,得到卖一 %d", b.Levels[0].Price)
	}
}

// 规则前提不成立时的逃生口:slack 不小于窗口,整个窗口都是"最近一轮",什么都不剔。
// 多开串城的盘口也靠这个暂停剔除
func TestBuild_slack不小于窗口时不剔除任何单(t *testing.T) {
	a := Build(side("T5_CLOTH", "Lymhurst", 1, model.SideOffer, askRows()...), model.SideOffer, 6*time.Hour)
	if want := []lvl{{100, 5}, {104, 4}, {105, 15}, {110, 3}}; a.Dropped != 0 || len(a.Stale) != 0 ||
		!same(levelsOf(a.Levels), want) {
		t.Fatalf("卖方应全保留 %v,得到 %v / 剔 %d", want, levelsOf(a.Levels), a.Dropped)
	}
	b := Build(side("T5_CLOTH", "Lymhurst", 1, model.SideRequest, bidRows(0)...), model.SideRequest, 6*time.Hour)
	if b.Dropped != 0 || len(b.Levels) != 5 || b.Levels[0].Price != 1010 {
		t.Fatalf("买方应全保留 5 档、买一 1010,得到 %v / 剔 %d", levelsOf(b.Levels), b.Dropped)
	}
}

// WS 推送和 /api/quotes 的最优价就是 Build 之后的第一档。审查 E 轮原样:第一眼卖一 2900、
// 2905,随后新单 2910;125s 后第二眼只剩 2905、2910,全是 touch(没有状态变化)
func TestBuild_第二眼没再看到的卖一被剔除(t *testing.T) {
	first := side("T6_METALBAR", "Thetford", 1, model.SideOffer,
		fx{2900, 5, T, 0}, fx{2905, 20, T, 0}, fx{2910, 7, T.Add(10 * time.Second), 0})
	b := Build(first, model.SideOffer, slack)
	if l := b.Levels[0]; l.Price != 2900 || l.Qty != 5 || !b.Newest.Equal(T.Add(10*time.Second)) {
		t.Fatalf("第一眼之后卖一应是 2900×5,得到 %+v / %v", l, b.Newest)
	}
	T2 := T.Add(125 * time.Second)
	second := append([]Order(nil), first...)
	second[1].LastSeen, second[2].LastSeen = T2, T2 // touch 只推 last_seen
	b2 := Build(second, model.SideOffer, slack)
	if l := b2.Levels[0]; l.Price != 2905 || l.Qty != 20 || b2.Dropped != 1 {
		t.Fatalf("卖一应退到 2905×20、剔掉 1 张,得到 %+v / %d", l, b2.Dropped)
	}
	if !b2.Newest.Equal(T2) || !b2.Newest.After(b.Newest) {
		t.Fatalf("最近一眼应前进到 %v,得到 %v", T2, b2.Newest)
	}
}

// 续页晚到超过 slack(扫描的 slack 只有 2 分钟,更容易撞上):第 1 页 10 分钟前、
// 第 2 页刚到。SQL 版把第 1 页的真实最优档整页当幽灵剔掉,卖一报成 150
func TestBuild_续页晚到不剔第一页(t *testing.T) {
	var rows []fx
	for i := int64(0); i < PageSize; i++ {
		rows = append(rows, fx{100 + i, 1, T.Add(-10 * time.Minute), 0}, fx{150 + i, 2, T.Add(-10 * time.Second), 0})
	}
	b := Build(side("T4_PLANKS", "Fort Sterling", 1, model.SideOffer, rows...), model.SideOffer, slack)
	if b.Levels[0].Price != 100 || b.Dropped != 0 || b.PrevPage != PageSize || b.Orders != 2*PageSize {
		t.Fatalf("卖一应是第 1 页的 100、不剔、前一页 50 张,得到 %d / %d / %d / %d",
			b.Levels[0].Price, b.Dropped, b.PrevPage, b.Orders)
	}
	if !b.Levels[0].Seen.Equal(T.Add(-10*time.Minute)) || !b.Truncated || b.Worst != 199 {
		t.Fatalf("最优档的龄应是第 1 页的;第 2 页满页 → 截断,得到 %v / %v / %d", b.Levels[0].Seen, b.Truncated, b.Worst)
	}
}

// 页满没满要跨品质、按物品数:装备一页 50 单横跨几个品质;两个物品同一时刻各翻一页
// 不能数成一页
func TestWithPages_跨品质按物品数(t *testing.T) {
	var all []Order
	for q, n := range map[int]int{1: 4, 2: 22, 3: 21, 4: 3} {
		for i := 0; i < n; i++ {
			all = append(all, Order{ItemID: "T5_SHOES_LEATHER_HELL@2", City: "Brecilien", Quality: q,
				Side: model.SideOffer, Price: int64(80_000 + q*1000 + i), Amount: 1, LastSeen: T})
		}
	}
	for i := 0; i < 10; i++ {
		all = append(all, Order{ItemID: "T4_BAG", City: "Brecilien", Quality: 1,
			Side: model.SideOffer, Price: int64(5000 + i), Amount: 1, LastSeen: T})
	}
	got := WithPages(all)
	if all[0].Page != 0 {
		t.Fatal("WithPages 不能改输入")
	}
	for _, o := range got {
		want := 50
		if o.ItemID == "T4_BAG" {
			want = 10
		}
		if o.Page != want {
			t.Fatalf("%s q%d 的 Page 应为 %d,得到 %d", o.ItemID, o.Quality, want, o.Page)
		}
	}
	// 只看 q1 的 4 张,Page 带着整页的 50 → 截断
	var q1 []Order
	for _, o := range got {
		if o.ItemID != "T4_BAG" && o.Quality == 1 {
			q1 = append(q1, o)
		}
	}
	if b := Build(q1, model.SideOffer, slack); !b.Truncated || b.Orders != 4 {
		t.Fatalf("跨品质满页应判截断,得到 %+v", b)
	}
	// 没带 Page 就按 q1 自己数:4 张,不截断
	for i := range q1 {
		q1[i].Page = 0
	}
	if b := Build(q1, model.SideOffer, slack); b.Truncated {
		t.Fatal("没带 Page 时按传进来的单自己数,4 张不该判截断")
	}
}

func TestBuild_空的和坏单(t *testing.T) {
	if b := Build(nil, model.SideOffer, slack); len(b.Levels) != 0 || !b.Newest.IsZero() {
		t.Fatalf("空输入应是零值,得到 %+v", b)
	}
	// 0 价只可能是解析出错,留着会成为卖一
	b := Build(side("X", "Y", 1, model.SideOffer, fx{0, 5, T, 0}, fx{10, 0, T, 0}), model.SideOffer, slack)
	if len(b.Levels) != 0 {
		t.Fatalf("0 价、0 件都该忽略,得到 %v", levelsOf(b.Levels))
	}
}
