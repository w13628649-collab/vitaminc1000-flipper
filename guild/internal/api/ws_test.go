package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func newWSEnv(t *testing.T) *wsEnv {
	t.Helper()
	h := hub.NewHub()
	srv := httptest.NewServer(New(nil, nil, h, time.Minute, nil).Routes())
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
