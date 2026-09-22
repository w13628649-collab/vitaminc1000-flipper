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

	"github.com/ludy/albion-guild/internal/capture"
	"github.com/ludy/albion-guild/internal/model"
	"github.com/ludy/albion-guild/internal/pcapdriver"
	"github.com/ludy/albion-guild/internal/protocol"
)

func main() {
	var (
		server   = flag.String("server", "http://127.0.0.1:8080", "公会服务端地址")
		device   = flag.String("device", "", "只监听某张网卡;留空则全部")
		port     = flag.Int("port", capture.DefaultPort, "Photon 端口")
		interval = flag.Duration("interval", 3*time.Second, "上传间隔")
		token    = flag.String("token", "", "CDK 换来的访问令牌(暂未启用)")
	)
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	// Windows 上先看驱动装没装。这个检查每次启动都要做,
	// 而不是只在安装时做一次——用户可能事后卸了 Npcap。
	if w := pcapdriver.Check(); w.Message != "" {
		slog.Warn(w.Message, "help", w.HelpURL)
	}

	devices := []string{*device}
	if *device == "" {
		found, err := capture.Devices()
		if err != nil {
			slog.Error("枚举网卡失败", "err", err)
			os.Exit(1)
		}
		devices = found
	}
	if len(devices) == 0 {
		slog.Error("没有可用网卡")
		os.Exit(1)
	}
	if len(devices) > 1 {
		slog.Info("将监听多张网卡", "count", len(devices))
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	up := &uploader{
		server: *server,
		token:  *token,
		client: &http.Client{Timeout: 15 * time.Second},
	}

	parser := protocol.New()
	parser.OnOrders = up.add
	parser.OnIdentity = func(_, name, loc string) {
		up.setReporter(name)
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

	slog.Info("客户端已启动", "server", *server)
	<-ctx.Done()
	wg.Wait()
	up.flush(context.WithoutCancel(ctx))
}

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
	u.mu.Unlock()
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
		u.mu.Unlock()
		slog.Warn("上传失败,已退回队列", "count", len(batch.Orders), "err", err)
		return
	}
	defer resp.Body.Close()

	var out struct{ Changed, Touched int }
	_ = json.NewDecoder(resp.Body).Decode(&out)
	slog.Info("已上传", "sent", len(batch.Orders), "changed", out.Changed, "touched", out.Touched)
}
