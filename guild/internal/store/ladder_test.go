package store

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/book"
	"albion-guild/internal/model"
)

// fx 是夹具里的一张单:价格 × 件数 @ 观测时间。
type fx struct {
	price int64
	qty   int32
	at    time.Time
}

type seeder struct {
	t      *testing.T
	st     *Store
	nextID int64
}

// put 经 WriteOrders 写入,和线上入库走同一条路:last_seen 就是 ObservedAt。
func (s *seeder) put(k model.QuoteKey, rows ...fx) {
	s.t.Helper()
	orders := make([]model.MarketOrder, 0, len(rows))
	for _, r := range rows {
		s.nextID++
		orders = append(orders, model.MarketOrder{
			OrderID: s.nextID, ItemID: k.ItemID, LocationID: k.LocationID, Quality: k.Quality,
			Side: k.Side, UnitPrice: r.price, Amount: r.qty, ObservedAt: r.at,
		})
	}
	if err := s.st.WriteOrders(context.Background(), "测试", orders); err != nil {
		s.t.Fatal(err)
	}
}

type lvl struct {
	price, qty int64
}

func levelsOf(b book.Side) []lvl {
	out := make([]lvl, len(b.Levels))
	for i, l := range b.Levels {
		out[i] = lvl{l.Price, l.Qty}
	}
	return out
}

