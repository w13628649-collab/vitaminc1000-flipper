package screen

import (
	"math"
	"strings"
	"testing"
)

func TestThousands_分组(t *testing.T) {
	for n, want := range map[int64]string{
		0: "0", 7: "7", 999: "999", 1000: "1,000", 12345: "12,345", 260066: "260,066",
		2022222: "2,022,222", -9500: "-9,500", -12: "-12", math.MinInt64: "-9,223,372,036,854,775,808",
	} {
		if got := Thousands(n); got != want {
			t.Errorf("Thousands(%d) = %q,想要 %q", n, got, want)
		}
	}
	for v, want := range map[float64]string{
		9.46: "9.5", 402: "402.0", 86543.21: "86,543.2", -1234.5: "-1,234.5", -0.01: "0.0", 0: "0.0",
	} {
		if got := thousands1(v); got != want {
			t.Errorf("thousands1(%v) = %q,想要 %q", v, got, want)
		}
	}
}

// 验收原样:「税后亏 -9.5 银/件」负号和"亏"叠成双重否定;价格也没有千分位
func TestUnprofitable_亏损写正数带千分位(t *testing.T) {
	rec := makePrice(priceOpt{buy: 2_000_000, sell: 2_100_000}) // 5% 价差
	stats := makeStats(statsOpt{avg7d: 2_050_000, dailyQty: 50})
	_, rej := Evaluate(rec, stats, config(), names, now)
	if rej == nil || rej.Reason != "unprofitable" {
		t.Fatalf("5%% 价差应判 unprofitable,得到 %+v", rej)
	}
	if strings.Contains(rej.Detail, "-") || !strings.Contains(rej.Detail, "税后每件亏 ") {
		t.Fatalf("亏损应写成正数「税后每件亏 X 银」,得到 %q", rej.Detail)
	}
	if !strings.Contains(rej.Detail, ",") {
		t.Fatalf("几万银的单件亏损应带千分位,得到 %q", rej.Detail)
	}
	// 明细带上判它时用的两边价:机会页不用再去查价页翻
	if rej.AskPrice != 2_100_000 || rej.BidPrice != 2_000_000 {
		t.Fatalf("拒绝应带卖一 / 买一,得到 %d / %d", rej.AskPrice, rej.BidPrice)
	}
}

// 只有卖单的那一类:买一 0 不输出(omitempty),卖一照带
func TestOneSided_只带有的那一边价(t *testing.T) {
	rec := makePrice(priceOpt{sell: 249376})
	rec.BuyPriceMax = 0 // makePrice 把 0 补成默认买价,这里要的就是"没有买单"
	_, rej := Evaluate(rec, makeStats(statsOpt{}), config(), names, now)
	if rej == nil || rej.Reason != "one_sided" {
		t.Fatalf("只有卖单应判 one_sided,得到 %+v", rej)
	}
	if rej.AskPrice != 249376 || rej.BidPrice != 0 {
		t.Fatalf("应只带卖一,得到 %d / %d", rej.AskPrice, rej.BidPrice)
	}
}
