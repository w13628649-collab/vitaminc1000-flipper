package protocol

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"albion-guild/internal/capture"
	"albion-guild/internal/model"
)

// 真实市场录包过整条解析链:photon 层 → protocol.Parser → 挂单。
//
// 2026-11-23 游戏升到 1.32.040 后怀疑抓不到挂单(docs/market-capture-handoff-2026-11-23.md),
// 作者在家里用新版录了这一段:20:18 进 Martlock 传送门城,20:20 进 Martlock 城、进市场,
// 翻符文、纤维的卖单和买单。以前的真实夹具只有进图、移动、切区,市场这条路从没做过真实端到端回归。
//
// **录包不进 git**:testdata/local/ 在仓库 .gitignore 里,里面有作者自己的角色、别的玩家的名字和
// 挂单。没有这个文件(换了台机器、新克隆的仓库)就跳过。
//
// 顺带钉住新版的两个形状:挂单响应现在带业务码 81/82(以前走的是没有 253 的那条路);
// 城市挂单的 LocationId 是 null(以前是空串),都要靠当前所在地补成 3008
const marketCapture = "testdata/local/market-1.32.040.pcap"

func TestLocalCapture_1_32_040市场挂单端到端(t *testing.T) {
	if _, err := os.Stat(marketCapture); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s 不在本机(录包不进仓库),跳过", marketCapture)
	}
	p := New()
	var orders []model.MarketOrder
	p.OnOrders = func(o []model.MarketOrder) { orders = append(orders, o...) }
	var responded int
	p.OnMarketResponded = func() { responded++ }
	var at time.Time
	p.SetClock(func() time.Time { return at })
	h := capture.Handler{
		OnRequest:  p.HandleRequest,
		OnResponse: func(c byte, rc int16, _ string, params map[byte]any) { p.HandleResponseCode(c, rc, params) },
		OnEvent:    p.HandleEvent,
	}
	st, err := capture.ReadFile(context.Background(), filepath.FromSlash(marketCapture), 0, h,
		func(ts time.Time) { at = ts })
	if err != nil {
		t.Fatal(err)
	}
	if st.Fed == 0 {
		t.Fatalf("一个包都没交给解析器: %+v", st)
	}

	// 8 次卖单 + 3 次买单,全是明文响应
	if responded != 11 {
		t.Fatalf("明文市场响应 %d 条,应为 11(8 次 81 + 3 次 82)", responded)
	}
	if len(orders) == 0 || p.HeldOrders() != 0 || p.Location() != "3008" {
		t.Fatalf("挂单 %d 条、扣着 %d 条、最后位置 %q:应解出挂单、全补上位置、停在 Martlock 市场 3008",
			len(orders), p.HeldOrders(), p.Location())
	}
	side := map[model.Side]int{}
	items := map[string]bool{}
	for _, o := range orders {
		if o.LocationID != "3008" || o.ItemID == "" || o.UnitPrice <= 0 || o.Amount <= 0 || o.Quality < 1 {
			t.Fatalf("字段不对: %+v", o)
		}
		if o.ObservedAt.Before(st.First) || o.ObservedAt.After(st.Last) {
			t.Fatalf("观测时间要用包上的时间: %v 不在 %v ~ %v", o.ObservedAt, st.First, st.Last)
		}
		side[o.Side]++
		items[o.ItemID] = true
	}
	if side[model.SideOffer] == 0 || side[model.SideRequest] == 0 {
		t.Fatalf("卖单和买单都要有: %v", side)
	}
	for _, id := range []string{"T4_RUNE", "T2_FIBER", "T4_FIBER_LEVEL3@3"} {
		if !items[id] {
			t.Errorf("翻过的 %s 没解出来", id)
		}
	}
	// 第一页卖单的第一条:T4 符文 11 件,包里 UnitPriceSilver=80000 → 8 银
	first := orders[0]
	if first.OrderID != 3393925145 || first.ItemID != "T4_RUNE" || first.UnitPrice != 8 || first.Amount != 11 ||
		first.Side != model.SideOffer {
		t.Fatalf("第一条挂单: %+v", first)
	}
}
