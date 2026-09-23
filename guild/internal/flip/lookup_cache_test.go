package flip

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newTestCache(ttl time.Duration) (*ttlCache[int], *fakeClock) {
	clk := &fakeClock{t: t0}
	c := newTTLCache[int](ttl, time.Minute)
	c.now = clk.now
	return c, clk
}

func TestTTLCacheHitAndExpire(t *testing.T) {
	c, clk := newTestCache(time.Minute)
	var calls atomic.Int32
	fetch := func(context.Context) (int, error) { return int(calls.Add(1)), nil }
	ctx := context.Background()

	v, cached, err := c.get(ctx, "k", fetch)
	if err != nil || v != 1 || cached {
		t.Fatalf("首次: v=%d cached=%v err=%v", v, cached, err)
	}
	clk.add(30 * time.Second)
	v, cached, _ = c.get(ctx, "k", fetch)
	if v != 1 || !cached {
		t.Fatalf("60 秒内应命中: v=%d cached=%v", v, cached)
	}
	clk.add(31 * time.Second)
	v, cached, _ = c.get(ctx, "k", fetch)
	if v != 2 || cached {
		t.Fatalf("过期应重拉: v=%d cached=%v", v, cached)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("fetch 调了 %d 次", n)
	}
}

func TestTTLCacheErrorsAreNotCached(t *testing.T) {
	c, _ := newTestCache(time.Minute)
	var calls atomic.Int32
	fetch := func(context.Context) (int, error) {
		if calls.Add(1) == 1 {
			return 0, errors.New("502")
		}
		return 7, nil
	}
	if _, _, err := c.get(context.Background(), "k", fetch); err == nil {
		t.Fatal("第一次应失败")
	}
	v, _, err := c.get(context.Background(), "k", fetch)
	if err != nil || v != 7 {
		t.Fatalf("失败不该被缓存: v=%d err=%v", v, err)
	}
}

// 两个人同时点开同一件东西只发一次请求
func TestTTLCacheSharesInflight(t *testing.T) {
	c, _ := newTestCache(time.Minute)
	release := make(chan struct{})
	var calls atomic.Int32
	fetch := func(context.Context) (int, error) {
		calls.Add(1)
		<-release
		return 42, nil
	}
	var wg sync.WaitGroup
	results := make([]int, 5)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, _, _ := c.get(context.Background(), "k", fetch)
			results[i] = v
		}(i)
	}
	// 等所有调用方都挂上去再放行
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("fetch 调了 %d 次,应合并成 1 次", n)
	}
	for _, v := range results {
		if v != 42 {
			t.Fatalf("results=%v", results)
		}
	}
}

// 等不及就先走,请求留在后台跑完进缓存 —— 限流器排队时 handler 不能陪着挂
func TestTTLCacheWaitTimesOutButFetchCompletes(t *testing.T) {
	c, _ := newTestCache(time.Minute)
	release := make(chan struct{})
	finished := make(chan struct{})
	fetch := func(ctx context.Context) (int, error) {
		defer close(finished)
		select {
		case <-release:
			return 9, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, _, err := c.get(ctx, "k", fetch); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应超时返回,得到 %v", err)
	}
	close(release)
	<-finished
	// flight 在 fetch 返回之后才落结果,给它一点时间
	var v int
	var cached bool
	for i := 0; i < 100; i++ {
		v, cached, _ = c.get(context.Background(), "k", func(context.Context) (int, error) {
			return -1, nil
		})
		if cached {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if v != 9 || !cached {
		t.Fatalf("后台结果应进缓存: v=%d cached=%v", v, cached)
	}
}