func sameLevels(a, b []lvl) bool {
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

// buildAll 是线上读簿那一步(flip.storeBooks.readSides)去掉串城处理:
// BookOrders 一次读完,按盘口边分组交给 book.Build。
func buildAll(orders []LiveOrder, slack time.Duration) map[model.QuoteKey]book.Side {
	out := map[model.QuoteKey]book.Side{}
	for g, group := range book.Group(orders) {
		k := model.QuoteKey{ItemID: g.ItemID, LocationID: g.City, Quality: int16(g.Quality), Side: g.Side}
		if s := book.Build(group, g.Side, slack); len(s.Levels) > 0 {
			out[k] = s
		}
	}
	return out
}

// readSide 读一个盘口边并整理,测试专用。
func readSide(t *testing.T, st *Store, k model.QuoteKey, since time.Time, slack time.Duration) book.Side {
	t.Helper()
	orders, err := st.BookOrders(context.Background(), []model.QuoteKey{k}, since)
	if err != nil {
		t.Fatal(err)
	}
	return buildAll(orders, slack)[k]
}

// 原来 BookSides 那组场景过一遍真库:SQL 只取数(窗口、0 件、0 价、请求的盘口边),
// 剔残单交给 book.Build。规则本身的细节(期望值为什么和 SQL 版不同)见 book 包的测试
func TestBookOrders(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	since := T.Add(-6 * time.Hour)
	const slack = 120 * time.Second
	sd := &seeder{t: t, st: st}

	// T6_METALBAR_LEVEL4@4 @ Martlock 的真实买方阶梯,同一眼看到
	t6 := []fx{
		{260066, 1, T}, {260065, 2, T}, {260064, 1, T}, {260060, 2, T}, {260050, 2, T},
		{260040, 1, T}, {260000, 2, T}, {53001, 15, T}, {1, 2000, T},
	}
	kT6 := model.QuoteKey{ItemID: "T6_METALBAR_LEVEL4@4", LocationID: "Martlock", Quality: 1, Side: model.SideRequest}
	sd.put(kT6, t6...)
	// 同一阶梯再加一张 20 分钟前的 270,000:比最近一眼的最高买价还高 → 已成交或已撤单
	kT6Ghost := kT6
	kT6Ghost.Quality = 2
	sd.put(kT6Ghost, append(append([]fx(nil), t6...), fx{270000, 3, T.Add(-20 * time.Minute)})...)

	kAsk := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1, Side: model.SideOffer}
	sd.put(kAsk,
		fx{100, 5, T.Add(-10 * time.Minute)}, // 比这一轮的卖一还便宜却没被看到 → 残单
		fx{104, 4, T.Add(-time.Minute)},
		fx{105, 7, T},
		fx{105, 6, T.Add(-30 * time.Second)},
		fx{105, 2, T.Add(-10 * time.Minute)}, // 压在边上,这一轮不是满页 → 剔除
		fx{110, 3, T.Add(-10 * time.Minute)}, // 更差,这一轮不是满页 → 剔除
		fx{90, 9, T.Add(-7 * time.Hour)},     // 窗口外:读都不读
		fx{120, 0, T.Add(30 * time.Second)},  // 0 件:不能拿它当"最近一眼"
		fx{0, 4, T.Add(40 * time.Second)},    // 0 价:解析出错,不能当卖一
	)
	// 买方镜像(和 kAsk 同物品同城同品质,只差方向,顺带验证两边不串)
	kBid := kAsk
	kBid.Side = model.SideRequest
	sd.put(kBid,
		fx{1000, 2, T}, fx{990, 3, T},
		fx{1010, 1, T.Add(-20 * time.Minute)},
		fx{995, 4, T.Add(-20 * time.Minute)},
		fx{990, 1, T.Add(-20 * time.Minute)},
		fx{980, 5, T.Add(-20 * time.Minute)},
	)
	// 没请求的品质有数据:不能混进结果,但它和 105×7 是同一次响应,要算进 page
	kAskQ3 := kAsk
	kAskQ3.Quality = 3
	sd.put(kAskQ3, fx{50, 99, T})
	// 没请求的**物品**在同一次响应里(列表页一页跨物品):同样要算进 page 和整页最差价
	sd.put(model.QuoteKey{ItemID: "T5_CLOTH_LEVEL1@1", LocationID: "Lymhurst", Quality: 1, Side: model.SideOffer},
		fx{130, 2, T})

	kEmpty := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Thetford", Quality: 1, Side: model.SideOffer}
	keys := []model.QuoteKey{kT6, kT6Ghost, kAsk, kBid, kEmpty, kAsk} // kAsk 重复一次,件数不能翻倍

	orders, err := st.BookOrders(ctx, keys, since)
	if err != nil {
		t.Fatal(err)
	}
	got := buildAll(orders, slack)

	t.Run("只回请求的品质和盘口边", func(t *testing.T) {
		for _, o := range orders {
			if o.Quality == 3 || o.City == "Thetford" || o.Price <= 0 || o.Amount <= 0 || !o.LastSeen.After(since) {
				t.Fatalf("不该读回来: %+v", o)
			}
			if o.LastSeen.Location() != time.UTC {
				t.Fatal("时间应统一成 UTC")
			}
		}
		// T6 两个品质各 9(+1)张,kAsk 窗口内有效的 6 张,kBid 6 张
		if len(orders) != 9+10+6+6 {
			t.Fatalf("应读回 31 张单(重复的 key 不翻倍),得到 %d", len(orders))
		}
		if _, ok := got[kEmpty]; ok || len(got) != 4 {
			t.Fatalf("没数据的 key 不该出现,应只有 4 个盘口边,得到 %d", len(got))
		}
	})

	t.Run("page跨品质跨物品按整次响应数", func(t *testing.T) {
		for _, o := range orders {
			switch {
			case o.ItemID == kT6.ItemID && o.LastSeen.Equal(T):
				// q1、q2 各 9 张同一时刻;买单整页最差是最低的 1 银
				if o.Page != 18 || o.PageWorst != 1 {
					t.Fatalf("T6 同一眼跨品质应是 18 张、整页最低 1,得到 %+v", o)
				}
			case o.ItemID == kAsk.ItemID && o.Side == model.SideOffer && o.LastSeen.Equal(T):
				// 105×7、没请求的 q3 那张 50×99、没请求的物品那张 130×2
				if o.Page != 3 || o.PageWorst != 130 {
					t.Fatalf("没请求的品质和物品也要算进 page 和整页最差价,得到 %+v", o)
				}
			case o.PageWorst < o.Price && o.Side == model.SideOffer:
				t.Fatalf("卖单整页最差价不会比自己便宜: %+v", o)
			}
		}
		// 和查价页的口径(ItemOrders)逐张一致:两边是同一段 SQL 对整张表数的,
		// 在 Go 里补数(WithPages)不会改它们
		all, err := st.ItemOrders(ctx, kAsk.ItemID, since)
		if err != nil {
			t.Fatal(err)
		}
		type pg struct {
			page  int
			worst int64
		}
		want := map[[3]int64]pg{}
		for _, o := range book.WithPages(all) {
			want[[3]int64{int64(o.Side), o.Price, o.LastSeen.UnixMicro()}] = pg{o.Page, o.PageWorst}
		}
		for _, o := range orders {
			if o.ItemID != kAsk.ItemID {
				continue
			}
			if p := want[[3]int64{int64(o.Side), o.Price, o.LastSeen.UnixMicro()}]; p != (pg{o.Page, o.PageWorst}) {
				t.Fatalf("BookOrders 数的 %d/%d 和 ItemOrders 的 %+v 不一致: %+v", o.Page, o.PageWorst, p, o)
			}
		}
	})

	t.Run("T6买方阶梯原样读出", func(t *testing.T) {
		b := got[kT6]
		if len(b.Levels) != 9 || b.QtyTotal != 2026 || b.Dropped != 0 || b.Levels[0].Price != 260066 ||
			b.Levels[8].Price != 1 || !b.Newest.Equal(T) {
			t.Fatalf("应为 9 档 / 2026 件 / 0 残单、从高到低,得到 %v / %d / %d", levelsOf(b), b.QtyTotal, b.Dropped)
		}
	})

	t.Run("买方比最高买价还高的旧单被剔除", func(t *testing.T) {
		b := got[kT6Ghost]
		if b.Dropped != 1 || len(b.Levels) != 9 || b.QtyTotal != 2026 || b.Levels[0].Price != 260066 {
			t.Fatalf("270,000 应被剔除,得到 dropped=%d levels=%v", b.Dropped, levelsOf(b))
		}
	})

	t.Run("卖方残单剔除、窗口外和坏单读不到", func(t *testing.T) {
		b := got[kAsk]
		if want := []lvl{{104, 4}, {105, 13}}; !sameLevels(levelsOf(b), want) {
			t.Fatalf("应为 %v,得到 %v", want, levelsOf(b))
		}
		if b.Dropped != 3 || b.QtyTotal != 17 || !b.Newest.Equal(T) {
			t.Fatalf("应剔 3 张 / 17 件 / Newest=T(0 件、0 价的单不算最近一眼),得到 %d / %d / %v",
				b.Dropped, b.QtyTotal, b.Newest)
		}
		if l := b.Levels[1]; l.Orders != 2 || !l.Seen.Equal(T) {
			t.Fatalf("105 那一档应是 2 张单、Seen=T,得到 %+v", l)
		}
	})

	t.Run("买方镜像", func(t *testing.T) {
		b := got[kBid]
		if want := []lvl{{1000, 2}, {990, 3}}; !sameLevels(levelsOf(b), want) || b.Dropped != 4 {
			t.Fatalf("应为 %v / 剔 4 张,得到 %v / %d", want, levelsOf(b), b.Dropped)
		}
	})

	// 规则前提不成立时要能一键退回"按窗口过滤":slack 设成不小于窗口
	t.Run("slack不小于窗口时不剔除任何单", func(t *testing.T) {
		all := buildAll(orders, 6*time.Hour)
		if a := all[kAsk]; a.Dropped != 0 || !sameLevels(levelsOf(a), []lvl{{100, 5}, {104, 4}, {105, 15}, {110, 3}}) {
			t.Fatalf("卖方应全保留(90 仍在窗口外),得到 %v / %d", levelsOf(a), a.Dropped)
		}
		if b := all[kBid]; b.Dropped != 0 || len(b.Levels) != 5 {
			t.Fatalf("买方应全保留,得到 %v / %d", levelsOf(b), b.Dropped)
		}
	})

	t.Run("空key列表", func(t *testing.T) {
		m, err := st.BookOrders(ctx, nil, since)
		if err != nil || len(m) != 0 {
			t.Fatalf("应返回空,得到 %v / %v", m, err)
		}
	})
}

