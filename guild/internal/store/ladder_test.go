package store

import (
	"context"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"albion-guild/internal/depth"
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

func levelsOf(b BookSide) []lvl {
	out := make([]lvl, len(b.Levels))
	for i, l := range b.Levels {
		out[i] = lvl{l.Price, l.Depth}
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

func TestBookSides(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	since := T.Add(-6 * time.Hour)
	const slack = 120 * time.Second
	sd := &seeder{t: t, st: st}

	// T6_METALBAR_LEVEL4@4 @ Martlock 的真实买方阶梯:顶上 7 档合计 11 件,
	// 然后断崖到 53,001,底下 2,000 件 1 银占位单。同一眼看到
	t6 := []fx{
		{260066, 1, T}, {260065, 2, T}, {260064, 1, T}, {260060, 2, T}, {260050, 2, T},
		{260040, 1, T}, {260000, 2, T}, {53001, 15, T}, {1, 2000, T},
	}
	kT6 := model.QuoteKey{ItemID: "T6_METALBAR_LEVEL4@4", LocationID: "Martlock", Quality: 1, Side: model.SideRequest}
	sd.put(kT6, t6...)

	// 同一阶梯再加一张 20 分钟前的 270,000:比最近一眼的最高买价还高,
	// 真还在的话这一眼一定看得到 → 已成交或已撤单
	kT6Ghost := kT6
	kT6Ghost.Quality = 2
	sd.put(kT6Ghost, append(append([]fx(nil), t6...), fx{270000, 3, T.Add(-20 * time.Minute)})...)

	// 卖方:最近一眼(T−2m 以内)是 104@T−1m 和 105@T,覆盖到 105
	kAsk := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 1, Side: model.SideOffer}
	sd.put(kAsk,
		fx{100, 5, T.Add(-10 * time.Minute)}, // 比这一眼的卖一还便宜却没被看到 → 幽灵
		fx{104, 4, T.Add(-time.Minute)},
		fx{105, 7, T},
		fx{105, 6, T.Add(-10 * time.Minute)}, // 正好在这一眼的边上,可能是同价没翻到 → 保留
		fx{110, 3, T.Add(-10 * time.Minute)}, // 比这一眼最差价还差,可能没翻到那页 → 保留
		fx{90, 9, T.Add(-7 * time.Hour)},     // 窗口外
		fx{120, 0, T.Add(30 * time.Second)},  // 0 件:不能拿它当"最近一眼"
	)

	// 买方镜像(和 kAsk 同物品同城同品质,只差方向,顺带验证两边不串)
	kBid := kAsk
	kBid.Side = model.SideRequest
	sd.put(kBid,
		fx{1000, 2, T}, fx{990, 3, T}, // 最近一眼覆盖 1000 → 990
		fx{1010, 1, T.Add(-20 * time.Minute)}, // 比买一还高 → 幽灵
		fx{995, 4, T.Add(-20 * time.Minute)},  // 在这一眼覆盖的段里没出现 → 幽灵
		fx{990, 1, T.Add(-20 * time.Minute)},  // 边上 → 保留
		fx{980, 5, T.Add(-20 * time.Minute)},  // 更差 → 保留
	)

	// 没请求的盘口有数据,不能混进结果
	sd.put(model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Lymhurst", Quality: 3, Side: model.SideOffer},
		fx{50, 99, T})

	kEmpty := model.QuoteKey{ItemID: "T5_CLOTH", LocationID: "Thetford", Quality: 1, Side: model.SideOffer}
	keys := []model.QuoteKey{kT6, kT6Ghost, kAsk, kBid, kEmpty, kAsk} // kAsk 重复一次,件数不能翻倍

	got, err := st.BookSides(ctx, keys, since, slack, 128)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("T6买方阶梯原样读出并能算近价", func(t *testing.T) {
		b, ok := got[kT6]
		if !ok {
			t.Fatal("缺 kT6")
		}
		if len(b.Levels) != 9 || b.LevelCount != 9 || b.QtyTotal != 2026 || b.Ghosts != 0 {
			t.Fatalf("应为 9 档 / 2026 件 / 0 幽灵,得到 %d 档(共 %d)/ %d 件 / %d 幽灵",
				len(b.Levels), b.LevelCount, b.QtyTotal, b.Ghosts)
		}
		if b.Levels[0].Price != 260066 || b.Levels[8].Price != 1 {
			t.Fatalf("买方应从高到低,得到 %v", levelsOf(b))
		}
		if !b.Newest.Equal(T) || !b.Levels[0].Seen.Equal(T) {
			t.Fatalf("Newest/Seen 应为 T,得到 %v / %v", b.Newest, b.Levels[0].Seen)
		}
		dl := make([]depth.Level, len(b.Levels))
		for i, l := range b.Levels {
			dl[i] = depth.Level{Price: l.Price, Qty: l.Depth}
		}
		sup := depth.Analyze(dl, 0.05)
		if sup.QtyNear != 11 || sup.LevelsNear != 7 || math.Abs(sup.GapAfterNear-0.7962) > 1e-4 {
			t.Fatalf("近价应为 11 件 / 7 档 / 断崖 79.62%%,得到 %+v", sup)
		}
	})

	t.Run("买方比最高买价还高的旧单被剔除", func(t *testing.T) {
		b := got[kT6Ghost]
		if b.Ghosts != 1 || b.LevelCount != 9 || b.QtyTotal != 2026 || b.Levels[0].Price != 260066 {
			t.Fatalf("270,000 应被剔除,得到 ghosts=%d levels=%v", b.Ghosts, levelsOf(b))
		}
	})

	t.Run("卖方幽灵单剔除", func(t *testing.T) {
		b := got[kAsk]
		want := []lvl{{104, 4}, {105, 13}, {110, 3}}
		if !sameLevels(levelsOf(b), want) {
			t.Fatalf("应为 %v,得到 %v", want, levelsOf(b))
		}
		if b.Ghosts != 1 || b.QtyTotal != 20 || b.LevelCount != 3 {
			t.Fatalf("应 1 幽灵 / 20 件 / 3 档,得到 %d / %d / %d", b.Ghosts, b.QtyTotal, b.LevelCount)
		}
		if b.Levels[1].Orders != 2 || !b.Levels[1].Seen.Equal(T) {
			t.Fatalf("105 那一档应是 2 张单、Seen=T,得到 %+v", b.Levels[1])
		}
		if !b.Newest.Equal(T) {
			t.Fatalf("0 件的单不该算进最近一眼,Newest 应为 T,得到 %v", b.Newest)
		}
	})

	t.Run("买方镜像:段内剔除、边上和更差的保留", func(t *testing.T) {
		b := got[kBid]
		want := []lvl{{1000, 2}, {990, 4}, {980, 5}}
		if !sameLevels(levelsOf(b), want) || b.Ghosts != 2 {
			t.Fatalf("应为 %v / 2 幽灵,得到 %v / %d", want, levelsOf(b), b.Ghosts)
		}
	})

	t.Run("没数据的key不在结果里", func(t *testing.T) {
		if _, ok := got[kEmpty]; ok {
			t.Fatal("kEmpty 没有数据,不该出现")
		}
		if len(got) != 4 {
			t.Fatalf("应只返回 4 个有数据的请求 key,得到 %d", len(got))
		}
	})

	t.Run("截断只砍档不砍合计", func(t *testing.T) {
		cut, err := st.BookSides(ctx, []model.QuoteKey{kAsk}, since, slack, 2)
		if err != nil {
			t.Fatal(err)
		}
		b := cut[kAsk]
		if !sameLevels(levelsOf(b), []lvl{{104, 4}, {105, 13}}) || b.LevelCount != 3 || b.QtyTotal != 20 {
			t.Fatalf("应返回 2 档、LevelCount=3、QtyTotal=20,得到 %v / %d / %d",
				levelsOf(b), b.LevelCount, b.QtyTotal)
		}
	})

	// 幽灵规则的前提(一次观测覆盖从最优价开始的连续一段)还没实机验证。
	// 不成立时要能一键退回"按窗口过滤":slack 设成不小于窗口
	t.Run("slack不小于窗口时不剔除任何单", func(t *testing.T) {
		all, err := st.BookSides(ctx, []model.QuoteKey{kAsk, kBid}, since, 6*time.Hour, 128)
		if err != nil {
			t.Fatal(err)
		}
		if a := all[kAsk]; a.Ghosts != 0 || !sameLevels(levelsOf(a), []lvl{{100, 5}, {104, 4}, {105, 13}, {110, 3}}) {
			t.Fatalf("卖方应全保留(90 仍在窗口外),得到 %v / %d", levelsOf(a), a.Ghosts)
		}
		if b := all[kBid]; b.Ghosts != 0 || b.LevelCount != 5 {
			t.Fatalf("买方应全保留,得到 %v / %d", levelsOf(b), b.Ghosts)
		}
	})

	t.Run("空key列表", func(t *testing.T) {
		m, err := st.BookSides(ctx, nil, since, slack, 128)
		if err != nil || len(m) != 0 {
			t.Fatalf("应返回空 map,得到 %v / %v", m, err)
		}
	})
}

// TestBookSides_查询计划 是手工检查,只在 FLIPPER_TEST_EXPLAIN=1 时跑:
// 对 792 个 key(默认物品清单 66 个 × 6 城 × 1 品质 × 2 边)跑
// EXPLAIN (ANALYZE, BUFFERS),确认走 idx_live_book 的 Index Only Scan。
//
// 2026-09-23 在 flipper-pg(timescaledb 2.30.1 / pg16,WSL docker)上的结果,
// 临时库 11.88 万行(请求的 792 key 各 30 张单,其中 24 张在 6h 窗口内;
// 另有品质 2~5 的 9.5 万行干扰数据),VACUUM ANALYZE 之后:
//   - unnest(792 行)→ Nested Loop → Index Only Scan using idx_live_book,
//     loops=792、每次约 0.01ms、Heap Fetches: 0、shared hit=2494,取数合计约 13ms;
//     last_seen/amount 走 INCLUDE 列上的 Filter,不回表
//   - 之后是一串 WindowAgg / Incremental Sort / GroupAggregate,线性,没有 JOIN
//   - Execution Time 151.7ms;Go 侧 BookSides 往返 215ms(含 19,008 行经 WSL
//     端口转发传回 Windows)
//
// 计划里原版 SQL 用"g 单独 GROUP BY 再 JOIN 回 r"求幽灵张数:unnest 的行数估计
// 恒为 1,规划器把那个 JOIN 做成嵌套循环,每一档重扫一遍全部 key 的聚合,
// Rows Removed by Join Filter 751 万,Execution Time 1450ms。改成窗口聚合后
// 降到上面的 151.7ms。live 表没有清理,这个数会随时间涨,上线后要定期复查。
func TestBookSides_查询计划(t *testing.T) {
	if os.Getenv("FLIPPER_TEST_EXPLAIN") != "1" {
		t.Skip("手工检查:设 FLIPPER_TEST_EXPLAIN=1 才跑")
	}
	st := openTestStore(t)
	ctx := context.Background()
	T := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

	// 直接用 SQL 灌,WriteOrders 一条条走太慢。卖单从 1000 往上、买单往下,
	// 每 15 分钟一张,n≥24 的落在 6h 窗口外
	if _, err := st.pool.Exec(ctx, `
		INSERT INTO market_order_live (order_id, item_id, location_id, quality, enchant, side,
		       unit_price, amount, expires_at, first_seen, last_seen, reporter, raw_location)
		SELECT row_number() OVER (), 'ITEM_' || i, 'CITY_' || c, q, 0, s,
		       1000 + (CASE WHEN s = 0 THEN n ELSE -n END) * 3, 1 + n % 7, NULL,
		       $1::timestamptz - n * interval '15 minutes', $1::timestamptz - n * interval '15 minutes', '', ''
		FROM generate_series(1, 66) i, generate_series(1, 6) c, generate_series(1, 5) q,
		     generate_series(0, 1) s, generate_series(0, 29) n`, T); err != nil {
		t.Fatal(err)
	}
	// index-only 要靠可见性图,刚灌完的表不 VACUUM 会一直回表
	if _, err := st.pool.Exec(ctx, `VACUUM ANALYZE market_order_live`); err != nil {
		t.Fatal(err)
	}

	var items, locs []string
	var quals, sides []int16
	var keys []model.QuoteKey
	for i := 1; i <= 66; i++ {
		for c := 1; c <= 6; c++ {
			for s := int16(0); s <= 1; s++ {
				item, loc := "ITEM_"+itoa(i), "CITY_"+itoa(c)
				items, locs = append(items, item), append(locs, loc)
				quals, sides = append(quals, 1), append(sides, s)
				keys = append(keys, model.QuoteKey{ItemID: item, LocationID: loc, Quality: 1, Side: model.Side(s)})
			}
		}
	}
	since := T.Add(-6 * time.Hour)
	rows, err := st.pool.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+bookSidesSQL,
		items, locs, quals, sides, since, (120 * time.Second).Seconds(), int64(128))
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

	start := time.Now()
	got, err := st.BookSides(ctx, keys, since, 120*time.Second, 128)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("BookSides 往返 %v,返回 %d 个 key", time.Since(start), len(got))
	if len(got) != 792 {
		t.Errorf("792 个 key 都有数据,得到 %d", len(got))
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
