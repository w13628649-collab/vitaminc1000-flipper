package aodp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"albion-guild/internal/conf"
)

// limiter 是双窗口令牌桶。AODP 有两条线——180/分钟 和 300/5 分钟,
// 后者等效 60/分钟,才是真正的约束。两条都满足才放行。
type limiter struct {
	mu      sync.Mutex
	windows []window
	hits    []time.Time
	// sleep 可替换,测试里不用真等
	sleep func(time.Duration)
	now   func() time.Time
}

type window struct {
	span  time.Duration
	limit int
}

func newLimiter(perMinute, per5Min int) *limiter {
	return &limiter{
		windows: []window{{time.Minute, perMinute}, {5 * time.Minute, per5Min}},
		sleep:   time.Sleep,
		now:     time.Now,
	}
}

func (l *limiter) acquire() {
	for {
		l.mu.Lock()
		now := l.now()
		longest := time.Duration(0)
		for _, w := range l.windows {
			if w.span > longest {
				longest = w.span
			}
		}
		// 只需保留最长窗口内的记录
		keep := l.hits[:0]
		for _, h := range l.hits {
			if now.Sub(h) <= longest {
				keep = append(keep, h)
			}
		}
		l.hits = keep

		wait := time.Duration(0)
		for _, w := range l.windows {
			count := 0
			var oldest time.Time
			for _, h := range l.hits {
				if now.Sub(h) <= w.span {
					count++
					if oldest.IsZero() {
						oldest = h
					}
				}
			}
			if count >= w.limit && !oldest.IsZero() {
				// 等到窗口内最早那一次请求滑出去
				if d := w.span - now.Sub(oldest) + 50*time.Millisecond; d > wait {
					wait = d
				}
			}
		}
		if wait <= 0 {
			l.hits = append(l.hits, now)
			l.mu.Unlock()
			return
		}
		l.mu.Unlock()
		slog.Debug("限流等待", "wait", wait)
		l.sleep(wait)
	}
}

// chunkByURLLength 按编码后的实际 URL 长度分批。
//
// 逗号分隔的 id 段是唯一可变部分;单个 id 就超长时仍然单独成批
// (让服务端去拒绝,好过在这里静默丢掉一个物品)。
func chunkByURLLength(itemIDs []string, prefixLen, suffixLen, maxURLLength int) [][]string {
	budget := maxURLLength - prefixLen - suffixLen
	var out [][]string
	var batch []string
	length := 0
	for _, id := range itemIDs {
		encoded := len(url.QueryEscape(id))
		extra := encoded
		if len(batch) > 0 {
			extra++ // 逗号
		}
		if len(batch) > 0 && length+extra > budget {
			out = append(out, batch)
			batch, length = nil, 0
			extra = encoded
		}
		batch = append(batch, id)
		length += extra
	}
	if len(batch) > 0 {
		out = append(out, batch)
	}
	return out
}

type Client struct {
	baseURL string
	cfg     conf.API
	http    *http.Client
	lim     *limiter

	mu       sync.Mutex
	requests int
}

func New(baseURL string, cfg conf.API) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		cfg:     cfg,
		http: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSeconds * float64(time.Second)),
		},
		lim: newLimiter(cfg.RatePerMinute, cfg.RatePer5Min),
	}
}

// Requests 是到目前为止发了多少次请求,报告里要显示。
func (c *Client) Requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	full := c.baseURL + path + "?" + params.Encode()
	delay := time.Second

	for attempt := 0; attempt < c.cfg.MaxRetries; attempt++ {
		c.lim.acquire()
		c.mu.Lock()
		c.requests++
		c.mu.Unlock()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
		if err != nil {
			return err
		}
		// Accept-Encoding 交给 Go 自己加。手动设的话它就不再透明解压了,
		// 响应体会是一堆 gzip 原始字节
		req.Header.Set("User-Agent", "albion-flipper/0.2 (personal market scanner)")

		resp, err := c.http.Do(req)
		if err != nil {
			if attempt == c.cfg.MaxRetries-1 {
				return err
			}
			slog.Warn("请求失败,稍后重试", "url", full, "delay", delay, "err", err)
			if !sleepCtx(ctx, delay) {
				return ctx.Err()
			}
			delay *= 2
			continue
		}

		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			retryAfter := delay
			if v := resp.Header.Get("Retry-After"); v != "" {
				if secs, err := strconv.ParseFloat(v, 64); err == nil {
					retryAfter = time.Duration(secs * float64(time.Second))
				}
			}
			resp.Body.Close()
			slog.Warn("被限流 429", "wait", retryAfter)
			if !sleepCtx(ctx, retryAfter) {
				return ctx.Err()
			}
			delay = max(delay*2, retryAfter)
			continue

		case resp.StatusCode >= 500:
			resp.Body.Close()
			if attempt == c.cfg.MaxRetries-1 {
				return fmt.Errorf("服务端 %d: %s", resp.StatusCode, full)
			}
			slog.Warn("服务端错误,稍后重试", "status", resp.StatusCode, "delay", delay)
			if !sleepCtx(ctx, delay) {
				return ctx.Err()
			}
			delay *= 2
			continue

		case resp.StatusCode >= 400:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			return fmt.Errorf("HTTP %d: %s: %s", resp.StatusCode, full, body)
		}

		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		return err
	}
	return fmt.Errorf("重试 %d 次仍失败: %s", c.cfg.MaxRetries, full)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// batched 把 item id 按 URL 预算切开,逐批取回。
func (c *Client) batched(ctx context.Context, endpoint string, itemIDs []string,
	params url.Values, collect func(raw json.RawMessage) error) error {

	prefix := fmt.Sprintf("%s/api/v2/stats/%s/", c.baseURL, endpoint)
	suffix := ".json?" + params.Encode()

	for _, batch := range chunkByURLLength(itemIDs, len(prefix), len(suffix), c.cfg.MaxURLLength) {
		path := fmt.Sprintf("/api/v2/stats/%s/%s.json", endpoint, strings.Join(batch, ","))
		var raw json.RawMessage
		if err := c.get(ctx, path, params, &raw); err != nil {
			return err
		}
		if err := collect(raw); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) FetchPrices(ctx context.Context, itemIDs, cities []string, qualities []int) ([]PriceRecord, error) {
	params := url.Values{
		"locations": {strings.Join(cities, ",")},
		"qualities": {joinInts(qualities)},
	}
	var out []PriceRecord
	err := c.batched(ctx, "prices", itemIDs, params, func(raw json.RawMessage) error {
		var rows []PriceRecord
		if err := json.Unmarshal(raw, &rows); err != nil {
			return err
		}
		out = append(out, rows...)
		return nil
	})
	return out, err
}

func (c *Client) FetchHistory(ctx context.Context, itemIDs, cities []string, qualities []int,
	days int, timeScale int) ([]HistorySeries, error) {

	if timeScale == 0 {
		timeScale = 24
	}
	params := url.Values{
		"locations":  {strings.Join(cities, ",")},
		"qualities":  {joinInts(qualities)},
		"time-scale": {strconv.Itoa(timeScale)},
		"date":       {time.Now().UTC().AddDate(0, 0, -days).Format("01-02-2006")},
	}
	var out []HistorySeries
	err := c.batched(ctx, "history", itemIDs, params, func(raw json.RawMessage) error {
		var rows []HistorySeries
		if err := json.Unmarshal(raw, &rows); err != nil {
			return err
		}
		out = append(out, rows...)
		return nil
	})
	return out, err
}

func joinInts(v []int) string {
	parts := make([]string, len(v))
	for i, n := range v {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}
