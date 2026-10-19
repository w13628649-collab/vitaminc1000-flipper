package protocol

import (
	"strings"
	"testing"
	"time"

	"albion-guild/internal/model"
)

const bidJSON = `{"Id":456,"ItemTypeId":"T4_PLANKS","LocationId":"","QualityLevel":1,"EnchantmentLevel":0,` +
	`"UnitPriceSilver":3000000,"Amount":200,"AuctionType":"request","Expires":"2026-09-30T00:00:00"}`

// ledgerParser 先进图(Join,对象 id 6740、在 0007),再看一页挂单,让订单进缓存
func ledgerParser(t *testing.T) (*Parser, *[]model.MyTrade, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 19, 20, 0, 0, 0, time.UTC)
	p := New()
	p.SetClock(func() time.Time { return now })
	var got []model.MyTrade
	p.OnTrade = func(tr model.MyTrade) { got = append(got, tr) }
	p.HandleResponse(1, map[byte]any{paramOperationCode: int16(2), 0: int64(6740), 1: "guid", 2: "甲", 8: "0007"})
	p.HandleResponse(1, map[byte]any{0: []string{offerJSON, bidJSON}})
	return p, &got, &now
}

func buy(p *Parser, amount int32, order int64)  { p.HandleRequest(1, req(83, 1, amount, 2, order)) }
func sell(p *Parser, amount int32, order int64) { p.HandleRequest(1, req(315, 4, amount, 1, order)) }
func req(code int16, ka byte, a int32, ko byte, o int64) map[byte]any {
	return map[byte]any{paramOperationCode: code, 0: int64(99), ka: a, ko: o}
}
func done(p *Parser, code int16, rc int16) {
	p.HandleResponseCode(1, rc, map[byte]any{paramOperationCode: code})
}

// 秒买:请求里只有件数和订单号,物品和单价从刚看过的挂单里查;服务端回 0 才记
func TestLedger_即时买(t *testing.T) {
	p, got, now := ledgerParser(t)
	buy(p, 5, 123)
	if len(*got) != 0 {
		t.Fatal("没等到响应就记了")
	}
	done(p, 83, 0)
	if len(*got) != 1 {
		t.Fatalf("应记 1 笔,得到 %d", len(*got))
	}
	tr := (*got)[0]
	if tr.Kind != model.TradeInstantBuy || tr.ItemID != "T5_METALBAR" || tr.Quality != 1 || tr.Amount != 5 ||
		tr.UnitPrice != 1120 || tr.Total != 5600 || tr.Location != "0007" || tr.OrderID != 123 ||
		tr.Character != "甲" || !tr.ObservedAt.Equal(*now) || !strings.HasPrefix(tr.Key, "instant:123:") {
		t.Fatalf("得到 %+v", tr)
	}
	// 同一个响应再来一次(多网卡重复看到)不重复记
	done(p, 83, 0)
	if len(*got) != 1 {
		t.Fatalf("重复响应记了两笔: %d", len(*got))
	}
}

// 秒卖给买单:key1 订单号、key4 件数
func TestLedger_即时卖(t *testing.T) {
	p, got, _ := ledgerParser(t)
	sell(p, 7, 456)
	done(p, 315, 0)
	if len(*got) != 1 || (*got)[0].Kind != model.TradeInstantSell || (*got)[0].ItemID != "T4_PLANKS" ||
		(*got)[0].Amount != 7 || (*got)[0].UnitPrice != 300 || (*got)[0].Total != 2100 {
		t.Fatalf("得到 %+v", *got)
	}
}

// 没成交的几种:服务端回非 0、订单没在列表里见过、响应来得太晚、响应对不上请求
func TestLedger_不算成交的情况(t *testing.T) {
	p, got, now := ledgerParser(t)
	buy(p, 5, 123)
	done(p, 83, 1) // 被别人抢了
	buy(p, 5, 999) // 没见过的订单
	done(p, 83, 0)
	buy(p, 5, 123)
	*now = now.Add(time.Minute) // 超时
	done(p, 83, 0)
	buy(p, 5, 123)
	done(p, 315, 0) // 响应是卖的,对不上
	if len(*got) != 0 {
		t.Fatalf("都不该记,得到 %+v", *got)
	}
}

func ticksOf(t time.Time) int64 { return t.Unix()*10_000_000 + 621355968000000000 }

// 市场邮件:先有列表(认类型),再读正文。同一封再读一次 Key 不变,服务端按 Key 去重
func TestLedger_市场邮件(t *testing.T) {
	p, got, _ := ledgerParser(t)
	recv := time.Date(2026, 10, 18, 9, 30, 0, 0, time.UTC)
	p.HandleResponse(1, map[byte]any{paramOperationCode: int16(174),
		3:  []int64{9001, 9002, 9003},
		7:  []string{"3005", "0007", "1002"},
		11: []string{"MARKETPLACE_SELLORDER_FINISHED_SUMMARY", "MARKETPLACE_BUYORDER_EXPIRED_SUMMARY", "GUILD_SOMETHING"},
		12: []int64{ticksOf(recv), ticksOf(recv), ticksOf(recv)},
	})
	read := func(id int64, body string) {
		p.HandleResponse(1, map[byte]any{paramOperationCode: int16(176), 0: id, 1: body})
	}
	read(9001, "5|T5_METALBAR|56000000|11200000")
	read(9002, "3|10|78400000|T4_PLANKS")
	read(9003, "随便什么")    // 不是市场邮件
	read(7777, "5|X|1|1") // 列表里没见过
	read(9001, "5|T5_METALBAR|56000000|11200000")
	if len(*got) != 3 {
		t.Fatalf("应记 3 笔(9001 两次、9002 一次),得到 %+v", *got)
	}
	a, b := (*got)[0], (*got)[1]
	if a.Kind != model.TradeOrderSold || a.ItemID != "T5_METALBAR" || a.Amount != 5 || a.Total != 5600 ||
		a.UnitPrice != 1120 || a.Location != "3005" || a.MailID != 9001 || a.Key != "mail:9001" ||
		!a.ObservedAt.Equal(recv) || a.Quality != 0 {
		t.Fatalf("卖单卖完: %+v", a)
	}
	// 买单到期:10 件买到 3 件,退款 7840 是没买到的 7 件 → 单价 1120,花了 3360
	if b.Kind != model.TradeBuyExpired || b.Amount != 3 || b.OrderAmount != 10 || b.UnitPrice != 1120 ||
		b.Total != 3360 || b.ItemID != "T4_PLANKS" {
		t.Fatalf("买单到期: %+v", b)
	}
	if (*got)[2].Key != a.Key {
		t.Fatal("同一封邮件再读,Key 要一样")
	}
}

