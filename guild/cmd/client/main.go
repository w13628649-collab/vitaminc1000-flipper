// 公会版客户端:抓本机的 Albion 流量,解析市场挂单,上传到公会服务端。
//
// 这一版是命令行的,Wails 界面后面再套。抓包能力需要 -tags pcap 编译:
//
//	go build -tags pcap -o flipper-client.exe ./cmd/client
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"albion-guild/internal/capture"
	"albion-guild/internal/diag"
	"albion-guild/internal/model"
	"albion-guild/internal/pcapdriver"
	"albion-guild/internal/protocol"
)

// 这两个由构建时的 -ldflags 注入:
//
//	-X main.version=... -X main.defaultServer=http://...
//
// 发给成员的构建直接带上服务端地址,他们双击就行,不用敲参数
var (
	version       = "dev"
	defaultServer = "http://127.0.0.1:8080"
)

func main() {
	var (
		server   = flag.String("server", defaultServer, "公会服务端地址")
		device   = flag.String("device", "", "只监听某张网卡;留空则全部")
		port     = flag.Int("port", capture.DefaultPort, "Photon 端口")
		interval = flag.Duration("interval", 3*time.Second, "上传间隔")
		token    = flag.String("token", "", "CDK 换来的访问令牌(暂未启用)")
	)
	flag.Parse()

	// 日志照常打到终端,同时 warn 及以上会被收走报给服务端。
	// 这样成员在自己机器上出问题,我们在服务端就能看到。
	reporter := diag.New(*server, version)
	slog.SetDefault(slog.New(reporter.Handler(
		slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go reporter.Run(ctx, 10*time.Second)

	// Windows 上先看驱动装没装。这个检查每次启动都要做,
	// 而不是只在安装时做一次——用户可能事后卸了 Npcap。
	driverMsg := ""
	if w := pcapdriver.Check(); w.Message != "" {
		driverMsg = w.Message
		slog.Warn(w.Message, "help", w.HelpURL)
	}

	devices := []string{*device}
	if *device == "" {
		found, err := capture.Devices()
		if err != nil {
			slog.Error("枚举网卡失败", "err", err)
			reporter.Flush(ctx)
			os.Exit(1)
		}
		devices = found
	}
	if len(devices) == 0 {
		slog.Error("没有可用网卡")
		reporter.Flush(ctx)
		os.Exit(1)
	}

	// 先报一次环境。即使之后一条错误都没有,服务端也能看到
	// "这个人确实把客户端跑起来了、装的什么版本、网卡有几张"
	reporter.Hello(ctx, map[string]any{
		"devices": len(devices), "port": *port,
		"driver": driverMsg, "server": *server,
	})
	if len(devices) > 1 {
		slog.Info("将监听多张网卡", "count", len(devices))
	}

	up := &uploader{
		server: *server,
		token:  *token,
		client: &http.Client{Timeout: 15 * time.Second},
	}

	parser := protocol.New()
	parser.OnOrders = up.add
	parser.OnIdentity = func(_, name, loc string) {
		up.setReporter(name)
		reporter.SetCharacter(name) // 诊断记录带上角色名,便于对人
		slog.Info("角色位置更新", "character", name, "location", loc)
	}

	h := capture.Handler{
		OnRequest: parser.HandleRequest,
		// Photon 的响应回调带 returnCode 和 debug 字符串,我们用不上
		OnResponse: func(code byte, _ int16, _ string, params map[byte]any) {
			parser.HandleResponse(code, params)
		},
		OnEncrypted: func() {
			// 市场数据被加密时会走到这:新号的挂单传不了,
			// 这是账号层面的策略,换个老号就好了
			slog.Warn("收到加密包,这个账号的市场数据可能传不出来")
		},
	}

	var wg sync.WaitGroup
	for _, d := range devices {
		wg.Add(1)
		go func(dev string) {
			defer wg.Done()
			// 一张网卡打不开不该影响其他网卡(Windows 上基于 Wintun 的
			// VPN 适配器经常打不开,这是正常的)
			if err := capture.Listen(ctx, capture.Options{Device: dev, Port: *port}, h); err != nil {
				slog.Warn("这张网卡跳过", "device", dev, "err", err)
			}
		}(d)
	}

	go up.run(ctx, *interval)

	slog.Info("客户端已启动", "server", *server, "client_id", reporter.ClientID())
	<-ctx.Done()

	// 等抓包 goroutine 收尾,但不无限等。成员机器上的网卡驱动五花八门,
	// 万一哪个卡在驱动里出不来,也不能让"关掉程序"变成"任务管理器结束进程"。
	waitOrTimeout(&wg, 3*time.Second)

	shutdown := context.WithoutCancel(ctx)
	up.flush(shutdown)
	reporter.Flush(shutdown)
}

func waitOrTimeout(wg *sync.WaitGroup, d time.Duration) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		slog.Warn("抓包线程没能按时退出,直接收尾")
	}
}

// maxPending 是断网时内存里最多堆多少条挂单。
// 一条大约 100 字节,10 万条约 10 MB——成员的机器扛得住,
// 再多就没意义了:市场数据本来就是越新越值钱。
const maxPending = 100_000

// uploader 攒一批再发,不是每解析出一条就发一次。
type uploader struct {
	server string
	token  string
	client *http.Client

	mu       sync.Mutex
	pending  []model.MarketOrder
	reporter string
}

func (u *uploader) add(orders []model.MarketOrder) {
	u.mu.Lock()
	u.pending = append(u.pending, orders...)
	u.trimLocked()
	u.mu.Unlock()
}

// trimLocked 超上限就丢最旧的。调用方必须持锁。
func (u *uploader) trimLocked() {
	if n := len(u.pending) - maxPending; n > 0 {
		u.pending = append(u.pending[:0], u.pending[n:]...)
	}
}

func (u *uploader) setReporter(name string) {
	u.mu.Lock()
	u.reporter = name
	u.mu.Unlock()
}

func (u *uploader) run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.flush(ctx)
		}
	}
}

func (u *uploader) flush(ctx context.Context) {
	u.mu.Lock()
	batch := model.UploadBatch{Reporter: u.reporter, Orders: u.pending}
	u.pending = nil
	u.mu.Unlock()

	if len(batch.Orders) == 0 {
		return
	}

	body, err := json.Marshal(batch)
	if err != nil {
		slog.Error("序列化失败", "err", err)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		u.server+"/api/upload", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		// 网络不好就把这批退回队列,下次一起发。
		// 断网期间数据堆在内存里,连上自动补——这是 HTTP 比 WS 省事的地方。
		u.mu.Lock()
		u.pending = append(batch.Orders, u.pending...)
		u.trimLocked()
		u.mu.Unlock()
		slog.Warn("上传失败,已退回队列", "count", len(batch.Orders), "err", err)
		return
	}
	defer resp.Body.Close()

	var out struct{ Changed, Touched int }
	_ = json.NewDecoder(resp.Body).Decode(&out)
	slog.Info("已上传", "sent", len(batch.Orders), "changed", out.Changed, "touched", out.Touched)
}
