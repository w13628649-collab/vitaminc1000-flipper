package protocol

import (
	"sync"
	"time"
)

// MarketCrypto 是"这个号的市场挂单是不是明文"的判断。
//
// 明文不是协议属性,是服务端对账号的当前取值(仓库 CLAUDE.md 待办第 5 条):
// 2024-01 SBI 对所有账号加密过市场数据,后来只放行了一部分,规则连 AODP 都不知道。
// 这条不成立,整条被动抓包路线归零,所以界面上要能一眼看出来。
type MarketCrypto string

const (
	CryptoUnknown   MarketCrypto = "unknown"
	CryptoClear     MarketCrypto = "clear"
	CryptoEncrypted MarketCrypto = "encrypted"
)

// Text 是给成员看的那句话。界面原样显示,文案只在这里改。
func (c MarketCrypto) Text() string {
	switch c {
	case CryptoClear:
		return "市场数据:明文"
	case CryptoEncrypted:
		return "市场数据:被加密,这个号的挂单传不出来"
	}
	return "市场数据:未确认(去市场刷一次)"
}

// DefaultCryptoWindow 是发出市场请求后等响应的时长。响应正常一两百毫秒就回来,
// 10 秒是给卡顿和分片重组留的余量;再长的话,窗口里混进无关加密消息的机会就大了
const DefaultCryptoWindow = 10 * time.Second

// CryptoStatus 是 CryptoTracker 的一次快照。
type CryptoStatus struct {
	State MarketCrypto
	// Since 是最后一次变化的时刻;一直是 unknown 时为零值
	Since time.Time
	// OtherEncrypted 是和市场请求无关的加密消息条数。它们不改市场状态:
	// 加密按消息粒度,地图、事件被加密不说明挂单被加密(docs/client-data.md 第二点七节)
	OtherEncrypted int64
}

// CryptoTracker 判断市场挂单是明文还是密文。语义照 AODP 客户端:
//
//   - **只有** 81/82 响应成功解出 ≥1 条挂单才置「明文」——正向信号只此一个
//   - 看到 81/82 **请求**之后 Window 内收到加密的响应、且这段时间里没有认出任何
//     明文的 81/82 响应(空结果也算)→ 置「加密」。判定在窗口结束时做,不是见到密文当场翻:
//     窗口里先来一条无关的密文、随后挂单明文到了,那条密文就是无关的
//   - 和市场请求无关的加密消息只计数,不改市场状态
//
// 以前客户端对任何加密包都喊"市场数据被加密",会误诊。
//
// 纯逻辑,时钟由 Now 注入;窗口到期的判定挂在每次调用上,另外靠 Tick 定时推一下
// (不然一个人发完请求就不动了,状态永远翻不过去)。
type CryptoTracker struct {
	Window time.Duration
	Now    func() time.Time
	// OnChange 在状态变化时回调,在锁外调
	OnChange func(from, to MarketCrypto)

	mu    sync.Mutex
	state MarketCrypto
	since time.Time
	other int64

	awaiting bool      // 发过市场请求、窗口还没结案
	reqAt    time.Time // 最近一次市场请求
	inWindow int64     // 窗口里收到的、可能是挂单响应的密文条数

	now     time.Time      // 这次调用的时刻,setLocked 记变化时间用
	changes []cryptoChange // 这次调用里发生的变化,出锁后回调
}

func NewCryptoTracker() *CryptoTracker {
	return &CryptoTracker{Window: DefaultCryptoWindow, Now: time.Now, state: CryptoUnknown}
}

type cryptoChange struct{ from, to MarketCrypto }

// MarketRequest 记一次 81/82 请求。连着翻页时窗口从最后一次请求起算。
func (t *CryptoTracker) MarketRequest() {
	t.do(func(now time.Time) {
		if !t.awaiting {
			t.awaiting, t.inWindow = true, 0
		}
		t.reqAt = now
	})
}

