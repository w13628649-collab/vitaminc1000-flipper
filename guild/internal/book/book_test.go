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

// 页满没满按整次响应数:同一 城市 × 方向 × 时间戳,跨品质、**跨物品**。
// 装备一页 50 单横跨几个品质;按分类翻列表页时一页横跨几个物品(实测 Martlock 一页 50 单
// 里 T2_FIBER、T4_ROCK、T1_WOOD… 12 个物品)。以前按物品分,跨物品的满页数不满
func TestWithPages_按整次响应跨品质跨物品数(t *testing.T) {
	var all []Order
	for q, n := range map[int]int{1: 4, 2: 22, 3: 21} {
		for i := 0; i < n; i++ {
			all = append(all, Order{ItemID: "T5_SHOES_LEATHER_HELL@2", City: "Brecilien", Quality: q,
				Side: model.SideOffer, Price: int64(80_000 + q*1000 + i), Amount: 1, LastSeen: T})
		}
	}
	// 同一页里另一个物品的 3 张:这一页因此是满的,整页最贵的是它
	for i := 0; i < 3; i++ {
		all = append(all, Order{ItemID: "T5_SHOES_LEATHER_HELL@3", City: "Brecilien", Quality: 1,
			Side: model.SideOffer, Price: int64(95_000 + i), Amount: 1, LastSeen: T})
	}
	// 别的时刻、别的方向、别的城市各是各的响应;已经带了数的不动(那是 store 对整张表数的)
	all = append(all,
		Order{ItemID: "T4_BAG", City: "Brecilien", Quality: 1, Side: model.SideOffer, Price: 5000, Amount: 1, LastSeen: T.Add(time.Second)},
		Order{ItemID: "T4_BAG", City: "Brecilien", Quality: 1, Side: model.SideRequest, Price: 4000, Amount: 1, LastSeen: T},
		Order{ItemID: "T4_BAG", City: "Brecilien", Quality: 1, Side: model.SideRequest, Price: 3900, Amount: 1, LastSeen: T},
		Order{ItemID: "T4_BAG", City: "Martlock", Quality: 1, Side: model.SideOffer, Price: 5100, Amount: 1, LastSeen: T,
			Page: 50, PageWorst: 6000},
	)
	got := WithPages(all)
	if all[0].Page != 0 || all[0].PageWorst != 0 {
		t.Fatal("WithPages 不能改输入")
	}
	for _, o := range got {
		var page int
		var worst int64
		switch {
		case o.City == "Martlock":
			page, worst = 50, 6000
		case o.ItemID == "T4_BAG" && o.Side == model.SideRequest:
			page, worst = 2, 3900 // 买单整页最差是最低价
		case o.ItemID == "T4_BAG":
			page, worst = 1, 5000
		default:
			page, worst = 50, 95_002
		}
		if o.Page != page || o.PageWorst != worst {
			t.Fatalf("%s %s q%d 的 Page / PageWorst 应为 %d / %d,得到 %d / %d",
				o.ItemID, o.City, o.Quality, page, worst, o.Page, o.PageWorst)
		}
	}
	// 只看 q1 的 4 张,Page 带着整页的 50 → 截断
	var q1 []Order
	for _, o := range got {
		if o.ItemID == "T5_SHOES_LEATHER_HELL@2" && o.Quality == 1 {
			q1 = append(q1, o)
		}
	}
	if b := Build(q1, model.SideOffer, slack); !b.Truncated || b.Orders != 4 {
		t.Fatalf("跨品质、跨物品的满页应判截断,得到 %+v", b)
	}
	// 没带 Page 就按 q1 自己数:4 张,不截断
	for i := range q1 {
		q1[i].Page, q1[i].PageWorst = 0, 0
	}
	if b := Build(q1, model.SideOffer, slack); b.Truncated {
		t.Fatal("没带 Page 时按传进来的单自己数,4 张不该判截断")
	}
}

// pageOf 给一组单标上整次响应的张数和整页最差价(模拟 store 对整张表数出来的)。
func pageOf(orders []Order, page int, worst int64) []Order {
	for i := range orders {
		orders[i].Page, orders[i].PageWorst = page, worst
	}
	return orders
}

