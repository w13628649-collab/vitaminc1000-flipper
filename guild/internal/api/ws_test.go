package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"albion-guild/internal/hub"
	"albion-guild/internal/model"
)

// wsEnv 起一个只有行情中转的服务端(不接库、不接倒爷那组接口)。
type wsEnv struct {
	t   *testing.T
	h   *hub.Hub
	srv *httptest.Server
	at  time.Time // 报价时间戳,每推一条往前走,免得被 Fanout 的乱序保护吞掉
}

func newWSEnv(t *testing.T) *wsEnv { return newWSEnvBuf(t, 0) }

// newWSEnvBuf 同 newWSEnv,只是把每条连接的发送积压设成 buf(0 = 线上默认值)。
func newWSEnvBuf(t *testing.T, buf int) *wsEnv {
	t.Helper()
	h := hub.NewHub()
	s := New(nil, nil, h, time.Minute, nil)
	s.WSBuffer = buf
	srv := httptest.NewServer(s.Routes())
	t.Cleanup(srv.Close)
	return &wsEnv{t: t, h: h, srv: srv, at: time.Now()}
}

func (e *wsEnv) dial() *websocket.Conn {
	e.t.Helper()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(e.srv.URL, "http")+"/ws", nil)
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = c.Close() })
	return c
}

func (e *wsEnv) send(c *websocket.Conn, cmd string) {
	e.t.Helper()
	if err := c.WriteMessage(websocket.TextMessage, []byte(cmd)); err != nil {
		e.t.Fatal(err)
	}
}

