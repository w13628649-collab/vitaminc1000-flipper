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

	"albion-guild/internal/api"
	"albion-guild/internal/conf"
	"albion-guild/internal/flip"
	"albion-guild/internal/hub"
	"albion-guild/internal/ingest"
	"albion-guild/internal/store"
)

// version 由构建时 -ldflags "-X main.version=..." 注入。
//
// 注意:Go 链接器对不存在的符号**静默忽略** -X,所以这个变量必须真的存在,
// 否则构建命令看着对、版本号永远是 dev。
var version = "dev"

func main() {
	var (
		addr = flag.String("addr", ":18420", "监听地址")
		dsn  = flag.String("dsn", envOr("FLIPPER_DSN",
			"postgres://postgres:dev@127.0.0.1:55432/flipper"), "PostgreSQL DSN")
		tick    = flag.Duration("tick", 200*time.Millisecond, "行情合并推送间隔")
		fresh   = flag.Duration("fresh", 30*time.Minute, "挂单多久没再看到就不算数")
		cacheSz = flag.Int("cache", 500_000, "去重用的活跃挂单缓存条数")
		natsURL = flag.String("nats", os.Getenv("FLIPPER_NATS"),
			"NATS 地址;留空走单实例的本地扇出")
		cfgPath  = flag.String("config", os.Getenv("FLIPPER_CONFIG"), "扫描器配置 YAML;留空用内置默认值")
		scanTick = flag.Duration("scan", 30*time.Minute, "自动扫描间隔;0 表示不自动扫")
		// 默认留空。给个 "release" 这种相对路径的话,工作目录一换
		// 路由照常注册、接口却全部 404,排查起来很费劲——
		// webui 那边刚踩过一模一样的坑
		relDir = flag.String("release-dir", "", "客户端二进制目录(绝对路径);留空则不提供下载")
		relVer = flag.String("release-version", "", "发布目录里客户端的版本号")
		minVer = flag.String("min-client", "", "接受的最低客户端版本")
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

	// 建表和加字段都编进二进制了,发版不用再手工跑 SQL
	if err := st.Migrate(ctx); err != nil {
		slog.Error("执行数据库迁移失败", "err", err)
		os.Exit(1)
	}

	cfg := conf.Default()
	if *cfgPath != "" {
		cfg, err = conf.Load(*cfgPath)
		if err != nil {
			slog.Error("读取配置失败", "path", *cfgPath, "err", err)
			os.Exit(1)
		}
	}

	h := hub.NewHub()

	// 单实例就在进程内扇出;给了 -nats 才走消息总线。
	// 两条路最后都落到同一个 Hub.Fanout,行为一致。
	var bc hub.Broadcaster = &hub.LocalBroadcaster{Hub: h}
	if *natsURL != "" {
		nb, err := hub.DialNats(*natsURL, h, "")
		if err != nil {
			slog.Error("连接 NATS 失败", "err", err)
			os.Exit(1)
		}
		bc = nb
		go func() {
			if err := nb.Run(ctx); err != nil {
				slog.Error("NATS 订阅退出", "err", err)
			}
		}()
	}

	conflator := hub.NewConflator(st, bc, *fresh)
	ing, err := ingest.New(st, conflator, *cacheSz)
	if err != nil {
		slog.Error("初始化摄取器失败", "err", err)
		os.Exit(1)
	}

	go ing.Run(ctx)
	go conflator.Run(ctx, *tick)

	flipper := flip.New(st, cfg)
	// 扫描读簿要知道哪些盘口最近串过城:那些盘口的"最近一眼"不可信,暂停幽灵剔除
	flipper.Conflicts = ing
	// 每次发布扫描结果(全量或快速重算)都推一条 scan 通知给订阅了的 WS 连接。
	// 直接给本实例的 hub,不走 NATS:扫描是每个实例各跑各的
	flipper.Events = h
	// 目录拉不动不该拦住服务端启动——行情中转本身不依赖它
	if err := flipper.LoadCatalog(ctx); err != nil {
		slog.Warn("载入物品目录失败,倒爷相关接口会返回 503", "err", err)
	}
	if *scanTick > 0 {
		go flipper.Run(ctx, *scanTick)
	}
	// 两次全量之间用缓存的 AODP 快照配最新抓包重算(capture.reeval_seconds),
	// 不打 AODP。开关关着时立刻返回
	go flipper.RunReeval(ctx)

	apiSrv := api.New(st, ing, h, *fresh, flipper)
	apiSrv.ReleaseDir = *relDir
	apiSrv.ReleaseVersion = *relVer
	apiSrv.MinClientVersion = *minVer

	srv := &http.Server{
		Addr:              *addr,
		Handler:           apiSrv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		slog.Info("服务端已启动", "version", version, "addr", *addr, "tick", *tick, "fresh", *fresh)
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
