// 公会版服务端:接收客户端上传的市场数据,去重落库,实时推给在线成员。
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ludy/albion-guild/internal/api"
	"github.com/ludy/albion-guild/internal/hub"
	"github.com/ludy/albion-guild/internal/ingest"
	"github.com/ludy/albion-guild/internal/store"
)

func main() {
	var (
		addr     = flag.String("addr", ":8080", "监听地址")
		dsn      = flag.String("dsn", envOr("FLIPPER_DSN",
			"postgres://postgres:dev@127.0.0.1:55432/flipper"), "PostgreSQL DSN")
		tick     = flag.Duration("tick", 200*time.Millisecond, "行情合并推送间隔")
		fresh    = flag.Duration("fresh", 30*time.Minute, "挂单多久没再看到就不算数")
		cacheSz  = flag.Int("cache", 500_000, "去重用的活跃挂单缓存条数")
	)
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, *dsn)
	if err != nil {
		slog.Error("打开数据库失败", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	h := hub.NewHub()
	// 单实例:直接扇给本进程的连接。要多实例时把这里换成 NATS 实现,
	// Hub.Fanout 那一侧一行都不用动。
	var bc hub.Broadcaster = &hub.LocalBroadcaster{Hub: h}

	conflator := hub.NewConflator(st, bc, *fresh)
	ing, err := ingest.New(st, conflator, *cacheSz)
	if err != nil {
		slog.Error("初始化摄取器失败", "err", err)
		os.Exit(1)
	}

	go ing.Run(ctx)
	go conflator.Run(ctx, *tick)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           api.New(st, ing, h, *fresh).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("服务端已启动", "addr", *addr, "tick", *tick, "fresh", *fresh)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("监听失败", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("正在关闭…")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
