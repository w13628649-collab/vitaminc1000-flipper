package flip

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"albion-guild/internal/aodp"
	"albion-guild/internal/book"
	"albion-guild/internal/conf"
	"albion-guild/internal/model"
	"albion-guild/internal/store"
)

// bmLadder 是按 key 和 since 过滤的假库
type bmLadder struct{ orders []store.LiveOrder }

func (f *bmLadder) BookOrders(_ context.Context, keys []model.QuoteKey, since time.Time) ([]store.LiveOrder, error) {
	want := map[model.QuoteKey]bool{}
	for _, k := range keys {
		want[k] = true
	}
	var in []store.LiveOrder
	for _, o := range f.orders {
		k := model.QuoteKey{ItemID: o.ItemID, LocationID: o.City, Quality: int16(o.Quality), Side: o.Side}
		if want[k] && o.LastSeen.After(since) {
			in = append(in, o)
		}
	}
	return book.WithPages(in), nil
}

func (f *bmLadder) CapturedItems(_ context.Context, cities []string, qualities []int, since time.Time) ([]store.CapturedItem, error) {
	seen := map[[2]any]bool{}
	var out []store.CapturedItem
	for _, o := range f.orders {
		k := [2]any{o.ItemID, o.Quality}
		if !slices.Contains(cities, o.City) || !slices.Contains(qualities, o.Quality) || !o.LastSeen.After(since) || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, store.CapturedItem{ItemID: o.ItemID, Quality: o.Quality})
	}
	return out, nil
}

func bmOrder(item, city string, q int, side model.Side, price, qty int64, at time.Time) store.LiveOrder {
	return store.LiveOrder{ItemID: item, City: city, Quality: q, Side: side, Price: price, Amount: qty, LastSeen: at}
}

func bmService(orders ...store.LiveOrder) (*Service, *[]string) {
	var asked []string
	s := &Service{Cfg: conf.Default(), ladder: &bmLadder{orders: orders}}
	s.bmFetch = func(_ context.Context, items, _ []string, _ []int) ([]aodp.PriceRecord, error) {
		asked = append(asked, items...)
		return nil, nil
	}
	return s, &asked
}

