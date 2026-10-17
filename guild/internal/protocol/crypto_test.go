package protocol

import (
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) add(d time.Duration)     { c.t = c.t.Add(d) }
func newTracker(c *fakeClock) *CryptoTracker { t := NewCryptoTracker(); t.Now = c.now; return t }

const encResponse = byte(0x80 | 3) // 131:加密的操作响应
const encEvent = byte(0x80 | 4)    // 132:加密的事件

func TestCrypto_起始是未知(t *testing.T) {
	st := NewCryptoTracker().Snapshot()
	if st.State != CryptoUnknown || !st.Since.IsZero() {
		t.Fatalf("得到 %+v", st)
	}
	if CryptoUnknown.Text() != "市场数据:未确认(去市场刷一次)" ||
		CryptoClear.Text() != "市场数据:明文" ||
		CryptoEncrypted.Text() != "市场数据:被加密,这个号的挂单传不出来" {
		t.Fatal("文案变了")
	}
}

// 未知 → 明文:唯一的正向信号是挂单响应解出 ≥1 条
func TestCrypto_挂单解出来才置明文(t *testing.T) {
	c := &fakeClock{time.Unix(1000, 0)}
	tr := newTracker(c)
	tr.MarketDecoded(0) // 空列表不算证据
	if tr.Snapshot().State != CryptoUnknown {
		t.Fatal("解出 0 条不该置明文")
	}
	tr.MarketRequest()
	c.add(200 * time.Millisecond)
	tr.MarketDecoded(3)
	st := tr.Snapshot()
	if st.State != CryptoClear || !st.Since.Equal(c.t) {
		t.Fatalf("得到 %+v", st)
	}
}

// 未知 → 加密:请求后窗口内来了加密响应、没等到成功的挂单,窗口到期时翻
func TestCrypto_请求后窗口内只有密文就置加密(t *testing.T) {
	c := &fakeClock{time.Unix(1000, 0)}
	tr := newTracker(c)
	var flips []MarketCrypto
	tr.OnChange = func(_, to MarketCrypto) { flips = append(flips, to) }

	tr.MarketRequest()
	c.add(300 * time.Millisecond)
	tr.Encrypted(encResponse)
	if tr.Snapshot().State != CryptoUnknown {
		t.Fatal("窗口还没到期,不该当场翻:后面可能还有明文挂单")
	}
	c.add(DefaultCryptoWindow)
	tr.Tick()
	st := tr.Snapshot()
	if st.State != CryptoEncrypted || st.OtherEncrypted != 0 {
		t.Fatalf("得到 %+v", st)
	}
	if len(flips) != 1 || flips[0] != CryptoEncrypted {
		t.Fatalf("应该只翻一次到加密,得到 %v", flips)
	}
	// 再来几次 Tick 不重复报
	tr.Tick()
	tr.Tick()
	if len(flips) != 1 {
		t.Fatalf("重复回调了:%v", flips)
	}
}

// 明文 → 加密 → 明文:账号状态是服务端的当前取值,会变
func TestCrypto_明文和加密可以来回翻(t *testing.T) {
	c := &fakeClock{time.Unix(1000, 0)}
	tr := newTracker(c)
	tr.MarketRequest()
	tr.MarketDecoded(1)
	c.add(time.Minute)
	tr.MarketRequest()
	tr.Encrypted(encResponse)
	c.add(DefaultCryptoWindow + time.Second)
	if st := tr.Snapshot(); st.State != CryptoEncrypted {
		t.Fatalf("明文之后又被加密,应翻成加密,得到 %v", st.State)
	}
	tr.MarketRequest()
	tr.MarketDecoded(2)
	if st := tr.Snapshot(); st.State != CryptoClear {
		t.Fatalf("又解出挂单,应翻回明文,得到 %v", st.State)
	}
}

// 窗口里先来一条无关的密文、随后挂单明文到了:不翻加密,那条算"其他"
func TestCrypto_窗口里有密文但挂单成功了不翻(t *testing.T) {
	c := &fakeClock{time.Unix(1000, 0)}
	tr := newTracker(c)
	tr.MarketRequest()
	tr.Encrypted(encResponse)
	c.add(time.Second)
	tr.MarketDecoded(5)
	c.add(DefaultCryptoWindow * 2)
	st := tr.Snapshot()
	if st.State != CryptoClear || st.OtherEncrypted != 1 {
		t.Fatalf("得到 %+v", st)
	}
}

// 和市场请求无关的加密消息不改市场状态,只计数
func TestCrypto_无关的加密不翻转(t *testing.T) {
	c := &fakeClock{time.Unix(1000, 0)}
	tr := newTracker(c)
	var flips int
	tr.OnChange = func(_, _ MarketCrypto) { flips++ }

	// 没发过市场请求:任何密文都无关
	for range 5 {
		tr.Encrypted(encResponse)
		tr.Encrypted(encEvent)
	}
	c.add(time.Hour)
	if st := tr.Snapshot(); st.State != CryptoUnknown || st.OtherEncrypted != 10 {
		t.Fatalf("得到 %+v", st)
	}

	// 已经是明文时,窗口外的密文不改状态
	tr.MarketRequest()
	tr.MarketDecoded(1)
	c.add(DefaultCryptoWindow + time.Second)
	tr.Encrypted(encResponse)
	// 窗口内但不可能是挂单响应的(加密事件)也不算
	tr.MarketRequest()
	tr.Encrypted(encEvent)
	c.add(DefaultCryptoWindow + time.Second)
	st := tr.Snapshot()
	if st.State != CryptoClear || st.OtherEncrypted != 12 {
		t.Fatalf("得到 %+v", st)
	}
	if flips != 1 {
		t.Fatalf("只该有未知→明文那一次,得到 %d 次", flips)
	}
}