// WS 推送和 /api/quotes 的最优价就是整理之后的第一档(flip.Service.BestQuotes)。
// 审查 E 轮原样:第一眼卖一 2900、2905,随后新单 2910;125s 后第二眼只剩 2905、2910,
// 全是 touch(没有状态变化)。以前的 BestQuotes 两次都报 2900,扫描却已经按幽灵剔掉了它
func TestBookOrders_第二眼没再看到的卖一被剔除(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 25, 13, 32, 20, 0, time.UTC)
	k := model.QuoteKey{ItemID: "T6_METALBAR", LocationID: "Thetford", Quality: 1, Side: model.SideOffer}
	orders := []model.MarketOrder{
		{OrderID: 1, ItemID: k.ItemID, LocationID: k.LocationID, Quality: 1, Side: k.Side, UnitPrice: 2900, Amount: 5, ObservedAt: T},
		{OrderID: 2, ItemID: k.ItemID, LocationID: k.LocationID, Quality: 1, Side: k.Side, UnitPrice: 2905, Amount: 20, ObservedAt: T},
		{OrderID: 3, ItemID: k.ItemID, LocationID: k.LocationID, Quality: 1, Side: k.Side, UnitPrice: 2910, Amount: 7, ObservedAt: T.Add(10 * time.Second)},
	}
	if err := st.WriteOrders(ctx, "测试", orders); err != nil {
		t.Fatal(err)
	}
	since := T.Add(-30 * time.Minute)
	const slack = 120 * time.Second

	first := readSide(t, st, k, since, slack)
	if len(first.Levels) == 0 || first.Levels[0].Price != 2900 || first.Levels[0].Qty != 5 ||
		!first.Newest.Equal(T.Add(10*time.Second)) {
		t.Fatalf("第一眼之后卖一应是 2900×5,得到 %+v", first)
	}

	T2 := T.Add(125 * time.Second)
	if err := st.TouchOrders(ctx, map[int64]time.Time{2: T2, 3: T2}); err != nil {
		t.Fatal(err)
	}
	b := readSide(t, st, k, since, slack)
	if b.Levels[0].Price != 2905 || b.Levels[0].Qty != 20 || b.Dropped != 1 {
		t.Fatalf("第二眼没再看到 2900:卖一应退到 2905×20、剔掉 1 张,得到 %+v", b)
	}
	if !b.Newest.Equal(T2) || !b.Newest.After(first.Newest) {
		t.Fatalf("最近一眼应前进到第二眼 %v,得到 %v", T2, b.Newest)
	}
}

