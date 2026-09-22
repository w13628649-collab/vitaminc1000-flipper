// Package hub 管 WebSocket 连接、订阅关系和行情扇出。
package hub

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"albion-guild/internal/model"
)

// Broadcaster 把一批行情送到所有该收到的客户端,不管它们连在哪个实例。
//
// 单实例时就是本地函数调用;要多实例了换成 NATS 实现,
// Hub.Fanout 那一侧一行都不用改。
type Broadcaster interface {
	Publish(ctx context.Context, quotes []model.Quote) error
}

// LocalBroadcaster 直接扇给本进程的连接。
type LocalBroadcaster struct{ Hub *Hub }

func (b *LocalBroadcaster) Publish(_ context.Context, quotes []model.Quote) error {
	b.Hub.Fanout(quotes)
	return nil
}

// Client 是一条 WS 连接。
type Client struct {
	send chan []byte
	subs map[string]struct{}
	mu   sync.RWMutex
}

func NewClient(buffer int) *Client {
	return &Client{
		send: make(chan []byte, buffer),
		subs: make(map[string]struct{}),
	}
}

func (c *Client) Out() <-chan []byte { return c.send }

func (c *Client) Subscribe(keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		c.subs[k] = struct{}{}
	}
}

func (c *Client) Unsubscribe(keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, k := range keys {
		delete(c.subs, k)
	}
}

func (c *Client) subscribed(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.subs[key]
	return ok
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}

	// lastAt 挡住乱序:多实例下晚发生的消息可能先到,
	// 不挡的话界面会停在旧值上。单实例也不亏,能挡住重试导致的重复。
	lastAt map[string]time.Time

	Dropped uint64 // 因客户端积压被丢弃的消息数,接监控用
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[*Client]struct{}),
		lastAt:  make(map[string]time.Time),
	}
}

func (h *Hub) Add(c *Client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) Remove(c *Client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
	h.mu.Unlock()
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// Fanout 把行情发给订阅了对应 key 的本地连接。
func (h *Hub) Fanout(quotes []model.Quote) {
	h.mu.Lock()
	fresh := quotes[:0]
	for _, q := range quotes {
		if last, ok := h.lastAt[q.Key]; ok && q.At.Before(last) {
			continue // 比手上的还旧,扔掉
		}
		h.lastAt[q.Key] = q.At
		fresh = append(fresh, q)
	}
	clients := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()

	if len(fresh) == 0 || len(clients) == 0 {
		return
	}

	// 同一条消息只序列化一次,再分发给所有订阅者
	encoded := make(map[string][]byte, len(fresh))
	for _, q := range fresh {
		if b, err := json.Marshal(q); err == nil {
			encoded[q.Key] = b
		}
	}

	for _, c := range clients {
		for key, payload := range encoded {
			if !c.subscribed(key) {
				continue
			}
			select {
			case c.send <- payload:
			default:
				// 客户端处理不过来:丢掉这条,不阻塞扇出循环。
				// 行情丢旧的没关系,下一个 tick 还会推最新值——
				// 这跟聊天不一样,绝不能让一个慢客户端卡住所有人。
				h.mu.Lock()
				h.Dropped++
				h.mu.Unlock()
			}
		}
	}
}

// QuoteSource 让 conflator 能把脏盘口换算成最新行情。
type QuoteSource interface {
	BestQuotes(ctx context.Context, keys []model.QuoteKey, fresh time.Duration) ([]model.Quote, error)
}

// Conflator 合并同一 tick 内对同一盘口的重复变更。
//
// 热门物品一秒可能变几十次,全推会把客户端淹掉,而中间那些值用户根本看不见。
// 攒 dirty key、到点统一算一次最优价,推送量降一到两个数量级。
type Conflator struct {
	mu    sync.Mutex
	dirty map[model.QuoteKey]struct{}

	src    QuoteSource
	bc     Broadcaster
	fresh  time.Duration
	Ticked uint64
}

func NewConflator(src QuoteSource, bc Broadcaster, fresh time.Duration) *Conflator {
	return &Conflator{
		dirty: make(map[model.QuoteKey]struct{}),
		src:   src,
		bc:    bc,
		fresh: fresh,
	}
}

func (c *Conflator) MarkDirty(k model.QuoteKey) {
	c.mu.Lock()
	c.dirty[k] = struct{}{}
	c.mu.Unlock()
}

func (c *Conflator) swap() []model.QuoteKey {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.dirty) == 0 {
		return nil
	}
	keys := make([]model.QuoteKey, 0, len(c.dirty))
	for k := range c.dirty {
		keys = append(keys, k)
	}
	c.dirty = make(map[model.QuoteKey]struct{}, len(keys))
	return keys
}

func (c *Conflator) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.tick(ctx)
		}
	}
}

func (c *Conflator) tick(ctx context.Context) {
	keys := c.swap()
	if len(keys) == 0 {
		return
	}
	c.Ticked++
	quotes, err := c.src.BestQuotes(ctx, keys, c.fresh)
	if err != nil {
		slog.Error("取最优价失败", "keys", len(keys), "err", err)
		return
	}
	if len(quotes) == 0 {
		return
	}
	if err := c.bc.Publish(ctx, quotes); err != nil {
		slog.Error("广播失败", "quotes", len(quotes), "err", err)
	}
}