// 请求之后窗口里什么都没收到(响应丢了):不下结论
func TestCrypto_窗口里什么都没有不下结论(t *testing.T) {
	c := &fakeClock{time.Unix(1000, 0)}
	tr := newTracker(c)
	tr.MarketRequest()
	c.add(DefaultCryptoWindow + time.Second)
	if st := tr.Snapshot(); st.State != CryptoUnknown {
		t.Fatalf("得到 %v", st.State)
	}
}

// 一次调用里先到期判加密、紧接着就是成功的挂单:两次变化都要报
func TestCrypto_一次调用里翻两次都报(t *testing.T) {
	c := &fakeClock{time.Unix(1000, 0)}
	tr := newTracker(c)
	var flips []MarketCrypto
	tr.OnChange = func(_, to MarketCrypto) { flips = append(flips, to) }
	tr.MarketRequest()
	tr.Encrypted(encResponse)
	c.add(DefaultCryptoWindow + time.Second)
	tr.MarketDecoded(1) // 没有 Tick 过,到期判定就在这次调用里
	if len(flips) != 2 || flips[0] != CryptoEncrypted || flips[1] != CryptoClear {
		t.Fatalf("得到 %v", flips)
	}
}

// Parser 把 81/82 请求和挂单解出条数报给加密判定
func TestParser_市场请求和解出条数接到加密判定(t *testing.T) {
	p := New()
	var reqs, decoded int
	p.OnMarketRequest = func() { reqs++ }
	p.OnMarketDecoded = func(n int) { decoded += n }
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(81), 1: "不读"})
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(82)})
	p.HandleRequest(1, map[byte]any{paramOperationCode: int16(17), 0: "0007"}) // 切区不算
	// 还不知道位置也算解出:那是明文的证据,只是挂单被丢掉
	p2 := New()
	p2.OnMarketDecoded = func(n int) { decoded += n }
	p2.HandleResponse(1, map[byte]any{paramOperationCode: int16(81),
		0: []string{offerJSON, requestJSON, "坏的 JSON"}})
	if reqs != 2 || decoded != 2 {
		t.Fatalf("请求 %d 次、解出 %d 条", reqs, decoded)
	}
}

// 空结果页(筛选没货、翻过最后一页)也是明文响应:要结掉窗口,
// 同窗口里那条无关密文不能把明文号判成加密
func TestCrypto_空结果加无关密文不判加密(t *testing.T) {
	cases := map[string]map[byte]any{
		"带253没有params0": {paramOperationCode: int16(81)},
		"带253空数组":       {paramOperationCode: int16(82), 0: []string{}},
		"无253空数组":       {0: []any{}},
	}
	for name, resp := range cases {
		c := &fakeClock{time.Unix(1000, 0)}
		tr := newTracker(c)
		p := New()
		p.OnMarketRequest = tr.MarketRequest
		p.OnMarketResponded = tr.MarketResponded
		p.OnMarketDecoded = tr.MarketDecoded

		p.HandleRequest(1, map[byte]any{paramOperationCode: int16(81)})
		c.add(200 * time.Millisecond)
		p.HandleResponse(1, resp)
		c.add(100 * time.Millisecond)
		tr.Encrypted(encResponse) // 无关的加密响应
		tr.Encrypted(0)           // 整包加密也算可能是响应
		c.add(DefaultCryptoWindow + time.Second)
		st := tr.Snapshot()
		if st.State != CryptoUnknown {
			t.Errorf("%s:空结果是明文响应,不该判成 %s", name, st.State)
		}
		if st.OtherEncrypted != 2 {
			t.Errorf("%s:那两条密文应该算「其他」,得到 %d", name, st.OtherEncrypted)
		}
	}
}

// 空结果只结窗口,不置明文:置明文仍然只看解出 ≥1 条
func TestCrypto_空结果不置明文(t *testing.T) {
	c := &fakeClock{time.Unix(1000, 0)}
	tr := newTracker(c)
	tr.MarketRequest()
	tr.MarketResponded()
	if tr.Snapshot().State != CryptoUnknown {
		t.Fatal("空结果不是明文的正向证据")
	}
	// 结了窗口之后再来的密文是「其他」,不会把状态翻成加密
	tr.Encrypted(encResponse)
	c.add(DefaultCryptoWindow + time.Second)
	if st := tr.Snapshot(); st.State != CryptoUnknown || st.OtherEncrypted != 1 {
		t.Fatalf("得到 %+v", st)
	}
}