func run(item string, q int, from int64, n int, at time.Time) []Order {
	out := make([]Order, n)
	for i := range out {
		out[i] = Order{ItemID: item, City: "Martlock", Quality: q, Side: model.SideOffer,
			Price: from + int64(i), Amount: 1, LastSeen: at}
	}
	return out
}

// 审查 high:2 小时前有人不筛品质翻了一页(q1 5 张 1000..1004 + q2 45 张 2000..2044,满页),
// 现在有人筛 q1 从头翻,q1 只剩 1010..1017,那 5 张已被买走。旧那页的 q2 单没被重看,
// 它一直"满页",以前就被当成续页之前那一页整页保留,1000 成了 q1 的幽灵卖一,
// 扫描读簿换成 book.Build 后连机会页都会报它。
// 现在续页还要接得上:按价排序的列表,下一页不可能比上一页整页最贵的 2044 还便宜
func TestBuild_筛品质重翻不当续页(t *testing.T) {
	old := pageOf(run("T5_SHOES_LEATHER_HELL@2", 1, 1000, 5, T.Add(-2*time.Hour)), PageSize, 2044)
	cur := pageOf(run("T5_SHOES_LEATHER_HELL@2", 1, 1010, 8, T), 8, 1017)
	b := Build(append(append([]Order(nil), old...), cur...), model.SideOffer, slack)
	if b.Levels[0].Price != 1010 || b.Dropped != 5 || b.PrevPage != 0 || !b.Levels[0].Seen.Equal(T) {
		t.Fatalf("卖一应是这一眼的 1010、旧的 5 张剔除,得到 %d / 剔 %d / prev %d / %v",
			b.Levels[0].Price, b.Dropped, b.PrevPage, b.Levels[0].Seen)
	}

	// 对照:真续页。第 1 页(q1 10 张 + q2 40 张,整页最贵 1039)10 分钟前,
	// 第 2 页 q1 从 1100 起、30 秒前到 —— 接在第 1 页末尾之后,第 1 页照旧保留
	p1 := pageOf(run("T5_SHOES_LEATHER_HELL@2", 1, 1000, 10, T.Add(-10*time.Minute)), PageSize, 1039)
	p2 := pageOf(run("T5_SHOES_LEATHER_HELL@2", 1, 1100, 20, T.Add(-30*time.Second)), 20, 1119)
	b = Build(append(append([]Order(nil), p1...), p2...), model.SideOffer, slack)
	if b.Levels[0].Price != 1000 || b.Dropped != 0 || b.PrevPage != 10 {
		t.Fatalf("接得上的续页应保留第 1 页的 1000,得到 %d / 剔 %d / prev %d", b.Levels[0].Price, b.Dropped, b.PrevPage)
	}

	// 同价接缝:第 2 页从第 1 页最后那个价开始(同价单被分页切开)仍算接得上
	p2 = pageOf(run("T5_SHOES_LEATHER_HELL@2", 1, 1039, 5, T.Add(-30*time.Second)), 5, 1043)
	b = Build(append(append([]Order(nil), p1...), p2...), model.SideOffer, slack)
	if b.Levels[0].Price != 1000 || b.PrevPage != 10 {
		t.Fatalf("同价接缝应算续页,得到 %d / prev %d", b.Levels[0].Price, b.PrevPage)
	}

	// 买方镜像:列表从高到低,下一页不可能比上一页整页最低的买价还高
	bOld := pageOf(run("T5_SHOES_LEATHER_HELL@2", 1, 3000, 5, T.Add(-2*time.Hour)), PageSize, 1000)
	bCur := pageOf(run("T5_SHOES_LEATHER_HELL@2", 1, 2000, 3, T), 3, 2000)
	for _, rows := range [][]Order{bOld, bCur} {
		for i := range rows {
			rows[i].Side = model.SideRequest
		}
	}
	b = Build(append(append([]Order(nil), bOld...), bCur...), model.SideRequest, slack)
	if b.Levels[0].Price != 2002 || b.Dropped != 5 {
		t.Fatalf("买方:比旧页整页最低 1000 还高的新买一接不上,旧的剔除,得到 %d / 剔 %d", b.Levels[0].Price, b.Dropped)
	}
}

