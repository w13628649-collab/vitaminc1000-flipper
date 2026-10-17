package flip

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/book"
	"albion-guild/internal/conf"
	"albion-guild/internal/depth"
	"albion-guild/internal/model"
	"albion-guild/internal/rank"
	"albion-guild/internal/refine"
	"albion-guild/internal/store"
)

// ── 假数据源 ───────────────────────────────────────────────────────────

// fakeRefine 是精炼页的假数据源:记下每次调用的参数,按事先放好的数据回。
// BookOrders 和 store.BookOrders 的约定一致:按 物品 × 城市 × 方向 过滤(品质全回,
// 调用方按请求的 key 丢掉多余的),since 之前的不回,Page 按整次响应数好。
type fakeRefine struct {
	mu     sync.Mutex
	orders []store.LiveOrder
	prices []aodp.PriceRecord
	days   []store.ItemDay

	bookErr, priceErr, volErr error
	// gate 非 nil 时 BookOrders 等它关上才返回(测并发合并用)
	gate chan struct{}

	bookCalls, priceCalls, volCalls int
	keys                            []model.QuoteKey
	since                           time.Time
	priceItems, priceCities         []string
	volItems                        []string
	volCity                         string
	volFirst, volLast               time.Time
}

func (f *fakeRefine) BookOrders(_ context.Context, keys []model.QuoteKey, since time.Time) ([]store.LiveOrder, error) {
	f.mu.Lock()
	f.bookCalls++
	f.keys, f.since = keys, since
	gate := f.gate
	f.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if f.bookErr != nil {
		return nil, f.bookErr
	}
	type scope struct {
		item, city string
		side       model.Side
	}
	want := map[scope]bool{}
	for _, k := range keys {
		want[scope{k.ItemID, k.LocationID, k.Side}] = true
	}
	var in []store.LiveOrder
	for _, o := range f.orders {
		if want[scope{o.ItemID, o.City, o.Side}] && o.LastSeen.After(since) {
			in = append(in, o)
		}
	}
	return book.WithPages(in), nil
}

func (f *fakeRefine) AODPPrices(_ context.Context, items, cities []string) ([]aodp.PriceRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.priceCalls++
	f.priceItems, f.priceCities = items, cities
	if f.priceErr != nil {
		return nil, f.priceErr
	}
	return f.prices, nil
}

func (f *fakeRefine) DailyVolume(_ context.Context, items []string, city string, quality int,
	first, last time.Time) ([]store.ItemDay, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volCalls++
	f.volItems, f.volCity, f.volFirst, f.volLast = items, city, first, last
	var out []store.ItemDay
	for _, d := range f.days {
		if slices.Contains(items, d.ItemID) && d.City == city && !d.Day.Before(first) && !d.Day.After(last) {
			out = append(out, d)
		}
	}
	return out, f.volErr
}

func (f *fakeRefine) calls() (books, prices, vols int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bookCalls, f.priceCalls, f.volCalls
}

// rNow 是这些测试的「现在」。
var rNow = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

// newRefineService 建一个用假数据源、钟可拨的 Service。
func newRefineService(src *fakeRefine) (*Service, *atomic.Int64) {
	s := &Service{Cfg: conf.Default(), RefineSrc: src}
	clk := &atomic.Int64{}
	clk.Store(rNow.UnixNano())
	s.refine.clock = func() time.Time { return time.Unix(0, clk.Load()).UTC() }
	return s, clk
}

// rOrders 是一个盘口边上的一轮挂单:每档一张单,都在 seen 那一刻被看到。
func rOrders(item, city string, side model.Side, seen time.Time, pairs ...int64) []store.LiveOrder {
	var out []store.LiveOrder
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, store.LiveOrder{ItemID: item, City: city, Quality: 1, Side: side,
			Price: pairs[i], Amount: pairs[i+1], FirstSeen: seen, LastSeen: seen})
	}
	return out
}

const (
	tBar  = "T4_METALBAR_LEVEL1@1"
	tOre  = "T4_ORE_LEVEL1@1"
	tOre0 = "T4_ORE"
	tT3   = "T3_METALBAR"
)

// barFixture 是 T4.1 钢条 @ Thetford 的一套盘口:1 小时前翻过的抓包,T3 条没抓到、
// AODP 有价,心哪边都没有。
func barFixture() *fakeRefine {
	seen := rNow.Add(-time.Hour)
	var orders []store.LiveOrder
	orders = append(orders, rOrders(tBar, "Thetford", model.SideOffer, seen, 1000, 20, 1010, 30)...)
	orders = append(orders, rOrders(tBar, "Thetford", model.SideRequest, seen, 900, 5, 890, 10)...)
	orders = append(orders, rOrders(tOre, "Thetford", model.SideOffer, seen, 200, 100, 210, 200)...)
	orders = append(orders, rOrders(tOre, "Thetford", model.SideRequest, seen, 150, 50)...)
	orders = append(orders, rOrders(tOre0, "Thetford", model.SideOffer, seen, 50, 1000)...)
	return &fakeRefine{
		orders: orders,
		prices: []aodp.PriceRecord{{ItemID: tT3, City: "Thetford", Quality: 1,
			SellPriceMin: 60, SellPriceMinDate: aodp.Stamp{T: rNow.Add(-2 * time.Hour)}}},
	}
}

