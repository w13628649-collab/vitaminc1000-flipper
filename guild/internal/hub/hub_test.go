package hub

import (
	"context"
	"encoding/json"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"albion-guild/internal/model"
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
				At: time.Now().Add(time.Duration(i) * time.Millisecond),
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

// 丢过消息的连接要能被写循环认出来,排空后补发 resync。没丢过的不该有这个信号:
// 否则界面每一轮都整体回拉一次
func TestFanout_丢过消息才标记Lagged(t *testing.T) {
	h := NewHub()
	c := NewClient(3)
	keys := []string{"a", "b", "c", "d", "e"}
	c.Subscribe(keys)
	h.Add(c)

	h.Fanout([]model.Quote{{Key: "a", Price: 1, At: time.Now()}})
	select {
	case <-c.Lagged():
		t.Fatal("没丢过消息,不该标记")
	default:
	}
	<-c.Out()

	now := time.Now().Add(time.Second)
	batch := make([]model.Quote, 0, len(keys))
	for i, k := range keys {
		batch = append(batch, model.Quote{Key: k, Price: int64(10 + i), At: now})
	}
	h.Fanout(batch) // 5 条塞进容量 3 的积压:丢 2 条
	if h.DroppedCount() != 2 {
		t.Fatalf("应丢 2 条,得到 %d", h.DroppedCount())
	}
	select {
	case <-c.Lagged():
	default:
		t.Fatal("丢过消息应标记 Lagged")
	}
	// 多次丢弃合成一个信号,读走就清掉
	select {
	case <-c.Lagged():
		t.Fatal("读走之后不该还有")
	default:
	}
}

// 主题消息被丢也算:resync 之后界面重订 scan,拿到补发的最近一条
func TestPublishTopic_丢了也标记Lagged(t *testing.T) {
	h := NewHub()
	c := NewClient(1)
	h.Add(c)
	h.SubscribeTopics(c, []string{TopicScan})
	h.PublishTopic(TopicScan, []byte("1"))
	h.PublishTopic(TopicScan, []byte("2")) // 积压满,丢
	select {
	case <-c.Lagged():
	default:
		t.Fatal("扫描通知被丢也应标记 Lagged")
	}
}

func TestRemove_摘掉后写循环收到退出信号(t *testing.T) {
	h := NewHub()
	c := NewClient(4)
	c.Subscribe([]string{"k"})
	h.Add(c)
	h.Remove(c)

	if h.ClientCount() != 0 {
		t.Fatal("摘掉后计数应归零")
	}
	// 退出信号走 Closed(),**不是**关掉 send。
	// 关 send 的话 Fanout 那边会往已关闭的通道发送而 panic,见下一个用例
	select {
	case <-c.Closed():
	default:
		t.Fatal("摘掉后 Closed() 应该已经关闭")
	}
}

// 这是能把整个服务端打挂的那条:Fanout 在**锁外**往 send 发,
// 同一时刻另一个 goroutine 正好把这个客户端摘掉。
// 以前 Remove 会 close(send),于是向已关闭通道发送 → panic,
// 而 select 加 default 根本挡不住(向已关闭通道发送必 panic,不看缓冲)。
//
// 要让这个用例真的有牙,两点必须做对:
//   - 缓冲**不能**填满。满了就走 default 分支,压根执行不到发送
//   - 解锁和发送之间要有活干。Fanout 拿完客户端快照就解锁,
//     然后才序列化;报文越多,Remove 越容易挤进这个窗口
func TestFanout_与Remove并发不会panic(t *testing.T) {
	const keys = 400
	keyList := make([]string, keys)
	for i := range keyList {
		keyList[i] = "k" + strconv.Itoa(i)
	}

	for round := range 60 {
		h := NewHub()
		c := NewClient(keys * 2) // 留足缓冲,确保走发送而不是 default
		c.Subscribe(keyList)
		h.Add(c)

		quotes := make([]model.Quote, keys)
		for i := range quotes {
			quotes[i] = model.Quote{Key: keyList[i], Price: int64(round*1000 + i), At: time.Now()}
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); h.Fanout(quotes) }()
		go func() { defer wg.Done(); runtime.Gosched(); h.Remove(c) }()
		wg.Wait()
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