// 审查 medium:满页之后隔几分钟"只重看了最优几张"。列表按价排序、一页 50 张,
// 一次响应里这个盘口边只出现几张,只可能是:
//   - 这一页是跨物品的,别的物品把这一页占满了 → 整页 50,没翻完,更深的旧单标 stale
//     (以前按物品数成 5,判成翻完了,45 张真单被当残单;实测 T5_MEAL_SOUP @ Brecilien
//     一页 33 张它 + 17 张 T5_MEAL_SOUP_FISH,下一页那 10 张就是这么被剔掉的)
//   - 整页真的只有这几张(列表翻到底了):更深的单要是还在,就在这一页里 → 剔除。
//     这一条依赖"同一次响应同一个时间戳"(ingest.Normalize 之后新单和 touch 同一个时刻)
func TestBuild_跨物品满页里只占几张时更深的旧单标stale(t *testing.T) {
	old := pageOf(run("T6_LEATHER", 1, 100, 50, T.Add(-3*time.Minute)), 45, 149) // 5 张挪走了,旧响应剩 45
	cur := pageOf(run("T6_LEATHER", 1, 100, 5, T), PageSize, 104)                // 跨物品满页,整页最贵 104
	b := Build(append(old[5:], cur...), model.SideOffer, slack)
	if !b.Truncated || b.Levels[0].Price != 100 || len(b.Levels) != 5 || b.Dropped != 0 || b.StaleOrders != 45 {
		t.Fatalf("跨物品满页:卖一 100、当前 5 档、45 张 stale 不剔,得到 truncated=%v %d / %d 档 / 剔 %d / stale %d",
			b.Truncated, b.Levels[0].Price, len(b.Levels), b.Dropped, b.StaleOrders)
	}

	cur = pageOf(run("T6_LEATHER", 1, 100, 5, T), 5, 104)
	b = Build(append(old[5:], cur...), model.SideOffer, slack)
	if b.Truncated || b.Dropped != 45 || b.StaleOrders != 0 || b.Levels[0].Price != 100 {
		t.Fatalf("整页只有 5 张 = 翻到底了,更深的 45 张剔除,得到 truncated=%v 剔 %d stale %d",
			b.Truncated, b.Dropped, b.StaleOrders)
	}
}

// 审查 low:最近一轮满页、整页一个价(实测 T4_RUNE @ Martlock 50 张全在 9 银),
// 更早看到的同价单可能只是排到了下一页 → stale,不剔。以前它们落进"不比卖一差"那一支,
// 前一页凑不满就整组剔掉,每次重算都把几十张真单记成残单(capture.ghosts +26)
func TestBuild_满页整页一个价时同价旧单标stale(t *testing.T) {
	var rows []fx
	for i := 0; i < PageSize; i++ {
		rows = append(rows, fx{9, 100, T.Add(-30 * time.Second), 0})
	}
	for i := 0; i < 12; i++ {
		rows = append(rows, fx{9, 50, T.Add(-10 * time.Minute), 0})
	}
	rows = append(rows, fx{8, 7, T.Add(-10 * time.Minute), 0}) // 比整页的价还便宜:没再出现就是没了
	b := Build(side("T5_RUNE", "Caerleon", 1, model.SideOffer, rows...), model.SideOffer, slack)
	if !b.Truncated || b.StaleOrders != 12 || b.StaleQty != 600 || b.Dropped != 1 || b.DroppedQty != 7 {
		t.Fatalf("同价 12 张应 stale、更便宜的 1 张剔除,得到 truncated=%v stale %d/%d 剔 %d/%d",
			b.Truncated, b.StaleOrders, b.StaleQty, b.Dropped, b.DroppedQty)
	}
	if len(b.Levels) != 1 || b.Levels[0].Qty != 5000 || b.QtyTotal != 5000 {
		t.Fatalf("当前盘口只有这一轮的 50 张,stale 不进合计,得到 %v / %d", levelsOf(b.Levels), b.QtyTotal)
	}

	// 这一轮不满页(列表翻到底了):同价的旧单还在的话就在这一页里 → 剔除
	b = Build(side("T5_RUNE", "Caerleon", 1, model.SideOffer, append(rows[20:PageSize:PageSize], rows[PageSize:]...)...),
		model.SideOffer, slack)
	if b.Truncated || b.StaleOrders != 0 || b.Dropped != 13 {
		t.Fatalf("不满页时同价旧单剔除,得到 truncated=%v stale %d 剔 %d", b.Truncated, b.StaleOrders, b.Dropped)
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
