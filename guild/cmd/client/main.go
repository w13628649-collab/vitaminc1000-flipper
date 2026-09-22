// 公会版客户端:一个窗口搞定抓包和看盘。
//
// 成员只开两个东西——游戏,和这个程序。程序里是一个 WebView2 窗口,
// 指向本机起的界面服务;界面资源编在 exe 里,数据通过反向代理
// 从公会服务端拿。服务端只管收数据、算账、下发,不碰界面。
//
// 抓包需要 -tags pcap 编译,发布版还要 -H windowsgui 藏掉控制台:
//
//	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -tags pcap \
//	  -ldflags "-H windowsgui -X main.version=… -X main.defaultServer=…" \
//	  -o flipper-client.exe ./cmd/client
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"albion-guild/internal/capture"
	"albion-guild/internal/clientui"
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
	defaultServer = "http://127.0.0.1:18420"
)

func main() {
	var (
		server   = flag.String("server", defaultServer, "公会服务端地址")
		device   = flag.String("device", "", "只监听某张网卡;留空则全部")
		port     = flag.Int("port", capture.DefaultPort, "Photon 端口")
		interval = flag.Duration("interval", 3*time.Second, "上传间隔")
		token    = flag.String("token", "", "CDK 换来的访问令牌(暂未启用)")
		headless = flag.Bool("headless", false, "不开窗口,只抓包上传")
	)
	flag.Parse()

	// 日志同时写终端和文件。发布版是 -H windowsgui 编的,没有控制台,
	// 出了问题成员能把这个文件发过来;warn 及以上还会自动报到服务端。
	reporter := diag.New(*server, version)
	slog.SetDefault(slog.New(reporter.Handler(
		slog.NewTextHandler(logWriter(), &slog.HandlerOptions{Level: slog.LevelInfo}))))

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
			fail(ctx, reporter, "枚举网卡失败", err.Error())
		}
		devices = found
	}
	if len(devices) == 0 {
		fail(ctx, reporter, "没有可用网卡",
			"一张网卡都枚举不到。多半是 Npcap 没装,或者装成了仅管理员模式。\n\n"+driverMsg)
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

	// 窗口顶部那排状态由这里喂。服务端不知道这些——
	// 它只看得到"有个客户端在传数据",看不到抓包到底转没转
	local := &clientui.Server{
		Upstream: *server,
		Token:    *token,
		Status: func() clientui.Status {
			st := up.snapshot()
			st.Version = version
			st.ClientID = reporter.ClientID()
			st.Server = *server
			st.Devices = len(devices)
			st.Capturing = len(devices) > 0 && driverMsg == ""
			st.DriverWarning = driverMsg
			return st
		},
	}

	parser := protocol.New()
	parser.OnOrders = up.add
	parser.OnIdentity = func(_, name, loc string) {
		up.setIdentity(name, loc)
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

	slog.Info("客户端已启动", "version", version, "server", *server, "client_id", reporter.ClientID())

	if *headless {
		<-ctx.Done()
	} else {
		uiURL, err := local.Listen(ctx)
		if err != nil {
			fail(ctx, reporter, "界面起不来", err.Error())
		}
		slog.Info("本地界面已就绪", "url", uiURL)
		fmt.Fprintf(os.Stderr, "\n  界面:%s\n\n", uiURL)

		// 窗口占住主线程直到用户关掉它。关窗口 = 退出程序,
		// 和任何桌面软件一样;抓包跟着一起停
		if showWindow(ctx, "Albion 行情终端", uiURL) {
			stop()
		} else {
			// 没有 WebView2 就退回默认浏览器,功能一样,只是多一个窗口
			openBrowser(uiURL)
			<-ctx.Done()
		}
	}

	// 等抓包 goroutine 收尾,但不无限等。成员机器上的网卡驱动五花八门,
	// 万一哪个卡在驱动里出不来,也不能让"关掉程序"变成"任务管理器结束进程"。
	waitOrTimeout(&wg, 3*time.Second)

	shutdown := context.WithoutCancel(ctx)
	up.flush(shutdown)
	reporter.Flush(shutdown)
}

// fail 是"不解决就完全没用"的启动失败。
//
// 发布版没有控制台,光 slog.Error 成员什么都看不到,只会觉得
// "双击了没反应"。所以同时弹系统对话框,并且把这条错误发到服务端。
func fail(ctx context.Context, reporter *diag.Reporter, title, body string) {
	slog.Error(title, "detail", body)
	reporter.Flush(ctx)
	alert("Albion 行情终端 — "+title, body)
	os.Exit(1)
}

// logWriter 让日志同时去终端和文件。
//
// 文件放在配置目录里,和 client_id 同一处。超过 2MB 就从头写——
// 排查只看最近的,留着几百兆的历史没意义。
func logWriter() io.Writer {
	dir := configDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return os.Stderr
	}
	path := filepath.Join(dir, "client.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 2<<20 {
		_ = os.Remove(path)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return os.Stderr
	}
	return io.MultiWriter(os.Stderr, f)
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

	mu        sync.Mutex
	pending   []model.MarketOrder
	character string
	location  string
	seen      int64
	uploaded  int64
	lastAt    time.Time
	lastErr   string
}

// snapshot 取当前状态给界面看。只填客户端自己知道的那几项,
// 版本、网卡数之类由调用方补。
func (u *uploader) snapshot() clientui.Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	st := clientui.Status{
		Character:      u.character,
		Location:       u.location,
		OrdersSeen:     u.seen,
		OrdersUploaded: u.uploaded,
		Pending:        len(u.pending),
		LastError:      u.lastErr,
		ServerOK:       u.lastErr == "",
	}
	if !u.lastAt.IsZero() {
		st.LastUploadAt = u.lastAt.Format(time.RFC3339)
	}
	return st
}

func (u *uploader) add(orders []model.MarketOrder) {
	u.mu.Lock()
	u.pending = append(u.pending, orders...)
	u.seen += int64(len(orders))
	u.trimLocked()
	u.mu.Unlock()
}

// trimLocked 超上限就丢最旧的。调用方必须持锁。
func (u *uploader) trimLocked() {
	if n := len(u.pending) - maxPending; n > 0 {
		u.pending = append(u.pending[:0], u.pending[n:]...)
	}
}

func (u *uploader) setIdentity(name, location string) {
	u.mu.Lock()
	u.character, u.location = name, location
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
	batch := model.UploadBatch{Reporter: u.character, Orders: u.pending}
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
		u.lastErr = err.Error()
		u.mu.Unlock()
		slog.Warn("上传失败,已退回队列", "count", len(batch.Orders), "err", err)
		return
	}
	defer resp.Body.Close()

	var out struct{ Changed, Touched int }
	_ = json.NewDecoder(resp.Body).Decode(&out)

	u.mu.Lock()
	u.uploaded += int64(len(batch.Orders))
	u.lastAt = time.Now()
	u.lastErr = ""
	u.mu.Unlock()
	slog.Info("已上传", "sent", len(batch.Orders), "changed", out.Changed, "touched", out.Touched)
}
