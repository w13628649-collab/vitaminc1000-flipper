package hub

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"albion-guild/internal/model"
)

func recv(c *Client) ([]byte, bool) {
	select {
	case msg := <-c.Out():
		return msg, true
	default:
		return nil, false
	}
}

// 只有订了 scan 主题的连接收得到;只订盘口 key 的老客户端永远收不到
func TestPublishTopic_只发给订阅了主题的连接(t *testing.T) {
	h := NewHub()
	topic, keysOnly, both, none := NewClient(8), NewClient(8), NewClient(8), NewClient(8)
	for _, c := range []*Client{topic, keysOnly, both, none} {
		h.Add(c)
	}
	if got := h.SubscribeTopics(topic, []string{TopicScan}); len(got) != 1 {
		t.Fatalf("scan 应订上,得到 %v", got)
	}
	keysOnly.Subscribe([]string{"k"})
	both.Subscribe([]string{"k"})
	h.SubscribeTopics(both, []string{TopicScan})

	if n := h.PublishTopic(TopicScan, []byte(`{"type":"scan","digest":"a"}`)); n != 2 {
		t.Fatalf("应推给 2 个订阅者,得到 %d", n)
	}
	for _, c := range []*Client{topic, both} {
		if msg, ok := recv(c); !ok || string(msg) != `{"type":"scan","digest":"a"}` {
			t.Fatalf("订阅者应收到原样的通知,得到 %q / %v", msg, ok)
		}
	}
	for _, c := range []*Client{keysOnly, none} {
		if msg, ok := recv(c); ok {
			t.Fatalf("没订 scan 的连接不该收到: %s", msg)
		}
	}

	// 反过来:报价只看 key,只订了主题的连接收不到报价
	h.Fanout([]model.Quote{{Key: "k", Price: 1, At: time.Now()}})
	if _, ok := recv(topic); ok {
		t.Fatal("只订主题的连接不该收到报价")
	}
	for _, c := range []*Client{keysOnly, both} {
		if _, ok := recv(c); !ok {
			t.Fatal("订了 key 的连接照常收报价")
		}
	}
	if h.TopicSubscribers(TopicScan) != 2 {
		t.Fatalf("订阅数应为 2,得到 %d", h.TopicSubscribers(TopicScan))
	}
}

// 之后才订阅的连接一订阅就先拿到最近一条;重复订阅再给一次
func TestSubscribeTopics_补发最近一条(t *testing.T) {
	h := NewHub()
	early := NewClient(8)
	h.Add(early)
	h.SubscribeTopics(early, []string{TopicScan}) // 还没发布过:什么都不补
	if _, ok := recv(early); ok {
		t.Fatal("没发布过时不该补发")
	}
	h.PublishTopic(TopicScan, []byte("1"))
	h.PublishTopic(TopicScan, []byte("2"))
	<-early.Out()
	<-early.Out()

	late := NewClient(8)
	h.Add(late)
	h.SubscribeTopics(late, []string{TopicScan})
	if msg, ok := recv(late); !ok || string(msg) != "2" {
		t.Fatalf("应补发最近那一条 2,得到 %q / %v", msg, ok)
	}
	if _, ok := recv(late); ok {
		t.Fatal("只补最近一条,不补历史")
	}
	h.SubscribeTopics(late, []string{TopicScan})
	if msg, ok := recv(late); !ok || string(msg) != "2" {
		t.Fatalf("重复订阅当作'再给一次当前状态',得到 %q / %v", msg, ok)
	}
}

func TestTopics_不认识的主题忽略_取消后不再收到(t *testing.T) {
	h := NewHub()
	c := NewClient(8)
	h.Add(c)
	if got := h.SubscribeTopics(c, []string{"quotes", "bogus", ""}); got != nil {
		t.Fatalf("不认识的主题不该订上,得到 %v", got)
	}
	if n := h.PublishTopic("bogus", []byte("x")); n != 0 {
		t.Fatal("不认识的主题不该发布")
	}
	h.SubscribeTopics(c, []string{"bogus", TopicScan})
	c.UnsubscribeTopics([]string{TopicScan, "never"})
	h.PublishTopic(TopicScan, []byte("x"))
	if msg, ok := recv(c); ok {
		t.Fatalf("取消订阅后不该收到: %s", msg)
	}
	if h.TopicSubscribers(TopicScan) != 0 {
		t.Fatal("取消后订阅数应为 0")
	}
}

// 背压和报价同一套:发不进去就丢、计数,绝不阻塞发布方(flip 是在锁里调的)
func TestPublishTopic_积压时丢弃而不阻塞(t *testing.T) {
	h := NewHub()
	slow := NewClient(1)
	h.Add(slow)
	h.SubscribeTopics(slow, []string{TopicScan})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			h.PublishTopic(TopicScan, []byte("x"))
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("发布被慢连接卡住了")
	}
	if got := h.DroppedCount(); got != 4 {
		t.Fatalf("缓冲 1、发 5 条,应丢 4 条,得到 %d", got)
	}

	// 报价塞满的连接上,主题消息和报价是同样的待遇:照样计数丢弃,不插队也不阻塞
	h.Remove(slow)
	busy := NewClient(2)
	busy.Subscribe([]string{"a", "b"})
	h.Add(busy)
	h.SubscribeTopics(busy, []string{TopicScan}) // 补发一条最近的,占掉一格
	h.Fanout([]model.Quote{{Key: "a", Price: 1, At: time.Now()}, {Key: "b", Price: 1, At: time.Now()}})
	before := h.DroppedCount()
	if n := h.PublishTopic(TopicScan, []byte("y")); n != 0 || h.DroppedCount() != before+1 {
		t.Fatalf("满了应丢这一条并计数,得到 推 %d / 丢 %d", n, h.DroppedCount()-before)
	}
}

// 摘掉的连接不再收;和 Remove、Fanout 并发不 panic(在 WSL 里 -race 跑)
func TestPublishTopic_与Remove和订阅并发(t *testing.T) {
	for round := 0; round < 50; round++ {
		h := NewHub()
		cs := make([]*Client, 8)
		for i := range cs {
			cs[i] = NewClient(64)
			h.Add(cs[i])
		}
		var wg sync.WaitGroup
		for i := range cs {
			wg.Add(3)
			c := cs[i]
			go func() { defer wg.Done(); h.SubscribeTopics(c, []string{TopicScan}) }()
			go func() { defer wg.Done(); runtime.Gosched(); h.Remove(c) }()
			go func() { defer wg.Done(); h.PublishTopic(TopicScan, []byte("z")) }()
		}
		wg.Wait()
		if h.ClientCount() != 0 || h.TopicSubscribers(TopicScan) != 0 {
			t.Fatal("全部摘掉后不该还有订阅者")
		}
	}
}
