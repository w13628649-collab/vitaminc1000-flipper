package flip

import (
	"context"
	"sync"
	"time"

	"albion-guild/internal/aodp"
)

// 查价页现场打 AODP 的那两路(prices / history)的缓存。
//
// 做成包级变量而不是 Service 的字段:Service 结构体在另一条后端线上改,
// 这里只加文件不碰它。
//
// 三件事一起管:
//   - **缓存**:同一个物品 60 秒内再查不再打 AODP(history 15 分钟,日线一天才变一根)
//   - **合并在飞请求**:两个人同时点开同一件东西只发一次
//   - **等待可以截断,请求本身不截断**:AODP 的限流器是阻塞等待、不看 ctx 的,
//     撞上定时扫描时可能一等几十秒。handler 只等到自己的期限就先回部分数据,
//     请求留在后台跑完、结果进缓存 —— 用户过几秒再点一次就有了
var (
	lookupPrices  = newTTLCache[[]aodp.PriceRecord](60*time.Second, 2*time.Minute)
	lookupHistory = newTTLCache[[]aodp.HistorySeries](15*time.Minute, 2*time.Minute)
)

type flight[T any] struct {
	done chan struct{}
	val  T
	err  error
	at   time.Time
}

type ttlCache[T any] struct {
	ttl time.Duration
	bg  time.Duration // 后台请求自己的期限,和发起它的 HTTP 请求脱钩
	now func() time.Time

	mu sync.Mutex
	m  map[string]*flight[T]
}

func newTTLCache[T any](ttl, bg time.Duration) *ttlCache[T] {
	return &ttlCache[T]{ttl: ttl, bg: bg, now: time.Now, m: map[string]*flight[T]{}}
}

func isDone(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// get 返回 (值, 是否直接命中缓存, 错误)。ctx 只管"我愿意等多久"。
// 失败的结果不缓存,下一次调用会重新发。
func (c *ttlCache[T]) get(ctx context.Context, key string,
	fetch func(context.Context) (T, error)) (T, bool, error) {

	c.mu.Lock()
	now := c.now()
	if f, ok := c.m[key]; ok {
		if !isDone(f.done) {
			c.mu.Unlock()
			v, err := c.wait(ctx, f)
			return v, false, err
		}
		if f.err == nil && now.Sub(f.at) < c.ttl {
			c.mu.Unlock()
			return f.val, true, nil
		}
	}
	// 顺手淘汰过期项,map 不会随查过的物品数无限长
	for k, f := range c.m {
		if isDone(f.done) && (f.err != nil || now.Sub(f.at) >= c.ttl) {
			delete(c.m, k)
		}
	}
	f := &flight[T]{done: make(chan struct{})}
	c.m[key] = f
	c.mu.Unlock()

	go func() {
		bctx, cancel := context.WithTimeout(context.Background(), c.bg)
		defer cancel()
		v, err := fetch(bctx)
		c.mu.Lock()
		f.val, f.err, f.at = v, err, c.now()
		if err != nil && c.m[key] == f {
			delete(c.m, key)
		}
		c.mu.Unlock()
		close(f.done)
	}()
	v, err := c.wait(ctx, f)
	return v, false, err
}

func (c *ttlCache[T]) wait(ctx context.Context, f *flight[T]) (T, error) {
	select {
	case <-f.done:
		return f.val, f.err
	case <-ctx.Done():
		var zero T
		return zero, ctx.Err()
	}
}
