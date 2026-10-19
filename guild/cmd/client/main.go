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
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"albion-guild/internal/capture"
	"albion-guild/internal/clientui"
	"albion-guild/internal/diag"
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
		noLink   = flag.Bool("no-shortcut", false, "不在桌面建、也不更新快捷方式")
		record   = flag.String("record", "", "同时把抓到的 Albion 原始包写成 .pcap 存到这个目录(回放和回归测试用)")
	)
	flag.Parse()

	// 日志同时写终端和文件。发布版是 -H windowsgui 编的,没有控制台,
	// 出了问题成员能把这个文件发过来;warn 及以上还会自动报到服务端。
	reporter := diag.New(*server, version)
	slog.SetDefault(slog.New(reporter.Handler(
		slog.NewTextHandler(logWriter(), &slog.HandlerOptions{Level: slog.LevelInfo}))))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	diagDone := make(chan struct{})
	go func() { defer close(diagDone); reporter.Run(ctx, 10*time.Second) }()

	// Windows 上先看驱动装没装。这个检查每次启动都要做,
	// 而不是只在安装时做一次——用户可能事后卸了 Npcap。
	driverMsg := ""
	if w := pcapdriver.Check(); w.Message != "" {
		driverMsg = w.Message
		slog.Warn(w.Message, "help", w.HelpURL)
	}

	var devices []capture.Device
	if *device != "" {
		devices = []capture.Device{{Name: *device, Category: capture.Categorize(*device, "")}}
	} else {
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
	categories := map[capture.Category]int{}
	for _, d := range devices {
		categories[d.Category]++
		slog.Info("网卡", "name", d.Name, "description", d.Description, "category", d.Category)
	}
	exitLag := capture.HasExitLag(devices)
	if exitLag {
		slog.Warn(capture.ExitLagHint)
	}
	if *record != "" {
		slog.Info("录包已开", "dir", *record)
	}
	reporter.Hello(ctx, map[string]any{
		"devices": len(devices), "categories": categories, "port": *port,
		"driver": driverMsg, "server": *server, "record": *record != "",
	})
	if len(devices) > 1 {
		slog.Info("将监听多张网卡", "count", len(devices))
	}

	// 界面上"抓包中"要反映**真的打开了几张网卡**,不是枚举到几张。
	// 枚举到 11 张但一张都打不开(Npcap 装成仅管理员模式最常见),
	// 显示"抓包中"就是在骗人,而那恰恰是最需要被看见的故障
	var live atomic.Int32
	// messages 是收到的 Albion 消息总数,started 是启动时刻。判断"开着却抓不到"用
	var messages atomic.Int64
	started := time.Now()

	up := &uploader{
		server: *server,
		token:  *token,
		client: &http.Client{Timeout: 15 * time.Second},
	}

	// 市场挂单是明文还是密文。以前对任何加密包都喊"市场数据被加密",会误诊:
	// 加密按消息粒度,只有市场请求之后没等到明文挂单才算(见 protocol.CryptoTracker)
	crypt := protocol.NewCryptoTracker()
	crypt.OnChange = func(from, to protocol.MarketCrypto) {
		// warn 只在翻到加密时打一次:它会顺着诊断通道报到服务端,刷屏没意义
		if to == protocol.CryptoEncrypted {
			slog.Warn("市场数据被加密:这个号的挂单传不出来(历史价和金价不受影响)", "from", string(from))
			return
		}
		slog.Info("市场数据状态变化", "from", string(from), "to", string(to))
	}

	// 窗口顶部那排状态由这里喂。服务端不知道这些——
	// 它只看得到"有个客户端在传数据",看不到抓包到底转没转
	local := &clientui.Server{
		Upstream: *server,
		Token:    *token,
		Quit:     stop, // 界面上的"退出"按钮,只在开不出窗口时显示
		Status: func() clientui.Status {
			st := up.snapshot()
			st.Version = version
			st.ClientID = reporter.ClientID()
			st.Server = *server
			st.Devices = int(live.Load())
			st.Capturing = live.Load() > 0
			st.DriverWarning = driverMsg
			// 有 ExitLag、开了一分钟还一条 Albion 消息都没收到,才提示。
			// 刚开客户端、游戏还没进图时不提示,不然每次启动都闪一下
			if exitLag && messages.Load() == 0 && time.Since(started) > time.Minute {
				st.CaptureHint = capture.ExitLagHint
			}
			cs := crypt.Snapshot()
			st.MarketCrypto = string(cs.State)
			st.MarketCryptoText = cs.State.Text()
			if !cs.Since.IsZero() {
				st.MarketCryptoSince = cs.Since.Format(time.RFC3339)
			}
			st.OtherEncrypted = cs.OtherEncrypted
			return st
		},
	}

	parser := protocol.New()
	parser.OnOrders = up.add
	parser.OnMarketRequest = crypt.MarketRequest
	parser.OnMarketResponded = crypt.MarketResponded
	parser.OnMarketDecoded = crypt.MarketDecoded
	parser.OnStations = up.addStations
	// 原始样本:明文全抓,所有认得出码的消息都记(protocol/sample.go)
	parser.Version = version
	parser.OnSample = up.addSample
	parser.OnIdentity = func(_, name, loc string) {
		up.setIdentity(name, loc)
		reporter.SetCharacter(name) // 诊断记录带上角色名,便于对人
		slog.Info("角色位置更新", "character", name, "location", loc)
	}

	h := capture.Handler{
		OnRequest: func(code byte, params map[byte]any) {
			messages.Add(1)
			parser.HandleRequest(code, params)
		},
		// Photon 的响应回调带 returnCode 和 debug 字符串,我们用不上
		OnResponse: func(code byte, _ int16, _ string, params map[byte]any) {
			messages.Add(1)
			parser.HandleResponse(code, params)
		},
		// 事件:HandleEvent 记全键样本(明文全抓),还没有类型化处理器
		OnEvent: func(code byte, params map[byte]any) {
			messages.Add(1)
			parser.HandleEvent(code, params)
		},
		// 密文只交给加密判定,由它决定是不是市场被加密;这里不再自己下结论
		OnEncrypted: crypt.Encrypted,
	}

	// 窗口到期的判定要有人推:发完市场请求就不动的话,状态永远翻不过去
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				crypt.Tick()
			}
		}
	}()

	var wg sync.WaitGroup
	for _, d := range devices {
		d := d.Name
		wg.Add(1)
		go func(dev string) {
			defer wg.Done()
			live.Add(1)
			defer live.Add(-1)
			// 一张网卡打不开不该影响其他网卡(Windows 上基于 Wintun 的
			// VPN 适配器经常打不开,这是正常的)
			if err := capture.Listen(ctx, capture.Options{Device: dev, Port: *port, RecordDir: *record}, h); err != nil {
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

		// 成员拿到的就是一个 exe,随手放在下载目录里,过两天就找不着了。
		// 桌面快捷方式在后台建,不拖慢窗口,出错只记日志。
		// -headless 是脚本和服务在用,不走到这里,也就不碰桌面
		if !*noLink {
			go ensureDesktopShortcut()
		}

		// 窗口占住主线程直到用户关掉它。关窗口 = 退出程序,
		// 和任何桌面软件一样;抓包跟着一起停
		if showWindow(ctx, appName, uiURL) {
			stop()
		} else {
			// 开不出窗口(没装 WebView2)就退回默认浏览器。
			//
			// 这时候没有窗口可关,而发布版又没有控制台,Ctrl+C 根本发不出来——
			// 光等 ctx.Done() 的话进程会永远留在后台,只能去任务管理器杀。
			// 所以界面上给一个"退出"按钮,走 /local/quit
			local.SetFallback(true)
			alert("Albion 行情终端 — 开不出程序窗口",
				"这台电脑没有 WebView2 运行时,已经改用默认浏览器打开界面。\n\n"+
					"功能完全一样,只是多一个窗口。想要独立窗口的话,"+
					"装一下 Microsoft Edge WebView2 Runtime 再重开。\n\n"+
					"退出程序请点界面右上角的「退出」。")
			openBrowser(uiURL)
			<-ctx.Done()
		}
	}

	// 等抓包 goroutine 收尾,但不无限等。成员机器上的网卡驱动五花八门,
	// 万一哪个卡在驱动里出不来,也不能让"关掉程序"变成"任务管理器结束进程"。
	waitOrTimeout(&wg, 3*time.Second)

	shutdown := context.WithoutCancel(ctx)
	up.flush(shutdown)

	// 等诊断上报把它手上那批发完,再补发收尾过程中新产生的
	waitOrTimeoutChan(diagDone, 5*time.Second, "诊断上报没能按时发完")
	reporter.Flush(shutdown)
}

func waitOrTimeoutChan(done <-chan struct{}, d time.Duration, msg string) {
	select {
	case <-done:
	case <-time.After(d):
		slog.Warn(msg)
	}
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
	return tolerantWriter{f, os.Stderr}
}

// tolerantWriter 写所有目标,一个写失败不影响其他的。
//
// **不能用 io.MultiWriter**:它遇到第一个错误就返回。发布版是
// -H windowsgui 编的,没有控制台,os.Stderr 是个无效句柄,写它必然失败——
// 于是日志文件一个字都写不进去,而那正是出问题时唯一的现场。
// 顺序也有讲究:文件在前,终端在后。
type tolerantWriter []io.Writer

func (w tolerantWriter) Write(p []byte) (int, error) {
	for _, target := range w {
		_, _ = target.Write(p)
	}
	return len(p), nil // 日志写不出去不该让调用方误以为出了别的事
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