func TestParseMarketMail(t *testing.T) {
	for _, c := range []struct {
		typ, body           string
		kind                string
		amount, orderAmount int32
		unit, total         int64
		item                string
	}{
		{"MARKETPLACE_SELLORDER_FINISHED_SUMMARY", "5|T5_METALBAR|56000000|11200000", model.TradeOrderSold, 5, 0, 1120, 5600, "T5_METALBAR"},
		// 买单买齐:单价用实付总额 ÷ 件数,不用第 4 段的挂单价
		{"MARKETPLACE_BUYORDER_FINISHED_SUMMARY", "4|T6_HIDE@1|50000000|13000000", model.TradeOrderBought, 4, 0, 1250, 5000, "T6_HIDE@1"},
		{"MARKETPLACE_SELLORDER_EXPIRED_SUMMARY", "2|5|30000000|T5_PLANKS", model.TradeSellExpired, 2, 5, 1500, 3000, "T5_PLANKS"},
		// 一件没卖掉:白付了创建费,也要记
		{"MARKETPLACE_SELLORDER_EXPIRED_SUMMARY", "0|5|0|T5_PLANKS", model.TradeSellExpired, 0, 5, 0, 0, "T5_PLANKS"},
		{"BLACKMARKET_SELLORDER_EXPIRED_SUMMARY", "1|1|2500000000|T8_MAIN_SWORD@3", model.TradeBMSellExpired, 1, 1, 250000, 250000, "T8_MAIN_SWORD@3"},
		// 买单到期一件没买到:退款全额,单价 = 退款 ÷ 总件数
		{"MARKETPLACE_BUYORDER_EXPIRED_SUMMARY", "0|10|100000000|T4_PLANKS", model.TradeBuyExpired, 0, 10, 1000, 0, "T4_PLANKS"},
	} {
		got, err := parseMarketMail(c.typ, c.body)
		if err != nil {
			t.Errorf("%s %q: %v", c.typ, c.body, err)
			continue
		}
		if got.Kind != c.kind || got.Amount != c.amount || got.OrderAmount != c.orderAmount ||
			got.UnitPrice != c.unit || got.Total != c.total || got.ItemID != c.item {
			t.Errorf("%s %q: 得到 %+v", c.typ, c.body, got)
		}
	}
	for _, bad := range [][2]string{
		{"GUILD_INVITE", "1|2|3"},
		{"MARKETPLACE_SELLORDER_FINISHED_SUMMARY", "5|T5"},
		{"MARKETPLACE_SELLORDER_FINISHED_SUMMARY", "五|T5|1|1"},
		{"MARKETPLACE_SELLORDER_EXPIRED_SUMMARY", "1|2|3"},
		{"MARKETPLACE_SELLORDER_FINISHED_SUMMARY", "5||1|1"},
	} {
		if _, err := parseMarketMail(bad[0], bad[1]); err == nil {
			t.Errorf("%s %q 应该报错", bad[0], bad[1])
		}
	}
}

// 银币:只认自己的(对象 id 对上 Join 的 key0),没变不报
func TestLedger_银币余额(t *testing.T) {
	p, _, _ := ledgerParser(t)
	var got []model.WalletObs
	p.OnWallet = func(w model.WalletObs) { got = append(got, w) }
	ev := func(obj int64, raw int64) { p.HandleEvent(1, map[byte]any{paramEventCode: int16(81), 0: obj, 1: raw}) }
	ev(6740, 12_345_670_000)
	ev(6740, 12_345_670_000) // 没变
	ev(1111, 99_990_000)     // 别人的
	ev(6740, 12_000_000_000)
	if len(got) != 2 || got[0].Silver != 1_234_567 || got[1].Silver != 1_200_000 || got[0].Character != "甲" {
		t.Fatalf("得到 %+v", got)
	}
}

// 专注:float,不是定点数;变化不到 1 点不报
func TestLedger_专注余额(t *testing.T) {
	p, _, _ := ledgerParser(t)
	var got []model.FocusObs
	p.OnFocus = func(f model.FocusObs) { got = append(got, f) }
	ev := func(v float32) { p.HandleEvent(1, map[byte]any{paramEventCode: int16(10), 0: int64(6740), 3: v}) }
	ev(25000.5)
	ev(25000.9)
	ev(24000)
	if len(got) != 2 || got[0].Focus != 25000.5 || got[1].Focus != 24000 {
		t.Fatalf("得到 %+v", got)
	}
}

func TestFromTicks(t *testing.T) {
	want := time.Date(2026, 10, 19, 12, 34, 56, 0, time.UTC)
	if got := fromTicks(ticksOf(want)); !got.Equal(want) {
		t.Fatalf("得到 %v", got)
	}
	if !fromTicks(0).IsZero() {
		t.Fatal("0 应该是零值")
	}
}