func mustRefine(t *testing.T, s *Service, p refine.Params) *RefinePlan {
	t.Helper()
	res, err := s.Refine(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func hasWarn(ws []refine.Warning, code string) bool {
	for _, w := range ws {
		if w.Code == code {
			return true
		}
	}
	return false
}

// ── 测试 ───────────────────────────────────────────────────────────────

func TestRefine按配方图收集盘口并按精炼窗口读簿(t *testing.T) {
	src := &fakeRefine{}
	s, _ := newRefineService(src)
	target := "T8_METALBAR_LEVEL4@4"
	res := mustRefine(t, s, s.RefineDefaults(target, ""))

	// 读单窗口是 refining.window_hours(72h),不是扫描的 6h
	if want := rNow.Add(-72 * time.Hour); !src.since.Equal(want) {
		t.Fatalf("读簿 since = %v,想要 %v", src.since, want)
	}
	// 读的是 (线, 城市) 快照:这条线全部物品在 Thetford 的两边,一次读出来,目标要的都在里面
	items, _ := refine.ItemsFor(s.Recipes(), target)
	all := s.snapItems(refine.Lines[0])
	got := map[model.QuoteKey]bool{}
	for _, k := range src.keys {
		if k.Quality != 1 || k.LocationID != "Thetford" {
			t.Fatalf("只该读 Thetford 品质 1: %+v", k)
		}
		got[k] = true
	}
	for _, it := range all {
		for _, side := range []model.Side{model.SideOffer, model.SideRequest} {
			if !got[model.QuoteKey{ItemID: it, LocationID: "Thetford", Quality: 1, Side: side}] {
				t.Fatalf("没读 %s 的 %d 边", it, side)
			}
		}
	}
	for _, it := range items {
		if !slices.Contains(all, it) {
			t.Fatalf("快照没覆盖目标要的 %s", it)
		}
	}
	// books 只数这个目标要的边
	if len(src.keys) != 2*len(all) || res.Books.Sides != 2*len(items) || res.Books.Empty != res.Books.Sides {
		t.Fatalf("盘口边 %d(快照物品 %d、目标要 %d),books %+v", len(src.keys), len(all), len(items), res.Books)
	}
	// AODP 按整条线要一次价,成交量要这条线 25 个目标在这座城的
	if !slices.Equal(src.priceItems, all) || !slices.Equal(src.priceCities, []string{"Thetford"}) {
		t.Fatalf("AODP 请求 %v @ %v", src.priceItems, src.priceCities)
	}
	first, last := rank.Window(rNow, 14)
	if !slices.Equal(src.volItems, refine.Lines[0].Targets()) || src.volCity != "Thetford" ||
		!src.volFirst.Equal(first) || !src.volLast.Equal(last) {
		t.Fatalf("成交量请求 %v @ %s [%v, %v]", src.volItems, src.volCity, src.volFirst, src.volLast)
	}
	// 什么都没有也回结果:缺价是警告,不是错误
	if !hasWarn(res.Warnings, refine.WarnNoSellPrice) || !hasWarn(res.Warnings, refine.WarnNoVolume) {
		t.Fatalf("应有缺卖价、没成交量的警告: %+v", res.Warnings)
	}

	// 出售城市另选:Martlock 建一份快照,Thetford 那份照用、不重读
	p := s.RefineDefaults(target, "")
	p.Sell.City = "Martlock"
	res = mustRefine(t, s, p)
	for _, k := range src.keys {
		if k.LocationID != "Martlock" {
			t.Fatalf("Thetford 的快照还新,不该重读: %+v", k)
		}
	}
	if b, pr, v := src.calls(); b != 2 || pr != 2 || v != 2 || !slices.Equal(src.priceCities, []string{"Martlock"}) {
		t.Fatalf("只为 Martlock 建一份:读簿 %d、AODP %d @ %v、成交量 %d", b, pr, src.priceCities, v)
	}
	// 目标在 Martlock 只用两边:原料还是在生产城市买
	if res.Books.Sides != 2*len(items)+2 {
		t.Fatalf("出售城市 Martlock 应只多用目标两边: %d", res.Books.Sides)
	}
	// 允许外城买料:原料也用 Martlock 的,两份快照都还新,一次都不读
	p = s.RefineDefaults(target, "")
	p.Sources.Cities = []string{"Thetford", "Martlock"}
	res = mustRefine(t, s, p)
	if b, _, _ := src.calls(); b != 2 || res.Books.Sides != 4*len(items) {
		t.Fatalf("买料城市有 Martlock 要用 Martlock 的原料:读簿 %d、边 %d", b, res.Books.Sides)
	}
}

func TestRefine抓包阶梯剔幽灵单_超过扫描窗口照用并标年龄(t *testing.T) {
	src := barFixture()
	old := rNow.Add(-30 * time.Hour)
	// 目标的卖单:31 小时前那一轮有张 990 的,30 小时前这一轮从 1000 起 —— 990 是幽灵单
	src.orders = slices.DeleteFunc(src.orders, func(o store.LiveOrder) bool {
		return o.ItemID == tBar && o.Side == model.SideOffer
	})
	src.orders = append(src.orders, rOrders(tBar, "Thetford", model.SideOffer, old.Add(-time.Hour), 990, 3)...)
	src.orders = append(src.orders, rOrders(tBar, "Thetford", model.SideOffer, old, 1000, 20, 1010, 30)...)
	// 窗口外(80 小时前)的单读不到
	src.orders = append(src.orders, rOrders("T3_ORE", "Thetford", model.SideOffer, rNow.Add(-80*time.Hour), 10, 10)...)
	s, _ := newRefineService(src)
	res := mustRefine(t, s, s.RefineDefaults(tBar, ""))

	post := res.Sell.Post
	if post == nil || post.BestAsk != 1000 || post.Source != "capture" {
		t.Fatalf("卖一应是剔掉幽灵单之后的 1000: %+v", post)
	}
	if post.AgeH == nil || math.Abs(*post.AgeH-30) > 1e-6 {
		t.Fatalf("30 小时前的盘口照用,年龄 30h: %+v", post.AgeH)
	}
	c := res.Candidates[0]
	if c.AgeLevel != "red" || !hasWarn(c.Warnings, refine.WarnStale) {
		t.Fatalf("用到 30 小时前的价要标红: %s %+v", c.AgeLevel, c.Warnings)
	}
	for _, st := range c.Steps {
		if st.Item == "T3_ORE" {
			t.Fatal("80 小时前的单在窗口外,不该出现")
		}
	}
	if res.Books.Capture != 5 || res.Books.WindowHours != 72 {
		t.Fatalf("books: %+v", res.Books)
	}
}

func TestRefine抓包没有的用AODP兜底标深度未知(t *testing.T) {
	src := barFixture()
	// 没时间戳的 AODP 价查不了新鲜度,不用
	src.prices = append(src.prices, aodp.PriceRecord{ItemID: tOre0, City: "Thetford", Quality: 1, BuyPriceMax: 40})
	s, _ := newRefineService(src)
	res := mustRefine(t, s, s.RefineDefaults(tBar, ""))

	if !slices.Contains(res.Data.DepthUnknown, tT3+"|Thetford") {
		t.Fatalf("T3 条只有 AODP 价,应列进 depth_unknown: %+v", res.Data)
	}
	var t3 *refine.Step
	for i, st := range res.Candidates[0].Steps {
		if st.Op == "buy" && st.Item == tT3 {
			t3 = &res.Candidates[0].Steps[i]
		}
	}
	if t3 == nil || t3.Source != "aodp" || t3.PriceFrom != 60 || t3.AgeH == nil || math.Abs(*t3.AgeH-2) > 1e-6 {
		t.Fatalf("T3 条应按 AODP 的 60 银买、年龄 2h: %+v", t3)
	}
	if !hasWarn(res.Candidates[0].Warnings, refine.WarnDepthUnknown) {
		t.Fatalf("候选应带 depth_unknown 警告: %+v", res.Candidates[0].Warnings)
	}
	if res.Books.AODP != 1 || !res.Books.AODPOK {
		t.Fatalf("只有 T3 条卖单一边用了 AODP: %+v", res.Books)
	}
}

// 抓包有阶梯、AODP 之后报了不同的价:MergeSide 会选 AODP,精炼页仍用阶梯(要吃深度),
// 但列进 aodp_newer 并加警告
func TestRefineAODP更新且不同价时仍用阶梯但告警(t *testing.T) {
	src := barFixture()
	src.prices = append(src.prices, aodp.PriceRecord{ItemID: tBar, City: "Thetford", Quality: 1,
		SellPriceMin: 950, SellPriceMinDate: aodp.Stamp{T: rNow.Add(-10 * time.Minute)}})
	s, _ := newRefineService(src)
	res := mustRefine(t, s, s.RefineDefaults(tBar, ""))

	if res.Sell.Post.BestAsk != 1000 || res.Sell.Post.Source != "capture" {
		t.Fatalf("仍用抓包阶梯: %+v", res.Sell.Post)
	}
	want := []AODPNewer{{Item: tBar, City: "Thetford", Side: "sell", CapturePrice: 1000, CaptureAgeH: 1,
		AODPPrice: 950, AODPAgeH: round3(10.0 / 60)}}
	if len(res.Books.AODPNewer) != 1 || res.Books.AODPNewer[0] != want[0] {
		t.Fatalf("aodp_newer: %+v", res.Books.AODPNewer)
	}
	if !hasWarn(res.Warnings, WarnAODPNewer) {
		t.Fatalf("应有 aodp_newer 警告: %+v", res.Warnings)
	}
	// 同价就不算:两个来源说的是同一个价
	src2 := barFixture()
	src2.prices = append(src2.prices, aodp.PriceRecord{ItemID: tBar, City: "Thetford", Quality: 1,
		SellPriceMin: 1000, SellPriceMinDate: aodp.Stamp{T: rNow.Add(-10 * time.Minute)}})
	s2, _ := newRefineService(src2)
	if res2 := mustRefine(t, s2, s2.RefineDefaults(tBar, "")); len(res2.Books.AODPNewer) != 0 {
		t.Fatalf("同价不该告警: %+v", res2.Books.AODPNewer)
	}
}

func TestRefine成交量按自然日平均(t *testing.T) {
	src := barFixture()
	first, _ := rank.Window(rNow, 14)
	counts := []int64{10, 0, 5, 20, 8, 7, 9, 5, 1} // 9 行、8 天有成交,合计 65
	for i, n := range counts {
		src.days = append(src.days, store.ItemDay{ItemID: tBar, City: "Thetford",
			Day: first.AddDate(0, 0, i), ItemCount: n, Source: store.SourceAODP})
	}
	// 窗口外、别的城的不算
	src.days = append(src.days,
		store.ItemDay{ItemID: tBar, City: "Thetford", Day: rNow.Truncate(24 * time.Hour), ItemCount: 999, Source: store.SourceAODP},
		store.ItemDay{ItemID: tBar, City: "Martlock", Day: first, ItemCount: 999, Source: store.SourceAODP})
	s, _ := newRefineService(src)
	res := mustRefine(t, s, s.RefineDefaults(tBar, ""))
	v := res.Volume
	if !v.Known || v.Days != 14 || v.DaysWithData != 8 || math.Abs(v.PerDay-65.0/14) > 1e-9 || v.Source != "aodp" {
		t.Fatalf("65 件 ÷ 14 个自然日,8 天有成交: %+v", v)
	}
	if hasWarn(res.Warnings, refine.WarnNoVolume) {
		t.Fatal("有成交数据不该报 no_volume")
	}

	// 一行都没有是「无数据」,不是 0
	s2, _ := newRefineService(barFixture())
	res2 := mustRefine(t, s2, s2.RefineDefaults(tBar, ""))
	if res2.Volume.Known || !hasWarn(res2.Warnings, refine.WarnNoVolume) {
		t.Fatalf("没有成交数据应 Known=false 并告警: %+v", res2.Volume)
	}
}

func TestRefine同样参数一分钟内只算一次且合并并发(t *testing.T) {
	src := barFixture()
	src.gate = make(chan struct{})
	s, clk := newRefineService(src)
	p := s.RefineDefaults(tBar, "")

	var wg sync.WaitGroup
	out := make([]*RefinePlan, 2)
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := s.Refine(context.Background(), p)
			if err != nil {
				t.Error(err)
			}
			out[i] = res
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if b, _, _ := src.calls(); b >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("等不到读簿")
		}
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // 给第二个请求时间撞上在飞的那次
	src.mu.Lock()
	close(src.gate)
	src.gate = nil
	src.mu.Unlock()
	wg.Wait()
	if b, pr, v := src.calls(); b != 1 || pr != 1 || v != 1 {
		t.Fatalf("两个并发的同样请求应只算一次:读簿 %d、AODP %d、成交量 %d", b, pr, v)
	}
	if out[0] == nil || out[0] != out[1] {
		t.Fatal("合并的两个请求应拿到同一个结果")
	}

	// 再来:同一份快照、同样参数,直接回缓存
	clk.Add(int64(3 * time.Second))
	if again := mustRefine(t, s, p); again != out[0] {
		t.Fatal("一分钟内同样参数、数据没变,应回同一个结果")
	}
	// 参数不同(哪怕只差一个件数)就是另一次计算,但数据还是那份快照:只重算引擎,不再读库
	q := p
	q.Qty = 11
	if res := mustRefine(t, s, q); res == out[0] || res.Qty != 11 {
		t.Fatal("不同参数应重算")
	}
	if b, pr, v := src.calls(); b != 1 || pr != 1 || v != 1 {
		t.Fatalf("参数变了只重算引擎:读簿 %d、AODP %d、成交量 %d", b, pr, v)
	}
	// 快照到 60 秒重读盘口;AODP 120 秒、成交量 10 分钟,没到期沿用
	clk.Store(rNow.Add(61 * time.Second).UnixNano())
	res := mustRefine(t, s, p)
	if b, pr, v := src.calls(); b != 2 || pr != 1 || v != 1 {
		t.Fatalf("60 秒后重读盘口、AODP 和成交量沿用:读簿 %d、AODP %d、成交量 %d", b, pr, v)
	}
	if !res.ParamsUsed.Now.Equal(rNow.Add(61*time.Second)) || res == out[0] || res.Books.Timing.Cache != "miss" {
		t.Fatalf("快照换了要重算,now 是新的: %v %s", res.ParamsUsed.Now, res.Books.Timing.Cache)
	}
	clk.Store(rNow.Add(125 * time.Second).UnixNano())
	mustRefine(t, s, p)
	if b, pr, v := src.calls(); b != 3 || pr != 2 || v != 1 {
		t.Fatalf("120 秒后 AODP 也重取:读簿 %d、AODP %d、成交量 %d", b, pr, v)
	}
}

// 快照(诉求 #1/#8):读簿、AODP、成交量只取决于 (线, 城市),和个人参数无关。
// 换专精、站费、件数只重算引擎;换目标(同一条线、同一座城)也不再读库。
func TestRefine快照命中时不再读库_参数变了只重算引擎(t *testing.T) {
	src := barFixture()
	s, clk := newRefineService(src)
	first := mustRefine(t, s, s.RefineDefaults(tBar, ""))
	if first.Books.Timing.Cache != "miss" || first.Books.Timing.EngineMS <= 0 {
		t.Fatalf("第一次要现建快照: %+v", first.Books.Timing)
	}
	clk.Add(int64(10 * time.Second))
	p := s.RefineDefaults(tBar, "")
	p.Focus.FCE = map[int]float64{4: 10000}
	p.Fees.StationPer100 = 300
	res := mustRefine(t, s, p)
	other := mustRefine(t, s, s.RefineDefaults("T4_METALBAR_LEVEL2@2", ""))
	if b, pr, v := src.calls(); b != 1 || pr != 1 || v != 1 {
		t.Fatalf("快照命中不该再读:读簿 %d、AODP %d、成交量 %d", b, pr, v)
	}
	tm := res.Books.Timing
	if tm.Cache != "hit" || tm.WaitMS != 0 || tm.SnapshotAgeS != 10 || other.Books.Timing.Cache != "hit" {
		t.Fatalf("应命中快照、快照 10 秒前读的: %+v", tm)
	}
	if !res.Books.ReadAt.Equal(rNow) || res.ParamsUsed.Fees.StationPer100 != 300 {
		t.Fatalf("read_at 是快照的读簿时刻: %v", res.Books.ReadAt)
	}
}

// 入库标脏(ingest 的 DirtyMarker):这条线这座城的快照下次用时重建,读到新挂单;
// 别的线、别的城、品质不是 1 的不受影响。距上次读簿不到 2 秒的不重建(抗翻页风暴)。
func TestRefine入库标脏后重建快照(t *testing.T) {
	src := barFixture()
	s, clk := newRefineService(src)
	res := mustRefine(t, s, s.RefineDefaults(tBar, ""))
	if res.Sell.Post.BestAsk != 1000 {
		t.Fatalf("卖一 %d", res.Sell.Post.BestAsk)
	}
	src.mu.Lock()
	src.orders = append(src.orders, rOrders(tBar, "Thetford", model.SideOffer, rNow.Add(time.Second), 980, 4)...)
	src.mu.Unlock()
	s.MarkRefineDirty(model.QuoteKey{ItemID: tBar, LocationID: "Thetford", Quality: 2, Side: model.SideOffer})
	s.MarkRefineDirty(model.QuoteKey{ItemID: "T4_LEATHER", LocationID: "Thetford", Quality: 1, Side: model.SideOffer})
	s.MarkRefineDirty(model.QuoteKey{ItemID: tOre, LocationID: "Martlock", Quality: 1, Side: model.SideOffer})
	clk.Add(int64(time.Second))
	mustRefine(t, s, s.RefineDefaults(tBar, ""))
	if b, _, _ := src.calls(); b != 1 {
		t.Fatalf("标脏的不是这份快照,不该重读:%d", b)
	}
	// 快照才读了 1.5 秒:标脏也先不重建
	s.MarkRefineDirty(model.QuoteKey{ItemID: tOre, LocationID: "Thetford", Quality: 1, Side: model.SideOffer})
	clk.Add(int64(500 * time.Millisecond))
	mustRefine(t, s, s.RefineDefaults(tBar, ""))
	if b, _, _ := src.calls(); b != 1 {
		t.Fatalf("2 秒内不重建:%d", b)
	}
	clk.Add(int64(time.Second))
	res = mustRefine(t, s, s.RefineDefaults(tBar, ""))
	if b, pr, _ := src.calls(); b != 2 || pr != 1 {
		t.Fatalf("标脏后重读盘口、AODP 沿用:读簿 %d、AODP %d", b, pr)
	}
	if res.Sell.Post.BestAsk != 980 || res.Books.Timing.Cache != "miss" {
		t.Fatalf("重建后应读到新挂单 980: %d %s", res.Sell.Post.BestAsk, res.Books.Timing.Cache)
	}
}

// 观测早于快照、入库晚于快照:客户端每 3 秒上传、ingest 每 2 秒落库,入库比观测晚最多约 5 秒。
// 只拿观测时刻(以前界面发的 after = quote.t)和读簿时刻比,这张单永远进不了快照,要等 60 秒 TTL。
// 入库标脏(服务端 main 现在把 MarkRefineDirty 和 conflator 一起接在 ingest 上)记的是提交之后的
// 服务端时刻,带着旧式的 after 来也要重建、读到它
func TestRefine观测早于快照入库晚于快照的单要读到(t *testing.T) {
	src := barFixture()
	s, clk := newRefineService(src)
	if res := mustRefine(t, s, s.RefineDefaults(tBar, "")); res.Sell.Post.BestAsk != 1000 {
		t.Fatalf("前提:卖一 %d", res.Sell.Post.BestAsk)
	}
	// 3 秒后入库一张 rNow-1s 观测到的单,ingest 提交之后标脏
	clk.Add(int64(3 * time.Second))
	src.mu.Lock()
	src.orders = append(src.orders, rOrders(tBar, "Thetford", model.SideOffer, rNow.Add(-time.Second), 980, 4)...)
	src.mu.Unlock()
	s.MarkRefineDirty(model.QuoteKey{ItemID: tBar, LocationID: "Thetford", Quality: 1, Side: model.SideOffer})

	clk.Add(int64(2 * time.Second))
	res, err := s.RefineWith(context.Background(), s.RefineDefaults(tBar, ""),
		RefineOpts{After: rNow.Add(-time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Sell.Post.BestAsk != 980 || !res.Books.ReadAt.Equal(rNow.Add(5*time.Second)) {
		t.Fatalf("入库标脏后要重建并读到 980:卖一 %d、read_at %v", res.Sell.Post.BestAsk, res.Books.ReadAt)
	}
}

// 客户端带来的 WS 报价时刻(RefineOpts.After)比快照新:重建,不回推送之前读的那份。
func TestRefine客户端带报价时刻时快照比它旧就重建(t *testing.T) {
	src := barFixture()
	s, clk := newRefineService(src)
	mustRefine(t, s, s.RefineDefaults(tBar, ""))
	clk.Add(int64(5 * time.Second))
	p := s.RefineDefaults(tBar, "")
	if _, err := s.RefineWith(context.Background(), p, RefineOpts{After: rNow.Add(-time.Second)}); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := src.calls(); b != 1 {
		t.Fatalf("报价早于快照:不用重建,读簿 %d", b)
	}
	res, err := s.RefineWith(context.Background(), p, RefineOpts{After: rNow.Add(4 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if b, _, _ := src.calls(); b != 2 || !res.Books.ReadAt.Equal(rNow.Add(5*time.Second)) {
		t.Fatalf("报价晚于快照要重建:读簿 %d、read_at %v", b, res.Books.ReadAt)
	}
	// 矩阵也认
	if _, err := s.RefineMatrixWith(context.Background(), "ORE", s.RefineDefaults("", "Thetford"),
		RefineOpts{After: rNow.Add(4 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if b, _, _ := src.calls(); b != 2 {
		t.Fatalf("快照已经比报价新,矩阵不用再读:%d", b)
	}
}

// 快照到期、后台重建又慢(库卡住):最多等 refineSnapWait,先回旧快照算的结果,标 stale,
// read_at 是旧的读簿时刻(界面的 craftLiveAfter 会再要一次);重建完了下一次就是新的。
func TestRefine快照过期回旧值并标stale(t *testing.T) {
	old := refineSnapWait
	refineSnapWait = 50 * time.Millisecond
	defer func() { refineSnapWait = old }()
	src := barFixture()
	s, clk := newRefineService(src)
	mustRefine(t, s, s.RefineDefaults(tBar, ""))
	gate := make(chan struct{})
	src.mu.Lock()
	src.gate = gate
	src.mu.Unlock()
	clk.Store(rNow.Add(61 * time.Second).UnixNano())
	res := mustRefine(t, s, s.RefineDefaults(tBar, ""))
	if res.Books.Timing.Cache != "stale" || !res.Books.ReadAt.Equal(rNow) || res.Books.Timing.WaitMS < 40 {
		t.Fatalf("没等到重建应先用旧的: %+v read_at %v", res.Books.Timing, res.Books.ReadAt)
	}
	close(gate)
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.refine.snapMu.Lock()
		sn, building := s.refine.snaps[snapKey{"ORE", "Thetford"}], len(s.refine.building) > 0
		s.refine.snapMu.Unlock()
		if !building && sn.at.Equal(rNow.Add(61*time.Second)) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("后台重建没完成")
		}
		time.Sleep(time.Millisecond)
	}
	res = mustRefine(t, s, s.RefineDefaults(tBar, ""))
	if res.Books.Timing.Cache != "hit" || !res.Books.ReadAt.Equal(rNow.Add(61*time.Second)) {
		t.Fatalf("重建完应命中新快照: %+v", res.Books.Timing)
	}
	// 冷启动没有旧的可用:读库失败就是错误
	src2 := barFixture()
	src2.bookErr = errors.New("连接断了")
	s2, _ := newRefineService(src2)
	if _, err := s2.Refine(context.Background(), s2.RefineDefaults(tBar, "")); err == nil {
		t.Fatal("冷启动读库失败应报错")
	}
}

// 后台预热只管最近 15 分钟用过、又该重建的快照;同一条线的几座城合成一批读。
func TestRefine预热只刷新最近用过的快照(t *testing.T) {
	src := barFixture()
	s, clk := newRefineService(src)
	p := s.RefineDefaults(tBar, "")
	p.Sell.City = "Martlock"
	mustRefine(t, s, p)
	if b, _, _ := src.calls(); b != 1 {
		t.Fatalf("两座城冷启动应合成一次读簿: %d", b)
	}
	s.warmOnce(context.Background())
	if b, _, _ := src.calls(); b != 1 {
		t.Fatalf("快照还新,预热不读: %d", b)
	}
	clk.Store(rNow.Add(61 * time.Second).UnixNano())
	s.warmOnce(context.Background())
	if b, pr, _ := src.calls(); b != 2 || pr != 1 || len(src.keys) != 2*2*len(s.snapItems(refine.Lines[0])) {
		t.Fatalf("到期的两座城合成一批重读:读簿 %d、AODP %d、边 %d", b, pr, len(src.keys))
	}
	// 之后再没人用:超过 15 分钟就不管了
	clk.Store(rNow.Add(17 * time.Minute).UnixNano())
	s.warmOnce(context.Background())
	if b, _, _ := src.calls(); b != 2 {
		t.Fatalf("15 分钟没人用不该预热: %d", b)
	}
	s.refine.snapMu.Lock()
	n := len(s.refine.active)
	s.refine.snapMu.Unlock()
	if n != 0 {
		t.Fatalf("闲置的应从 active 里清掉: %d", n)
	}
}

func TestRefine参数不合法是ParamError且不碰数据源(t *testing.T) {
	src := barFixture()
	s, _ := newRefineService(src)
	cases := []struct {
		name string
		mut  func(*refine.Params)
	}{
		{"不是半成品", func(p *refine.Params) { p.Target = "T8_ORE_LEVEL4@4" }},
		{"线外物品", func(p *refine.Params) { p.Target = "T8_MAIN_FIRESTAFF" }},
		{"石头线", func(p *refine.Params) { p.Target = "T6_STONEBLOCK" }},
		{"不认识的城市", func(p *refine.Params) { p.City = "Atlantis" }},
		{"不认识的出售城市", func(p *refine.Params) { p.Sell.City = "Atlantis" }},
		// 黑市在服务端城市列表里,但只收装备、没有精炼站、没有卖单
		{"黑市不能当出售城市", func(p *refine.Params) { p.Sell.City = "Black Market" }},
		{"黑市不能当生产城市", func(p *refine.Params) { p.City = "Black Market" }},
		{"黑市不能当买料城市", func(p *refine.Params) { p.Sources.Cities = []string{"Thetford", "Black Market"} }},
		{"不认识的买料城市", func(p *refine.Params) { p.Sources.Cities = []string{"Thetford", "Atlantis"} }},
		{"数据年龄超过窗口", func(p *refine.Params) { p.Sources.MaxAgeHours = 100 }},
		{"专注超过上限", func(p *refine.Params) { p.Focus.Budget = 40000 }},
		{"k 录成百分数", func(p *refine.Params) { p.Fees.TransmuteFactor = 115.4 }},
		{"件数 0", func(p *refine.Params) { p.Qty = 0 }},
		{"不存在的附魔", func(p *refine.Params) { p.Target = "T8_METALBAR_LEVEL5@5" }},
		{"不规范的 id", func(p *refine.Params) { p.Target = "T8_METALBAR_LEVEL0@0" }},
		{"本金 +Inf", func(p *refine.Params) { p.Daily.Capital = math.Inf(1) }},
	}
	for _, c := range cases {
		p := s.RefineDefaults(tBar, "")
		c.mut(&p)
		_, err := s.Refine(context.Background(), p)
		var pe *ParamError
		if !errors.As(err, &pe) {
			t.Errorf("%s:应是 ParamError,得到 %v", c.name, err)
		}
	}
	if b, pr, v := src.calls(); b+pr+v != 0 {
		t.Fatalf("参数不合法不该读数据:读簿 %d、AODP %d、成交量 %d", b, pr, v)
	}
}

func TestRefine读库失败是错误_AODP失败只告警(t *testing.T) {
	src := barFixture()
	src.bookErr = errors.New("连接断了")
	s, _ := newRefineService(src)
	_, err := s.Refine(context.Background(), s.RefineDefaults(tBar, ""))
	var pe *ParamError
	if err == nil || errors.As(err, &pe) {
		t.Fatalf("读库失败应是普通错误: %v", err)
	}

	src2 := barFixture()
	src2.priceErr = errors.New("AODP 503")
	s2, _ := newRefineService(src2)
	res := mustRefine(t, s2, s2.RefineDefaults(tBar, ""))
	if res.Books.AODPOK || res.Books.AODPError == "" || !hasWarn(res.Warnings, WarnAODPUnavailable) {
		t.Fatalf("AODP 没取到应只告警: %+v", res.Books)
	}
	// T3 条没有抓包、AODP 也没取到,就是缺价(结果照样回,只是拿不满料)
	if !slices.Contains(res.Data.Missing, tT3) || res.Candidates[0].Filled {
		t.Fatalf("T3 条应缺价: %+v", res.Data)
	}

	src3 := barFixture()
	src3.volErr = errors.New("查询超时")
	s3, _ := newRefineService(src3)
	if _, err := s3.Refine(context.Background(), s3.RefineDefaults(tBar, "")); err == nil || errors.As(err, &pe) {
		t.Fatalf("读成交历史失败应是普通错误: %v", err)
	}
}

// 服务层只是搬运:同一份盘口,经服务层算出来的和手工拼好 Books 直接调引擎的逐字节相同
func TestRefine服务层结果和直接调引擎一致(t *testing.T) {
	src := barFixture()
	s, _ := newRefineService(src)
	p := s.RefineDefaults(tBar, "")
	p.Sources.BidOrders = true
	got := mustRefine(t, s, p)

	seen := rNow.Add(-time.Hour)
	lad := func(pairs ...int64) refine.Ladder {
		l := refine.Ladder{Seen: seen, Source: "capture"}
		for i := 0; i+1 < len(pairs); i += 2 {
			l.Levels = append(l.Levels, depth.Level{Price: pairs[i], Qty: pairs[i+1]})
		}
		return l
	}
	books := refine.Books{
		{ItemID: tBar, City: "Thetford"}:  {Asks: lad(1000, 20, 1010, 30), Bids: lad(900, 5, 890, 10)},
		{ItemID: tOre, City: "Thetford"}:  {Asks: lad(200, 100, 210, 200), Bids: lad(150, 50)},
		{ItemID: tOre0, City: "Thetford"}: {Asks: lad(50, 1000)},
		{ItemID: tT3, City: "Thetford"}: {Asks: refine.Ladder{Levels: []depth.Level{{Price: 60}},
			Seen: rNow.Add(-2 * time.Hour), Source: "aodp", DepthUnknown: true}},
	}
	p = refine.Normalized(p)
	p.Now = rNow
	want, err := refine.Plan(refine.Input{Params: p, Books: books, Recipes: s.Recipes(),
		Volume: refine.Volume{Days: 14}})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(got.Result)
	b, _ := json.Marshal(want)
	if string(a) != string(b) {
		t.Fatalf("服务层和直接调引擎不一致\n服务层 %s\n引擎   %s", a, b)
	}
}

func TestRefineMatrix一条线25格共用一次读簿(t *testing.T) {
	src := barFixture()
	s, _ := newRefineService(src)
	base := s.RefineDefaults("", "Thetford")
	m, err := s.RefineMatrix(context.Background(), "ORE", base)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Cells) != 25 || m.Line != "ORE" || m.City != "Thetford" || m.Qty != 10 {
		t.Fatalf("矩阵: %d 格 %s %s %d", len(m.Cells), m.Line, m.City, m.Qty)
	}
	for i, target := range refine.Lines[0].Targets() {
		c := m.Cells[i]
		if c.Target != target || c.Error != "" || c.Sell == nil || c.Volume == nil || c.Data == nil {
			t.Fatalf("第 %d 格: %+v", i, c)
		}
	}
	bar := m.Cells[1] // T4.1
	if bar.Target != tBar || bar.Best == nil || !bar.Best.Filled || bar.Best.Profit.Post == nil || bar.Candidates == 0 {
		t.Fatalf("T4.1 条那一格应有能算的最优候选: %+v", bar.Best)
	}
	// 单独算那一格,最优候选是同一条
	single := mustRefine(t, s, s.RefineDefaults(tBar, "Thetford"))
	if single.Candidates[0].ID != bar.Best.ID || single.Candidates[0].UnitCost != bar.Best.UnitCost {
		t.Fatalf("矩阵和单算不一致: %s %g vs %s %g", bar.Best.ID, bar.Best.UnitCost,
			single.Candidates[0].ID, single.Candidates[0].UnitCost)
	}
	// 单算那一格命中矩阵建的快照,不再读
	if b, pr, v := src.calls(); b != 1 || pr != 1 || v != 1 {
		t.Fatalf("矩阵应一次读簿、一次 AODP、一次成交量,单算那次命中快照:%d %d %d", b, pr, v)
	}
	if len(m.Unverified) == 0 {
		t.Fatal("矩阵要列未核实参数")
	}

	for _, bad := range []struct {
		line string
		mut  func(*refine.Params)
	}{
		{"ROCK", func(*refine.Params) {}},
		{"ore", func(*refine.Params) {}},
		{"ORE", func(p *refine.Params) { p.Focus.Mode = refine.FocusCustom; p.Focus.CustomTiers = []int{8} }},
		{"ORE", func(p *refine.Params) { p.City = "Atlantis" }},
	} {
		p := s.RefineDefaults("", "Thetford")
		bad.mut(&p)
		_, err := s.RefineMatrix(context.Background(), bad.line, p)
		var pe *ParamError
		if !errors.As(err, &pe) {
			t.Errorf("%s:应是 ParamError,得到 %v", bad.line, err)
		}
	}
}

// 服务端的默认值原样回给界面:界面按它判「是不是默认值」,k 的录入日期和金价也从这里拿
func TestRefine回带服务端默认值(t *testing.T) {
	s, _ := newRefineService(barFixture())
	s.Cfg.Refining.TransmuteMeasuredAt = "2026-09-20"
	s.Cfg.Refining.TransmuteGoldPrice = 13554
	s.Cfg.Refining.StationFeeByCity = map[string]float64{"Thetford": 800}
	res := mustRefine(t, s, s.RefineDefaults(tBar, ""))
	d := res.ServerDefaults
	if d.TransmuteMeasuredAt != "2026-09-20" || d.TransmuteGoldPrice != 13554 || d.TransmuteFactor != 1.154 ||
		d.StationFeeByCity["Thetford"] != 800 || d.WindowHours != 72 || d.Capital != 10_000_000 || d.DailyBonus == nil {
		t.Fatalf("server_defaults: %+v", d)
	}
	if res.ParamsUsed.Fees.StationPer100 != 800 {
		t.Fatalf("Thetford 覆盖了站费,打底参数应是 800: %g", res.ParamsUsed.Fees.StationPer100)
	}
	m, err := s.RefineMatrix(context.Background(), "ORE", s.RefineDefaults("", "Thetford"))
	if err != nil || m.ServerDefaults.TransmuteMeasuredAt != "2026-09-20" {
		t.Fatalf("矩阵也要回带: %+v %v", m.ServerDefaults, err)
	}
}

func TestRefineRecipe用配置的k(t *testing.T) {
	s, _ := newRefineService(&fakeRefine{})
	tr, err := s.RefineRecipe("T8_METALBAR_LEVEL4@4", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tr.TransmuteFactor != 1.154 || tr.TransmuteStationPer100 != 500 || len(tr.Chain) != 6 {
		t.Fatalf("配方树: %+v", tr)
	}
	k := 1.0
	tr, err = s.RefineRecipe("T8_ORE_LEVEL4@4", &k, nil)
	if err != nil || tr.TransmuteFactor != 1.0 {
		t.Fatalf("请求带的 k 优先: %+v %v", tr, err)
	}
	var pe *ParamError
	if _, err := s.RefineRecipe("T8_MAIN_FIRESTAFF", nil, nil); !errors.As(err, &pe) {
		t.Fatalf("线外物品应是 ParamError: %v", err)
	}
}

// 串城的盘口边暂停剔除,slack 放到整个精炼窗口(72h),不是扫描的 6h
func TestRefine串城的盘口在精炼窗口里暂停剔除(t *testing.T) {
	src := barFixture()
	k := model.QuoteKey{ItemID: tOre, LocationID: "Thetford", Quality: 1, Side: model.SideOffer}
	src.orders = slices.DeleteFunc(src.orders, func(o store.LiveOrder) bool {
		return o.ItemID == tOre && o.Side == model.SideOffer
	})
	// 20 小时前那一轮 190 的,1 小时前这一轮 200 起:平时 190 是幽灵单
	src.orders = append(src.orders, rOrders(tOre, "Thetford", model.SideOffer, rNow.Add(-20*time.Hour), 190, 7)...)
	src.orders = append(src.orders, rOrders(tOre, "Thetford", model.SideOffer, rNow.Add(-time.Hour), 200, 100, 210, 200)...)

	s, _ := newRefineService(src)
	best := func(res *RefinePlan) int64 {
		for _, rs := range res.RawSources {
			for _, sp := range rs.Mixed.Split {
				if sp.Start == tOre {
					return sp.PriceFrom
				}
			}
		}
		return 0
	}
	res := mustRefine(t, s, s.RefineDefaults(tBar, ""))
	if got := best(res); got != 200 || len(res.Books.Conflicted) != 0 {
		t.Fatalf("没串城时 190 是幽灵单,应从 200 起买: %d %v", got, res.Books.Conflicted)
	}

	s2, _ := newRefineService(src)
	s2.Conflicts = fakeConflicts{k: rNow.Add(-30 * time.Minute)}
	res2 := mustRefine(t, s2, s2.RefineDefaults(tBar, ""))
	if got := best(res2); got != 190 {
		t.Fatalf("串过城的盘口暂停剔除,190 那张要留着: %d", got)
	}
	if !slices.Equal(res2.Books.Conflicted, []string{tOre + "|Thetford|卖单"}) || !hasWarn(res2.Warnings, WarnConflicted) {
		t.Fatalf("串城要列出来并告警: %v %+v", res2.Books.Conflicted, res2.Warnings)
	}
}

// oldPageFixture:T4.1 矿卖单的「最近一轮」是 3.5 小时前从 200 起的两档,更早还有一整页
// (50 张,161~165)。那一页离最近一轮 gap 那么久。
func oldPageFixture(gap time.Duration) *fakeRefine {
	src := barFixture()
	src.orders = slices.DeleteFunc(src.orders, func(o store.LiveOrder) bool {
		return o.ItemID == tOre && o.Side == model.SideOffer
	})
	latest := rNow.Add(-210 * time.Minute)
	page := []int64{161, 4, 163, 1, 164, 1}
	for i := 0; i < 47; i++ {
		page = append(page, 165, 21)
	}
	src.orders = append(src.orders, rOrders(tOre, "Thetford", model.SideOffer, latest.Add(-gap), page...)...)
	src.orders = append(src.orders, rOrders(tOre, "Thetford", model.SideOffer, latest, 200, 100, 210, 200)...)
	return src
}

func hoursOf(p *float64) string {
	if p == nil {
		return "nil"
	}
	return strconv.FormatFloat(*p, 'f', 3, 64) + "h"
}

func oreBuy(t *testing.T, c refine.Candidate) refine.Step {
	t.Helper()
	for _, st := range c.Steps {
		if st.Op == "buy" && st.Item == tOre {
			return st
		}
	}
	t.Fatalf("候选 %s 没有买 %s: %+v", c.ID, tOre, c.Steps)
	return refine.Step{}
}

// 读簿窗口是 72h:几天前那一页满页、最近一轮又恰好接在它后面时,旧页不能被当成续页留下来
// (审查 finding 2:影子库里 69 小时前的残单被计划买进,步骤却报 3.5 小时、ok)。
// 真续页(同一次浏览,隔几分钟)照旧保留,而且步骤、候选的新鲜度按实际吃到的最旧那一档报。
func TestRefine隔天的旧页不当续页_新鲜度按吃到的档报(t *testing.T) {
	s, _ := newRefineService(oldPageFixture(65 * time.Hour))
	res := mustRefine(t, s, s.RefineDefaults(tBar, ""))
	c := res.Candidates[0]
	st := oreBuy(t, c)
	if st.PriceFrom != 200 {
		t.Fatalf("65 小时前那一页不是续页,应当从本轮的 200 起买,得到 %d(%+v)", st.PriceFrom, st.Levels)
	}
	if st.AgeH == nil || math.Abs(*st.AgeH-3.5) > 1e-6 {
		t.Fatalf("买入步骤的年龄应是 3.5h: %s", hoursOf(st.AgeH))
	}

	// 同一次浏览里隔 10 分钟的续页:保留第 1 页,而且年龄按第 1 页那一档报
	s2, _ := newRefineService(oldPageFixture(10 * time.Minute))
	res2 := mustRefine(t, s2, s2.RefineDefaults(tBar, ""))
	c2 := res2.Candidates[0]
	st2 := oreBuy(t, c2)
	if st2.PriceFrom != 161 {
		t.Fatalf("隔 10 分钟的续页应当保留第 1 页,从 161 起买,得到 %d", st2.PriceFrom)
	}
	wantAge := 3.5 + 10.0/60
	if st2.AgeH == nil || math.Abs(*st2.AgeH-wantAge) > 1e-3 {
		t.Fatalf("吃到了第 1 页的档,步骤年龄应按那一档报 %.3fh,得到 %s", wantAge, hoursOf(st2.AgeH))
	}
	if c2.OldestAgeH == nil || *c2.OldestAgeH < wantAge-1e-3 {
		t.Fatalf("候选的最旧年龄应不小于 %.3fh: %s", wantAge, hoursOf(c2.OldestAgeH))
	}

	// 数据最大年龄 3.6h:第 1 页那几档(3.67h)太旧不用,本轮(3.5h)的照用
	p := s2.RefineDefaults(tBar, "")
	p.Sources.MaxAgeHours = 3.6
	res3 := mustRefine(t, s2, p)
	if st3 := oreBuy(t, res3.Candidates[0]); st3.PriceFrom != 200 {
		t.Fatalf("比 max_age_hours 旧的档不该用,应从 200 起买,得到 %d", st3.PriceFrom)
	}
}

// 用户把数据最大年龄调得比读簿窗口小:抓包这一边整个太旧、AODP 的价够新时,用 AODP(深度未知),
// 不能让太旧的抓包把新的 AODP 价挡在外面变成缺价(审查 finding 8)。
func TestRefine抓包太旧时用更新的AODP(t *testing.T) {
	src := barFixture()
	src.orders = slices.DeleteFunc(src.orders, func(o store.LiveOrder) bool {
		return o.ItemID == tOre && o.Side == model.SideOffer
	})
	src.orders = append(src.orders, rOrders(tOre, "Thetford", model.SideOffer, rNow.Add(-10*time.Hour), 200, 100)...)
	src.prices = append(src.prices, aodp.PriceRecord{ItemID: tOre, City: "Thetford", Quality: 1,
		SellPriceMin: 205, SellPriceMinDate: aodp.Stamp{T: rNow.Add(-time.Hour)}})
	s, _ := newRefineService(src)
	p := s.RefineDefaults(tBar, "")
	p.Sources.MaxAgeHours = 6
	res := mustRefine(t, s, p)
	if slices.Contains(res.Data.Missing, tOre) || len(res.Data.Stale) != 0 {
		t.Fatalf("AODP 有 1 小时前的价,T4.1 矿不该缺价: missing %v stale %v", res.Data.Missing, res.Data.Stale)
	}
	st := oreBuy(t, res.Candidates[0])
	if st.Source != "aodp" || st.PriceFrom != 205 || st.AgeH == nil || math.Abs(*st.AgeH-1) > 1e-6 {
		t.Fatalf("应按 AODP 的 205 买、年龄 1h: %+v", st)
	}
	want := tOre + "|Thetford|卖单"
	if !slices.Equal(res.Books.AODPOverStale, []string{want}) || res.Books.AODP != 2 || res.Books.Capture != 4 {
		t.Fatalf("books: %+v", res.Books)
	}

	// 默认的 72h:10 小时前的抓包阶梯照用
	res2 := mustRefine(t, s, s.RefineDefaults(tBar, ""))
	if st := oreBuy(t, res2.Candidates[0]); st.Source != "capture" || st.PriceFrom != 200 {
		t.Fatalf("没超过数据最大年龄时仍用抓包阶梯: %+v", st)
	}
	// AODP 也太旧:照旧当缺价(stale),不拿更旧的 AODP 顶
	src.prices[len(src.prices)-1].SellPriceMinDate = aodp.Stamp{T: rNow.Add(-8 * time.Hour)}
	s3, _ := newRefineService(src)
	p3 := s3.RefineDefaults(tBar, "")
	p3.Sources.MaxAgeHours = 6
	res3 := mustRefine(t, s3, p3)
	if !slices.Contains(res3.Data.Stale, want) || len(res3.Books.AODPOverStale) != 0 {
		t.Fatalf("两边都太旧时应当是 stale: %+v %+v", res3.Data.Stale, res3.Books.AODPOverStale)
	}
}

// coverConflicts 是带覆盖范围的串城来源(*ingest.Ingestor 那样)。
type coverConflicts struct {
	fakeConflicts
	since time.Time
}

func (c coverConflicts) ConflictCoverageSince(time.Time) time.Time { return c.since }

// 串城判定只覆盖服务端这次启动之后、保留期之内(审查 finding 10):books.conflicts_since 标出来。
func TestRefine标出串城判定覆盖到多早(t *testing.T) {
	s, _ := newRefineService(barFixture())
	if res := mustRefine(t, s, s.RefineDefaults(tBar, "")); res.Books.ConflictsSince != nil {
		t.Fatalf("没接串城检测时是 null: %v", res.Books.ConflictsSince)
	}
	s2, _ := newRefineService(barFixture())
	since := rNow.Add(-5 * time.Hour)
	s2.Conflicts = coverConflicts{fakeConflicts{}, since}
	res := mustRefine(t, s2, s2.RefineDefaults(tBar, ""))
	if res.Books.ConflictsSince == nil || !res.Books.ConflictsSince.Equal(since) {
		t.Fatalf("conflicts_since 应是 %v,得到 %v", since, res.Books.ConflictsSince)
	}
}

// 矩阵里 25 次 Plan 并行跑,共用同一份 books:WSL 的 -race 会盯这里
func TestRefineMatrix并行和串行结果一致(t *testing.T) {
	src := barFixture()
	s, _ := newRefineService(src)
	m, err := s.RefineMatrix(context.Background(), "ORE", s.RefineDefaults("", "Thetford"))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, 25)
	for _, c := range m.Cells {
		id := ""
		if c.Best != nil {
			id = c.Best.ID
		}
		ids = append(ids, c.Target+"="+id)
	}
	for i, target := range refine.Lines[0].Targets() {
		p := s.RefineDefaults(target, "Thetford")
		res := mustRefine(t, s, p)
		id := ""
		for _, c := range res.Candidates {
			if c.Reference == "" {
				id = c.ID
				break
			}
		}
		if ids[i] != target+"="+id {
			t.Fatalf("第 %d 格并行 %s,单算 %s", i, ids[i], target+"="+id)
		}
	}
}
