package scan

import (
	"testing"
	"time"

	"albion-guild/internal/depth"
	"albion-guild/internal/model"
)

// 端到端:T5_WOOD 的买方只抓到 5 件在最优价附近,再往下就是 500 的占位单。
// 价格过滤全过(纯 AODP 时它是榜首),深度闸门把它拦成 no_bid_side;
// 其余物品的判定不受影响(T5_ORE 的 troll 卖单照样是 deviation)
func TestRunWithCapture_薄买方被深度闸门拦下(t *testing.T) {
	books := &fakeBooks{data: map[model.QuoteKey]CapturedSide{
		bidKey("T5_WOOD", "Lymhurst"): side(now.Add(-10*time.Minute),
			depth.Level{Price: 1000, Qty: 5}, depth.Level{Price: 500, Qty: 1000}),
	}}
	res := runCapture(t, captureCfg(), books)

	if findOpp(res, "T5_WOOD") != nil {
		t.Fatal("买方近价只有 5 件,T5_WOOD 不该上榜")
	}
	if res.RejectCounts["no_bid_side"] != 1 || res.RejectCounts["deviation"] != 1 {
		t.Fatalf("应有 1 条 no_bid_side、deviation 仍为 1,得到 %v", res.RejectCounts)
	}
	for _, r := range res.Rejected {
		if r.ItemID == "T5_WOOD" && (r.Reason != "no_bid_side" || r.BidSource != "capture") {
			t.Fatalf("T5_WOOD 应因抓包买方太薄被拒,得到 %+v", r)
		}
	}
	if findOpp(res, "T5_CLOTH") == nil {
		t.Fatal("没有抓包的 T5_CLOTH 不受影响,应照常上榜")
	}
	if res.Capture.BidsUsed != 1 {
		t.Fatalf("买方用上了抓包价,汇总应记 1,得到 %+v", res.Capture)
	}
}