// 审查 high 的原样(经真 SQL):2 小时前不筛品质翻了一页(q1 5 张 + q2 45 张 = 满页),
// 现在筛 q1 从头翻,q1 只剩 1010..1017。旧页的 q2 单没被重看,旧页一直"满页";
// 以前 q1 那 5 张被当成续页之前那一页保留,1000 成了扫描的卖一(旧 SQL 规则是 1010)。
// 对照的真续页:第 1 页(q1 10 张 + q2 40 张)10 分钟前,第 2 页接在整页最贵的那张之后
func TestBookOrders_筛品质重翻和真续页(t *testing.T) {
	st := openTestStore(t)
	T := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	sd := &seeder{t: t, st: st}
	run := func(k model.QuoteKey, from int64, n int, at time.Time) {
		rows := make([]fx, n)
		for i := range rows {
			rows[i] = fx{from + int64(i), 1, at}
		}
		sd.put(k, rows...)
	}
	a1 := model.QuoteKey{ItemID: "T5_SHOES_LEATHER_HELL@2", LocationID: "Martlock", Quality: 1, Side: model.SideOffer}
	a2 := a1
	a2.Quality = 2
	run(a1, 1000, 5, T.Add(-2*time.Hour))
	run(a2, 2000, 45, T.Add(-2*time.Hour))
	run(a1, 1010, 8, T)

	b1 := model.QuoteKey{ItemID: "T6_SHOES_LEATHER_HELL@2", LocationID: "Martlock", Quality: 1, Side: model.SideOffer}
	b2 := b1
	b2.Quality = 2
	run(b1, 1000, 10, T.Add(-10*time.Minute))
	run(b2, 1010, 40, T.Add(-10*time.Minute)) // 整页最贵 1049
	run(b1, 1100, 20, T.Add(-30*time.Second))

	got := readSide(t, st, a1, T.Add(-6*time.Hour), 120*time.Second)
	if got.Levels[0].Price != 1010 || got.Dropped != 5 || got.PrevPage != 0 {
		t.Fatalf("筛品质重翻:卖一应是 1010、旧的 5 张剔除,得到 %d / 剔 %d / prev %d",
			got.Levels[0].Price, got.Dropped, got.PrevPage)
	}
	got = readSide(t, st, b1, T.Add(-6*time.Hour), 120*time.Second)
	if got.Levels[0].Price != 1000 || got.Dropped != 0 || got.PrevPage != 10 {
		t.Fatalf("真续页:第 1 页的 1000 应保留,得到 %d / 剔 %d / prev %d", got.Levels[0].Price, got.Dropped, got.PrevPage)
	}
}