// read 读下一条消息,解成 map。超时算失败。
func (e *wsEnv) read(c *websocket.Conn) map[string]any {
	e.t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := c.ReadMessage()
	if err != nil {
		e.t.Fatalf("没等到消息: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(msg, &m); err != nil {
		e.t.Fatalf("消息不是 JSON 对象: %s", msg)
	}
	return m
}

func (e *wsEnv) quote(key string) {
	e.at = e.at.Add(time.Millisecond)
	e.h.Fanout([]model.Quote{{Key: key, Price: 1000, At: e.at}})
}

// waitQuote 反复推同一个 key 的报价,直到这条连接收到——sub 指令没有回执,
// 只能这样确认服务端已经处理完了。返回收到的那条
func (e *wsEnv) waitQuote(c *websocket.Conn, key string) map[string]any {
	e.t.Helper()
	got := make(chan map[string]any, 1)
	go func() {
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, msg, err := c.ReadMessage()
		if err != nil {
			got <- nil
			return
		}
		var m map[string]any
		_ = json.Unmarshal(msg, &m)
		got <- m
	}()
	deadline := time.After(3 * time.Second)
	for {
		e.quote(key)
		select {
		case m := <-got:
			if m == nil {
				e.t.Fatal("连接断了")
			}
			return m
		case <-deadline:
			e.t.Fatal("订阅 3 秒内没生效")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func (e *wsEnv) waitSubscribers(n int) {
	e.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for e.h.TopicSubscribers(hub.TopicScan) != n {
		if time.Now().After(deadline) {
			e.t.Fatalf("scan 订阅数 3 秒内没到 %d,现在 %d", n, e.h.TopicSubscribers(hub.TopicScan))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// untilSentinel 推一条哨兵报价,然后一直读到它为止(顺带读掉 waitQuote 期间多推出来的
// 报价)。同一条连接上消息是按序的:中途读到带 type 的消息就返回它,没读到返回 nil
func (e *wsEnv) untilSentinel(c *websocket.Conn, sentinel string) (typed map[string]any) {
	e.t.Helper()
	e.quote(sentinel)
	for {
		m := e.read(c)
		if _, ok := m["type"]; ok {
			return m
		}
		if m["k"] == sentinel {
			return nil
		}
	}
}

const scanMsg = `{"type":"scan","evaluated_at":"2026-09-25T10:00:00Z","started_at":"2026-09-25T09:30:00Z",` +
	`"digest":"abc","opportunities":3,"routes":2,"full":false}`

// 老客户端只发 keys:报价照收,扫描通知永远收不到
func TestWS_老客户端只订key收不到扫描通知(t *testing.T) {
	e := newWSEnv(t)
	c := e.dial()
	e.send(c, `{"op":"sub","keys":["k1","sentinel"]}`)
	q := e.waitQuote(c, "k1")
	if _, typed := q["type"]; typed || q["k"] != "k1" || q["p"] == nil {
		t.Fatalf("报价消息形状应保持 {k,p,d,n,t}、没有 type,得到 %v", q)
	}

	e.h.PublishTopic(hub.TopicScan, []byte(scanMsg))
	if m := e.untilSentinel(c, "sentinel"); m != nil {
		t.Fatalf("没订 scan 的连接收到了扫描通知: %v", m)
	}
}

// keys 和 topics 写在同一条 sub 里:两样都生效;通知原样到达
func TestWS_同一条指令订key和scan(t *testing.T) {
	e := newWSEnv(t)
	c := e.dial()
	e.send(c, `{"op":"sub","keys":["k1","sentinel1","sentinel2"],"topics":["scan"]}`)
	e.waitSubscribers(1)
	e.waitQuote(c, "k1")

	e.h.PublishTopic(hub.TopicScan, []byte(scanMsg))
	m := e.untilSentinel(c, "sentinel1")
	if m == nil || m["type"] != "scan" || m["digest"] != "abc" || m["full"] != false || m["routes"] != float64(2) {
		t.Fatalf("应收到原样的扫描通知,得到 %v", m)
	}

	// 取消 scan,key 的订阅不受影响
	e.send(c, `{"op":"unsub","topics":["scan"]}`)
	e.waitSubscribers(0)
	e.h.PublishTopic(hub.TopicScan, []byte(scanMsg))
	// 换一个哨兵:上一个哨兵还躺在连接里没读
	if m := e.untilSentinel(c, "sentinel2"); m != nil {
		t.Fatalf("取消 scan 之后不该再收到: %v", m)
	}
}

// 已经发布过的话,订阅时立刻补发最近一条:重连的界面不用等下一轮
func TestWS_订阅scan时补发最近一条(t *testing.T) {
	e := newWSEnv(t)
	e.h.PublishTopic(hub.TopicScan, []byte(scanMsg))
	c := e.dial()
	e.send(c, `{"op":"sub","topics":["scan"]}`)
	if m := e.read(c); m["type"] != "scan" || m["digest"] != "abc" {
		t.Fatalf("应立刻补到最近一条,得到 %v", m)
	}
}

// 不认识的主题不订、不报错,连接照常可用
func TestWS_不认识的主题忽略(t *testing.T) {
	e := newWSEnv(t)
	c := e.dial()
	e.send(c, `{"op":"sub","keys":["sentinel"],"topics":["bogus"]}`)
	e.waitQuote(c, "sentinel")
	if n := e.h.TopicSubscribers(hub.TopicScan); n != 0 {
		t.Fatalf("不该订上任何主题,得到 %d", n)
	}

	resp, err := http.Get(e.srv.URL + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var stats map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	ts, ok := stats["topic_subscribers"].(map[string]any)
	if !ok || ts["scan"] != float64(0) || stats["clients"] != float64(1) {
		t.Fatalf("/api/stats 应报 scan 订阅数,得到 %v", stats)
	}
}

// subscribeN 订 n 个 key(k0..k{n-1})加一个哨兵,等订阅生效。waitQuote 期间可能多推出
// 几条哨兵报价还躺在连接里,调用方读的时候按 key 过滤
func (e *wsEnv) subscribeN(c *websocket.Conn, n int) []string {
	e.t.Helper()
	keys := make([]string, n)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}
	b, _ := json.Marshal(map[string]any{"op": "sub", "keys": append(append([]string(nil), keys...), "sentinel")})
	e.send(c, string(b))
	e.waitQuote(c, "sentinel")
	return keys
}

// 验收原样:实时页订了 280 个盘口边,一次上传翻过全部,一轮 Fanout 280 条。
// 以前每条连接只能积压 256 条,多出来的 24 条悄悄丢掉。现在一条不丢
func TestWS_一轮几百条报价不丢(t *testing.T) {
	e := newWSEnv(t)
	c := e.dial()
	keys := e.subscribeN(c, 280)
	before := e.h.DroppedCount()

	e.at = e.at.Add(time.Second)
	batch := make([]model.Quote, len(keys))
	for i, k := range keys {
		batch[i] = model.Quote{Key: k, Price: int64(2000 + i), At: e.at}
	}
	e.h.Fanout(batch)

	want := map[any]bool{}
	for _, k := range keys {
		want[k] = true
	}
	got := map[any]bool{}
	for len(got) < len(keys) {
		m := e.read(c)
		if _, typed := m["type"]; typed {
			t.Fatalf("没丢消息不该收到带 type 的消息: %v", m)
		}
		if want[m["k"]] { // 跳过订阅时多推出来的哨兵
			got[m["k"]] = true
		}
	}
	if d := e.h.DroppedCount() - before; d != 0 {
		t.Fatalf("280 条不该丢,丢了 %d", d)
	}
}

// 真丢了(积压上限调到 1 造出来):排空之后补发一条 {"type":"resync"},界面据此整体回拉
func TestWS_丢过消息后补发resync(t *testing.T) {
	e := newWSEnvBuf(t, 1)
	c := e.dial()
	keys := e.subscribeN(c, 200)

	// 一次 Fanout 200 条塞进容量 1 的积压:写循环再快也追不上,几乎必丢。
	// 没丢就再来一轮(至多 20 轮),断言只在真丢过之后做
	before := e.h.DroppedCount()
	for round := 0; round < 20 && e.h.DroppedCount() == before; round++ {
		e.at = e.at.Add(time.Second)
		batch := make([]model.Quote, len(keys))
		for i, k := range keys {
			batch[i] = model.Quote{Key: k, Price: int64(round*1000 + i), At: e.at}
		}
		e.h.Fanout(batch)
	}
	if e.h.DroppedCount() == before {
		t.Skip("20 轮都没丢,造不出积压")
	}
	// 同一轮里写循环读走信号之后 Fanout 可能又丢几条,resync 可能补不止一次,这里只要求至少一次
	for {
		m := e.read(c) // 3 秒读不到就失败
		if m["type"] == "resync" {
			break
		}
		if _, typed := m["type"]; typed {
			t.Fatalf("只该有 resync 这一种带 type 的消息,得到 %v", m)
		}
	}
}
