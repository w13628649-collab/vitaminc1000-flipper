package aodp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"albion-guild/internal/conf"
)

// 2026-10-02 实测 AODP 的样子:TCP 连得上,HTTP 一直不回。查价页因此每点一次
// 白等满 12 秒。Down 要能认出这种状态,对面一恢复就清掉
func TestDown_不响应算挂恢复后清掉(t *testing.T) {
	var mode atomic.Int32 // 0 正常 1 不回话 2 回 502 3 回 429 4 回 404
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch mode.Load() {
		case 1:
			time.Sleep(300 * time.Millisecond)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
			return
		case 3:
			w.WriteHeader(http.StatusTooManyRequests)
			return
		case 4:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := New(srv.URL, conf.API{RatePerMinute: 1000, RatePer5Min: 1000, TimeoutSeconds: 0.05, MaxRetries: 1})
	ctx := context.Background()
	fetch := func() error {
		_, err := c.FetchPrices(ctx, []string{"T4_ORE"}, []string{"Thetford"}, []int{1})
		return err
	}
	isDown := func() (string, bool) {
		_, why, down := c.Down(time.Now(), time.Minute)
		return why, down
	}

	if _, down := isDown(); down {
		t.Fatal("还没发过请求,不该算挂")
	}

	mode.Store(1)
	if fetch() == nil {
		t.Fatal("不回话应该报错")
	}
	why, down := isDown()
	if !down || why != "请求超时" {
		t.Fatalf("不回话应该算挂、原因是请求超时,得到 down=%v why=%q", down, why)
	}

	mode.Store(0)
	if err := fetch(); err != nil {
		t.Fatalf("恢复后应该成功: %v", err)
	}
	if _, down := isDown(); down {
		t.Fatal("成功一次之后就不该再算挂")
	}

	mode.Store(2)
	_ = fetch()
	if why, down := isDown(); !down || why != "服务端 502" {
		t.Fatalf("5xx 应该算挂,得到 down=%v why=%q", down, why)
	}

	// 429 和 4xx 是对面回话了,只是不给数据:不算挂,而且会清掉上面的 502
	mode.Store(4)
	_ = fetch()
	if _, down := isDown(); down {
		t.Fatal("404 说明对面在线,不该算挂")
	}
	mode.Store(2)
	_ = fetch()
	mode.Store(3)
	_ = fetch()
	if _, down := isDown(); down {
		t.Fatal("429 说明对面在线,不该算挂")
	}
}

// 调用方自己不等了(ctx 到期/取消)不是 AODP 的锅,不能把它记成"连不上"
func TestDown_调用方自己取消不算(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	c := New(srv.URL, conf.API{RatePerMinute: 1000, RatePer5Min: 1000, TimeoutSeconds: 5, MaxRetries: 1})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := c.FetchPrices(ctx, []string{"T4_ORE"}, []string{"Thetford"}, []int{1}); err == nil {
		t.Fatal("调用方 20ms 就不等了,应该报错")
	}
	if _, _, down := c.Down(time.Now(), time.Minute); down {
		t.Fatal("调用方自己取消的,不该记成 AODP 连不上")
	}
}

// 最近一次失败太久以前,就不再当它还挂着:定时扫描隔一阵会再碰 AODP,
// 没有新记录说明这段时间没人用它,回到正常的等法
func TestDown_失败太久以前就不算(t *testing.T) {
	c := New("http://unused.invalid", conf.API{RatePerMinute: 1000, RatePer5Min: 1000, TimeoutSeconds: 1, MaxRetries: 1})
	c.noteFail("请求超时")
	if _, _, down := c.Down(time.Now(), time.Minute); !down {
		t.Fatal("刚失败过应该算挂")
	}
	if _, _, down := c.Down(time.Now().Add(2*time.Minute), time.Minute); down {
		t.Fatal("失败已经超出窗口,不该再算挂")
	}
}