// 审查 medium 的原样(经真 SQL):T−3m 这个物品自己满页 50 张(100..149),T 时刻最优 5 张
// 被重看、其余 45 张没有。
//   - 这 5 张所在的那次响应是跨物品的满页(另一个物品 45 张占满了这一页):没翻完,
//     45 张 stale,合计和近价件数照旧只算这一轮 —— 以前按物品数成 5 张,判成翻完了,45 张被剔
//   - 那次响应就只有这 5 张:列表翻到底了,另外 45 张要是还在就在这一页里 → 剔除
func TestBookOrders_满页之后隔几分钟只重看最优几张(t *testing.T) {
	for _, crossItem := range []bool{true, false} {
		st := openTestStore(t)
		ctx := context.Background()
		T := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
		k := model.QuoteKey{ItemID: "T6_LEATHER", LocationID: "Martlock", Quality: 1, Side: model.SideOffer}
		other := model.QuoteKey{ItemID: "T6_LEATHER_LEVEL1@1", LocationID: "Martlock", Quality: 1, Side: model.SideOffer}
		var orders []model.MarketOrder
		for i := int64(0); i < 50; i++ {
			orders = append(orders, model.MarketOrder{OrderID: i + 1, ItemID: k.ItemID, LocationID: k.LocationID, Quality: 1,
				Side: k.Side, UnitPrice: 100 + i, Amount: 1, ObservedAt: T.Add(-3 * time.Minute)})
		}
		if crossItem {
			for i := int64(0); i < 45; i++ {
				orders = append(orders, model.MarketOrder{OrderID: 100 + i, ItemID: other.ItemID, LocationID: other.LocationID,
					Quality: 1, Side: other.Side, UnitPrice: 100 + i%5, Amount: 1, ObservedAt: T})
			}
		}
		if err := st.WriteOrders(ctx, "测试", orders); err != nil {
			t.Fatal(err)
		}
		touch := map[int64]time.Time{}
		for i := int64(1); i <= 5; i++ {
			touch[i] = T
		}
		if err := st.TouchOrders(ctx, touch); err != nil {
			t.Fatal(err)
		}
		b := readSide(t, st, k, T.Add(-6*time.Hour), 120*time.Second)
		if b.Levels[0].Price != 100 || len(b.Levels) != 5 || b.QtyTotal != 5 {
			t.Fatalf("crossItem=%v:当前盘口是这一轮的 5 张、卖一 100,得到 %d / %d 档 / %d 件",
				crossItem, b.Levels[0].Price, len(b.Levels), b.QtyTotal)
		}
		if crossItem && (!b.Truncated || b.StaleOrders != 45 || b.Dropped != 0) {
			t.Fatalf("跨物品满页:45 张应 stale、不剔,得到 truncated=%v stale %d 剔 %d", b.Truncated, b.StaleOrders, b.Dropped)
		}
		if !crossItem && (b.Truncated || b.StaleOrders != 0 || b.Dropped != 45) {
			t.Fatalf("整页只有 5 张:45 张剔除,得到 truncated=%v stale %d 剔 %d", b.Truncated, b.StaleOrders, b.Dropped)
		}
	}
}