// MarketResponded 记一条认出来的明文 81/82 响应,不管里面有没有挂单。
//
// 它结掉窗口但**不改状态**:空结果页(筛选没货、翻过最后一页)很常见,
// 它说明这条响应不是密文,窗口里那几条密文就不是挂单响应,算回"其他"。
// 以前只有解出 ≥1 条才结窗口,明文号翻到空页时同窗口来一条无关密文,
// 到期就被判成加密——这个判断决定整条抓包路线还走不走,误诊代价大。
// 置「明文」仍然只看 MarketDecoded(n ≥ 1)
func (t *CryptoTracker) MarketResponded() {
	t.do(func(time.Time) {
		t.other += t.inWindow
		t.awaiting, t.inWindow = false, 0
	})
}

// MarketDecoded 记一次 81/82 响应解出了 n 条挂单(JSON 能解开就算,
// 不管位置知不知道)。n ≥ 1 才是明文的证据。
func (t *CryptoTracker) MarketDecoded(n int) {
	if n < 1 {
		return
	}
	t.do(func(time.Time) {
		// 窗口里那几条密文不是挂单响应,算回"其他"
		t.other += t.inWindow
		t.awaiting, t.inWindow = false, 0
		t.setLocked(CryptoClear)
	})
}

// Encrypted 记一条密文。msgType 是 photon 报上来的 0x80|kind,整包加密时是 0。
func (t *CryptoTracker) Encrypted(msgType byte) {
	t.do(func(now time.Time) {
		if t.awaiting && now.Sub(t.reqAt) <= t.window() && maybeResponse(msgType) {
			t.inWindow++
			return
		}
		t.other++
	})
}

// Tick 只做窗口到期的判定。
func (t *CryptoTracker) Tick() { t.do(func(time.Time) {}) }

// Snapshot 返回当前状态(先结掉到期的窗口)。
func (t *CryptoTracker) Snapshot() CryptoStatus {
	var st CryptoStatus
	t.do(func(time.Time) {
		st = CryptoStatus{State: t.state, Since: t.since, OtherEncrypted: t.other}
	})
	return st
}

// do 在锁里先结掉到期的窗口再执行 f,状态变化的回调放到锁外。
func (t *CryptoTracker) do(f func(now time.Time)) {
	now := t.clock()
	t.mu.Lock()
	if t.state == "" {
		t.state = CryptoUnknown
	}
	t.now = now
	t.resolveLocked(now)
	f(now)
	// 一次调用里可能翻两次(窗口到期判了加密,紧接着这条就是成功的挂单响应),
	// 两次都要报,不能只比首尾
	changes := t.changes
	t.changes = nil
	cb := t.OnChange
	t.mu.Unlock()
	if cb != nil {
		for _, c := range changes {
			cb(c.from, c.to)
		}
	}
}

// resolveLocked 结掉已经过期的窗口:窗口里有可能是挂单响应的密文、
// 又没等到成功的挂单响应 → 加密。窗口里什么都没有就不下结论(响应可能丢了)。
func (t *CryptoTracker) resolveLocked(now time.Time) {
	if !t.awaiting || now.Sub(t.reqAt) <= t.window() {
		return
	}
	if t.inWindow > 0 {
		t.setLocked(CryptoEncrypted)
	}
	t.awaiting, t.inWindow = false, 0
}

func (t *CryptoTracker) setLocked(s MarketCrypto) {
	if s == t.state {
		return
	}
	t.changes = append(t.changes, cryptoChange{t.state, s})
	t.state, t.since = s, t.now
}

func (t *CryptoTracker) window() time.Duration {
	if t.Window > 0 {
		return t.Window
	}
	return DefaultCryptoWindow
}

func (t *CryptoTracker) clock() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// maybeResponse 判断一条密文有没有可能是挂单响应。挂单是操作响应,
// Photon 的响应类型是 3(有的版本用 7),加密后是 0x83 / 0x87;
// 整包加密(0)看不出类型,只能算可能。加密的事件、请求不可能是挂单响应
func maybeResponse(msgType byte) bool {
	if msgType == 0 {
		return true
	}
	kind := msgType &^ 0x80
	return kind == 3 || kind == 7
}
