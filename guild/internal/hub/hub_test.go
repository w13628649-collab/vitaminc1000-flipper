package hub

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ludy/albion-guild/internal/model"
)

func key(item string, side model.Side) model.QuoteKey {
	return model.QuoteKey{ItemID: item, LocationID: "Martlock", Quality: 1, Side: side}
}

// ── conflation ────────────────────────────────────────────

type fakeSource struct {
	mu    sync.Mutex
	calls [][]model.QuoteKey
	out   []model.Quote
}

func (f *fakeSource) BestQuotes(_ context.Context, keys []model.QuoteKey, _ time.Duration) ([]model.Quote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, keys)
	return f.out, nil
}

type fakeBroadcaster struct {
	mu    sync.Mutex
	batch [][]model.Quote
}

func (f *fakeBroadcaster) Publish(_ context.Context, q []model.Quote) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batch = append(f.batch, q)
	return nil
}

func TestConflator_同一盘口一个tick内只算一次(t *testing.T) {
	// 热门物品一秒能变几十次。不合并的话客户端会被淹,
	// 而中间那些值用户根本看不见。
	src := &fakeSource{out: []model.Quote{{Key: "x", Price: 100, At: time.Now()}}}
	bc := &fakeBroadcaster{}
	c := NewConflator(src, bc, time.Minute)

	for range 50 {
		c.MarkDirty(key("T5_METALBAR", model.SideOffer))
	}
	c.MarkDirty(key("T5_CLOTH", model.SideOffer))

	c.tick(context.Background())

	if len(src.calls) != 1 {
		t.Fatalf("应该只查一次库,查了 %d 次", len(src.calls))
	}
	if got := len(src.calls[0]); got != 2 {
		t.Fatalf("51 次标记应合并成 2 个盘口,得到 %d", got)
	}
}

func TestConflator_没有脏盘口就不打扰数据库(t *testing.T) {
	src := &fakeSource{}
	c := NewConflator(src, &fakeBroadcaster{}, time.Minute)
	c.tick(context.Background())
	if len(src.calls) != 0 {
		t.Fatal("空 tick 不该查库")
	}
}

func TestConflator_取走之后脏集合被清空(t *testing.T) {
	src := &fakeSource{out: []model.Quote{{Key: "x", At: time.Now()}}}
	c := NewConflator(src, &fakeBroadcaster{}, time.Minute)
	c.MarkDirty(key("T5_METALBAR", model.SideOffer))
	c.tick(context.Background())
	c.tick(context.Background())
	if len(src.calls) != 1 {
		t.Fatalf("第二个 tick 不该再查,共查了 %d 次", len(src.calls))
	}
}

// ── 扇出 ──────────────────────────────────────────────────

func TestFanout_只发给订阅了的客户端(t *testing.T) {
	h := NewHub()
	sub, other := NewClient(8), NewClient(8)
	sub.Subscribe([]string{"T5_METALBAR|Martlock|1|0"})
	other.Subscribe([]string{"T5_CLOTH|Martlock|1|0"})
	h.Add(sub)
	h.Add(other)

	h.Fanout([]model.Quote{{Key: "T5_METALBAR|Martlock|1|0", Price: 1120, At: time.Now()}})

	select {
	case msg := <-sub.Out():
		var q model.Quote
		if err := json.Unmarshal(msg, &q); err != nil || q.Price != 1120 {
			t.Fatalf("订阅者收到的内容不对: %s", msg)
		}
	default:
		t.Fatal("订阅者没收到")
	}
	select {
	case <-other.Out():
		t.Fatal("没订阅这个 key 的客户端不该收到")
	default:
	}
}

func TestFanout_丢弃比手上更旧的行情(t *testing.T) {
	// 多实例下晚发生的消息可能先到。不挡的话界面会停在旧值上,
	// 而且看起来"一切正常"——这种 bug 极难复现。
	h := NewHub()
	c := NewClient(8)
	c.Subscribe([]string{"k"})
	h.Add(c)

	now := time.Now()
	h.Fanout([]model.Quote{{Key: "k", Price: 200, At: now}})
	<-c.Out()

	h.Fanout([]model.Quote{{Key: "k", Price: 100, At: now.Add(-time.Second)}})
	select {
	case msg := <-c.Out():
		t.Fatalf("旧行情不该被发出去: %s", msg)
	default:
	}
}

func TestFanout_慢客户端丢消息而不是阻塞(t *testing.T) {
	// 行情跟聊天不一样:聊天丢了就没了,行情丢旧的无所谓,
	// 下个 tick 还会推最新值。绝不能让一个慢客户端卡住所有人。
	h := NewHub()
	slow := NewClient(2) // 故意开得很小
	slow.Subscribe([]string{"k"})
	h.Add(slow)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 20 {
			h.Fanout([]model.Quote{{
				Key: "k", Price: int64(i),
				At:  time.Now().Add(time.Duration(i) * time.Millisecond),
			}})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("扇出被慢客户端卡住了")
	}
	if h.Dropped == 0 {
		t.Fatal("缓冲满了应该计数丢弃,而不是悄悄阻塞")
	}
}

func TestRemove_关闭后不再收到(t *testing.T) {
	h := NewHub()
	c := NewClient(4)
	c.Subscribe([]string{"k"})
	h.Add(c)
	h.Remove(c)

	if h.ClientCount() != 0 {
		t.Fatal("摘掉后计数应归零")
	}
	if _, ok := <-c.Out(); ok {
		t.Fatal("通道应该已关闭")
	}
}

func TestUnsubscribe_取消后不再收到(t *testing.T) {
	h := NewHub()
	c := NewClient(4)
	c.Subscribe([]string{"k"})
	c.Unsubscribe([]string{"k"})
	h.Add(c)

	h.Fanout([]model.Quote{{Key: "k", Price: 1, At: time.Now()}})
	select {
	case <-c.Out():
		t.Fatal("已取消订阅还收到了")
	default:
	}
}