// 黑市收普通 3 件,Martlock 有杰出 1 件、普通 2 件:三件都做,杰出那件降级卖进普通单
func TestBlackMarket_抓包两边都有(t *testing.T) {
	now := time.Now()
	s, asked := bmService(
		bmOrder("T6_2H_AXE", conf.BlackMarket, 1, model.SideRequest, 118_000, 3, now.Add(-10*time.Minute)),
		bmOrder("T6_2H_AXE", "Martlock", 4, model.SideOffer, 82_000, 1, now.Add(-30*time.Minute)),
		bmOrder("T6_2H_AXE", "Martlock", 1, model.SideOffer, 90_000, 2, now.Add(-30*time.Minute)),
	)
	plan, err := s.BlackMarket(context.Background(), BlackMarketParams{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Error != "" || plan.Items != 1 || plan.CaptureItems != 1 || plan.BMNewest.IsZero() {
		t.Fatalf("概况: %+v", plan)
	}
	if len(plan.Rows) != 2 {
		t.Fatalf("应有两行(杰出→普通、普通→普通),得到 %+v", plan.Rows)
	}
	var qty int64
	for _, r := range plan.Rows {
		qty += r.Qty
		if r.City != "Martlock" || r.SellQuality != 1 || r.BuyEst || r.SellEst || r.Name == "" {
			t.Fatalf("行: %+v", r)
		}
	}
	// 普通两件合计赚得多,排在杰出那一件前面:按这单总赚排,不按每件
	if qty != 3 || plan.Rows[0].BuyQuality != 1 || plan.Rows[1].BuyQuality != 4 || plan.Rows[0].Profit < plan.Rows[1].Profit {
		t.Fatalf("合计 %d 件: %+v", qty, plan.Rows)
	}
	if !slices.Equal(*asked, []string{"T6_2H_AXE"}) {
		t.Fatalf("该给抓包里的黑市物品现取 AODP,问了 %v", *asked)
	}
}

// 黑市那边超过 45 分钟的不算;主城那边超过 180 分钟的不算
func TestBlackMarket_两侧数据龄分开卡(t *testing.T) {
	now := time.Now()
	s, _ := bmService(
		bmOrder("OLD_BM", conf.BlackMarket, 1, model.SideRequest, 118_000, 3, now.Add(-50*time.Minute)),
		bmOrder("OLD_BM", "Martlock", 1, model.SideOffer, 50_000, 3, now.Add(-time.Minute)),
		bmOrder("OLD_CITY", conf.BlackMarket, 1, model.SideRequest, 118_000, 3, now.Add(-time.Minute)),
		bmOrder("OLD_CITY", "Martlock", 1, model.SideOffer, 50_000, 3, now.Add(-190*time.Minute)),
		bmOrder("OLD_CITY", "Lymhurst", 1, model.SideOffer, 60_000, 1, now.Add(-170*time.Minute)),
	)
	plan, err := s.BlackMarket(context.Background(), BlackMarketParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].ItemID != "OLD_CITY" || plan.Rows[0].City != "Lymhurst" {
		t.Fatalf("只该剩 OLD_CITY 在 Lymhurst 那一件: %+v", plan.Rows)
	}
	// 放宽黑市那侧,OLD_BM 就回来了
	plan, _ = s.BlackMarket(context.Background(), BlackMarketParams{BMMaxAge: time.Hour})
	if !slices.ContainsFunc(plan.Rows, func(r BlackMarketRow) bool { return r.ItemID == "OLD_BM" }) {
		t.Fatalf("放宽到 1 小时应有 OLD_BM: %+v", plan.Rows)
	}
}

// 主城没抓过,用 AODP 的最优价,件数按 1 件、标 BuyEst;同一物品 3 分钟内不重复问 AODP
func TestBlackMarket_主城用AODP兜底(t *testing.T) {
	now := time.Now()
	s, _ := bmService(bmOrder("T5_BAG", conf.BlackMarket, 2, model.SideRequest, 40_000, 5, now.Add(-5*time.Minute)))
	calls := 0
	s.bmFetch = func(_ context.Context, items, cities []string, _ []int) ([]aodp.PriceRecord, error) {
		calls++
		if !slices.Contains(cities, conf.BlackMarket) || slices.Contains(cities[:len(cities)-1], conf.BlackMarket) {
			t.Fatalf("现取的地点该是各城加黑市: %v", cities)
		}
		return []aodp.PriceRecord{
			{ItemID: "T5_BAG", City: "Lymhurst", Quality: 3, SellPriceMin: 30_000, SellPriceMinDate: aodp.Stamp{T: now.Add(-20 * time.Minute)}},
			{ItemID: "T5_BAG", City: "Thetford", Quality: 1, SellPriceMin: 10_000, SellPriceMinDate: aodp.Stamp{T: now.Add(-20 * time.Minute)}},
		}, nil
	}
	plan, err := s.BlackMarket(context.Background(), BlackMarketParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 1 {
		t.Fatalf("普通货进不了良好的单,只该有 Lymhurst 那行: %+v", plan.Rows)
	}
	r := plan.Rows[0]
	if r.City != "Lymhurst" || r.BuyQuality != 3 || r.SellQuality != 2 || r.Qty != 1 || !r.BuyEst || r.SellEst {
		t.Fatalf("行: %+v", r)
	}
	if _, err := s.BlackMarket(context.Background(), BlackMarketParams{}); err != nil || calls != 1 {
		t.Fatalf("3 分钟内不该再问 AODP,问了 %d 次", calls)
	}
}

// AODP 没回:记在 Error 里,抓包的照样出;下次还会再问
func TestBlackMarket_AODP失败不影响抓包(t *testing.T) {
	now := time.Now()
	s, _ := bmService(
		bmOrder("T6_2H_AXE", conf.BlackMarket, 1, model.SideRequest, 118_000, 1, now.Add(-time.Minute)),
		bmOrder("T6_2H_AXE", "Caerleon", 1, model.SideOffer, 90_000, 1, now.Add(-time.Minute)),
	)
	calls := 0
	s.bmFetch = func(context.Context, []string, []string, []int) ([]aodp.PriceRecord, error) {
		calls++
		return nil, errors.New("503")
	}
	plan, err := s.BlackMarket(context.Background(), BlackMarketParams{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Error == "" || len(plan.Rows) != 1 {
		t.Fatalf("该带着错误出抓包那行: %+v", plan)
	}
	s.BlackMarket(context.Background(), BlackMarketParams{})
	if calls != 2 {
		t.Fatalf("失败的不该占 3 分钟的名额,问了 %d 次", calls)
	}
}

// 只看选中的城市;不认识的城市、越界的数据龄报参数错
func TestBlackMarket_参数(t *testing.T) {
	now := time.Now()
	s, _ := bmService(
		bmOrder("T6_2H_AXE", conf.BlackMarket, 1, model.SideRequest, 118_000, 2, now.Add(-time.Minute)),
		bmOrder("T6_2H_AXE", "Caerleon", 1, model.SideOffer, 90_000, 1, now.Add(-time.Minute)),
		bmOrder("T6_2H_AXE", "Martlock", 1, model.SideOffer, 80_000, 1, now.Add(-time.Minute)),
	)
	plan, err := s.BlackMarket(context.Background(), BlackMarketParams{Cities: []string{"Caerleon"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 1 || plan.Rows[0].City != "Caerleon" || !slices.Equal(plan.Cities, []string{"Caerleon"}) {
		t.Fatalf("只该有 Caerleon: %+v", plan)
	}
	var pe *ParamError
	for _, p := range []BlackMarketParams{
		{Cities: []string{conf.BlackMarket}},
		{Cities: []string{"Atlantis"}},
		{BMMaxAge: 30 * time.Second},
		{CityMaxAge: 25 * time.Hour},
	} {
		if _, err := s.BlackMarket(context.Background(), p); !errors.As(err, &pe) {
			t.Fatalf("%+v 该报参数错,得到 %v", p, err)
		}
	}
}