// TestBookOrders_查询计划 是手工检查,只在 FLIPPER_TEST_EXPLAIN=1 时跑:
// 对 1750 个 key(125 物品 × 7 城 × 1 品质 × 2 边,扫描实际的量级)跑
// EXPLAIN (ANALYZE, BUFFERS),确认走 idx_live_book 的 Index Only Scan,
// 再量 BookOrders 往返和 Go 侧 Group + Build 的耗时。
//
// 数据:85 个材料只有 q1、40 件装备 q1~q5,每个盘口边 30 张单、每 15 分钟一张,
// 24 张在 6h 窗口内。共 11.97 万行;一次响应一个时间戳(同 城市 × 方向 × 轮次、每 5 个物品一页),
// 窗口里 9100 页。请求 q1,page 由 pagedSQL 按 last_seen 回表对整张表数(跨物品、跨品质)。
//
// 2026-09-26 在 flipper-pg(timescaledb / pg16,WSL docker)上,VACUUM ANALYZE 之后:
//   - 取挂单:Nested Loop → Index Only Scan using idx_live_book(loops=1750,Heap Fetches: 0)
//   - 数整页:9100 页各一次 Index Scan using idx_live_stale(每页约 11 行),GroupAggregate 成一行,
//     和 4.4 万张单 Merge Append 之后 WindowAgg;排序都在内存里。Execution Time ≈ 267ms
//   - Go 侧 BookOrders 往返 ≈ 275~320ms(4.4 万行经 WSL 端口转发),Group + Build ≈ 17ms,合计 ≈ 300ms
//   - WS 标脏的量级(14 个 key、30 分钟窗口)BookOrders + Build ≈ 3ms
//   - 对照真库(1.67 万行、175 个物品,只读 EXPLAIN ANALYZE,READ ONLY 事务):1750 个 key、
//     1.56 万张单、458 页,Execution Time 76ms(页数不跨物品之前是 39ms)
//   - 反例:把数出来的页 JOIN 回挂单那一版,规划器估 o 只有 10 行,选了嵌套循环 + 连接过滤,
//     同一份数据 56 秒。所以这里断言计划里没有 "Rows Removed by Join Filter"
//
// live 表没有清理,这个数会随时间涨,上线后要定期复查。
func TestBookOrders_查询计划(t *testing.T) {
	if os.Getenv("FLIPPER_TEST_EXPLAIN") != "1" {
		t.Skip("手工检查:设 FLIPPER_TEST_EXPLAIN=1 才跑")
	}
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

	// 直接用 SQL 灌,WriteOrders 一条条走太慢。卖单从 1000 往上、买单往下。
	// 一次响应一个时间戳(同 城市 × 方向 × 轮次、每 5 个物品一页):装备那几页 25 张、
	// 材料那几页 5 张,和实测"一页至多 50 张"同一个量级。所有物品共用 30 个时间戳的话,
	// 按 last_seen 回表数整页那一步每个时间戳要翻 4000 行,测出来的是一个不存在的最坏情况
	if _, err := st.pool.Exec(ctx, `
		INSERT INTO market_order_live (order_id, item_id, location_id, quality, enchant, side,
		       unit_price, amount, expires_at, first_seen, last_seen, reporter, raw_location)
		SELECT row_number() OVER (), 'ITEM_' || i, 'CITY_' || c, q, 0, s,
		       1000 + (CASE WHEN s = 0 THEN n ELSE -n END) * 3, 1 + n % 7, NULL, at, at, '', ''
		FROM generate_series(1, 125) i, generate_series(1, 7) c, generate_series(1, 5) q,
		     generate_series(0, 1) s, generate_series(0, 29) n,
		     LATERAL (SELECT $1::timestamptz - n * interval '15 minutes' + (c * 2 + s) * interval '1 second'
		                     + (i / 5) * interval '1 millisecond' AS at) ts
		WHERE q = 1 OR i <= 40`, T); err != nil {
		t.Fatal(err)
	}
	// index-only 要靠可见性图,刚灌完的表不 VACUUM 会一直回表
	if _, err := st.pool.Exec(ctx, `VACUUM ANALYZE market_order_live`); err != nil {
		t.Fatal(err)
	}

	var keys []model.QuoteKey
	var items, locs []string
	var sides []int16
	for i := 1; i <= 125; i++ {
		for c := 1; c <= 7; c++ {
			for s := int16(0); s <= 1; s++ {
				item, loc := "ITEM_"+itoa(i), "CITY_"+itoa(c)
				keys = append(keys, model.QuoteKey{ItemID: item, LocationID: loc, Quality: 1, Side: model.Side(s)})
				items, locs, sides = append(items, item), append(locs, loc), append(sides, s)
			}
		}
	}
	since := T.Add(-6 * time.Hour)
	rows, err := st.pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+bookOrdersSQL,
		items, locs, sides, since, []int16{1})
	if err != nil {
		t.Fatal(err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, line)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(plan, "\n")
	t.Log("\n" + text)
	if !strings.Contains(text, "Index Only Scan using idx_live_book") {
		t.Error("没走 idx_live_book 的 Index Only Scan")
	}
	if strings.Contains(text, "Rows Removed by Join Filter") {
		t.Error("计划里出现了连接过滤:数整页那一步被接成了嵌套循环 JOIN(见上面的反例)")
	}

	for round := 0; round < 3; round++ {
		start := time.Now()
		orders, err := st.BookOrders(ctx, keys, since)
		if err != nil {
			t.Fatal(err)
		}
		fetched := time.Since(start)
		got := buildAll(orders, 120*time.Second)
		t.Logf("第 %d 次:BookOrders 往返 %v(%d 行),Group+Build %v,合计 %v,%d 个 key",
			round+1, fetched, len(orders), time.Since(start)-fetched, time.Since(start), len(got))
		if len(got) != len(keys) {
			t.Errorf("%d 个 key 都有数据,得到 %d", len(keys), len(got))
		}
	}

	// WS 报价每次标脏只读那几个盘口边:一个装备 × 7 城 × 2 边,窗口 30 分钟(-fresh)
	few := keys[:14]
	for round := 0; round < 3; round++ {
		start := time.Now()
		orders, err := st.BookOrders(ctx, few, T.Add(-30*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		got := buildAll(orders, 120*time.Second)
		t.Logf("标脏 %d 个 key 第 %d 次:BookOrders + Build 合计 %v(%d 行)", len(few), round+1, time.Since(start), len(orders))
		if len(got) != len(few) {
			t.Errorf("%d 个 key 都有数据,得到 %d", len(few), len(got))
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
