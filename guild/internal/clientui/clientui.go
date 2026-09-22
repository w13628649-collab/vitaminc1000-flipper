// Package clientui 是客户端窗口背后的本地 HTTP 服务。
//
// 成员只开两个窗口:游戏,和我们这个程序。程序窗口里是一个 WebView2,
// 指向本机的这个服务:
//
//	/          编在 exe 里的界面资源
//	/api/*     反代到公会服务端
//	/ws        反代 WebSocket
//	/local/*   客户端自己的状态(抓包、网卡、上传量)
//
// 为什么要反代而不是让前端直接打服务端地址:
//
//   - 前端代码一个字都不用改。同源,没有 CORS,WebSocket 也不用换 host
//   - CDK 令牌在这一层注入。成员永远不需要知道 token 长什么样,
//     更不会把它粘到浏览器地址栏里
//   - 服务端挂了,窗口照样开得起来,能给出一句人话的错误,
//     而不是一片白屏
package clientui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"albion-guild/internal/webui"
)

// Status 是窗口顶部要显示的客户端自身状态。
// 服务端不知道这些——它只看得到"有个客户端在传数据"。
type Status struct {
	Version   string `json:"version"`
	ClientID  string `json:"client_id"`
	Character string `json:"character"`
	Location  string `json:"location"`
	Server    string `json:"server"`

	Devices   int  `json:"devices"`
	Capturing bool `json:"capturing"`
	// DriverWarning 非空表示 Npcap 没装或者装得不对。
	// 这是唯一一个"不解决就完全没用"的问题,必须显眼
	DriverWarning string `json:"driver_warning"`

	// Fallback 为 true 表示界面跑在浏览器里,没有程序窗口。
	// 界面据此显示"退出"按钮
	Fallback bool `json:"fallback"`

	OrdersSeen     int64  `json:"orders_seen"`
	OrdersUploaded int64  `json:"orders_uploaded"`
	Pending        int    `json:"pending"`
	LastUploadAt   string `json:"last_upload_at"`
	LastError      string `json:"last_error"`
	ServerOK       bool   `json:"server_ok"`
}

// StatusFunc 由客户端主程序提供,每次请求时取当前状态。
type StatusFunc func() Status

type Server struct {
	// Upstream 是公会服务端地址。
	Upstream string
	// Token 是 CDK 换来的访问令牌,反代时加到请求头上。暂时可以为空。
	Token  string
	Status StatusFunc
	// Quit 由主程序提供,界面点"退出"时调用。
	Quit func()

	mu       sync.Mutex
	fallback bool

	once  sync.Once
	proxy *httputil.ReverseProxy
}

// SetFallback 标记"没开出窗口,用的是浏览器"。
// 这种情况下界面要多显示一个退出按钮——没有窗口可关,
// 而发布版没有控制台,Ctrl+C 也发不出来。
func (s *Server) SetFallback(v bool) {
	s.mu.Lock()
	s.fallback = v
	s.mu.Unlock()
}

// Listen 在本机回环上挑一个空闲端口起服务,返回窗口该访问的地址。
//
// 端口写死会撞车——成员可能同时开两个角色的客户端。让内核分配,
// 反正地址是程序自己递给 WebView2 的,不用人记。
func (s *Server) Listen(ctx context.Context) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("本地界面端口起不来: %w", err)
	}
	srv := &http.Server{
		Handler:           s.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Warn("本地界面服务退出", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	return "http://" + ln.Addr().String(), nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /local/status", s.handleStatus)
	mux.HandleFunc("POST /local/quit", s.handleQuit)
	mux.Handle("/api/", s.reverse())
	mux.Handle("/ws", s.reverse())
	// 这条不能写成 "GET /":Go 1.22 的 ServeMux 会判它和 "/api/" 冲突
	// ——路径更泛、方法却更窄,它认为这是注册错误而不是兜底
	mux.Handle("/", http.FileServerFS(webui.FS()))
	// 本地端口谁都能打:成员随便访问一个网站,那个页面的 JS 就能
	// 扫到这个端口,然后借着代理往公会服务端塞假数据、或者读走行情。
	// WebSocket 握手不受同源策略约束,更是直接能连。
	// 所以在最外层卡一道:Host 必须是回环地址,Origin 要么没有
	// (我们自己的页面是同源请求,不带 Origin),要么就是我们自己
	return guard(mux)
}

// guard 挡掉浏览器里其他网页对这个本地端口的访问。
//
// 两条:
//   - Host 头必须是 127.0.0.1 / localhost。挡 DNS rebinding——
//     攻击者把自己的域名解析到 127.0.0.1,浏览器就会带着他的 Host 打过来
//   - 有 Origin 的话必须是我们自己。浏览器对跨站请求一定带 Origin,
//     而我们自己页面里的同源 fetch 不带,WebSocket 带的是自己的地址
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host != "127.0.0.1" && host != "localhost" && host != "::1" {
			http.Error(w, "只接受本机访问", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host {
				http.Error(w, "跨站访问被拒绝", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
	if s.Quit != nil {
		// 先把响应发出去再退,否则界面看到的是连接被掐断
		go s.Quit()
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var st Status
	if s.Status != nil {
		st = s.Status()
	}
	s.mu.Lock()
	st.Fallback = s.fallback
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(st)
}

func (s *Server) reverse() http.Handler {
	s.once.Do(func() {
		// url.Parse 对 "10.30.31.30:18420" 这种不报错——它会当成
		// scheme "10.30.31.30"。少写 http:// 是最常见的配错,必须显式挡
		target, err := url.Parse(strings.TrimRight(s.Upstream, "/"))
		if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
			slog.Error("服务端地址不合法,要带 http:// 前缀", "server", s.Upstream, "err", err)
			return
		}
		s.proxy = &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
				// 保留原始 Host 会让服务端拿到 127.0.0.1,日志里没法区分来源
				pr.Out.Host = target.Host
				// 无论有没有 token 都要覆盖掉入站的 Authorization。
				// 只在非空时 Set 的话,页面侧自带的凭据会原样穿过去,
				// 等于把这个本地端口变成一个可以冒充任何人的跳板
				if s.Token != "" {
					pr.Out.Header.Set("Authorization", "Bearer "+s.Token)
				} else {
					pr.Out.Header.Del("Authorization")
				}
			},
			// 服务端连不上时给一句人话。默认的 502 页面是空的,
			// 界面上只会看到一片空白表格,成员根本不知道发生了什么
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				// 界面切页时会取消还没跑完的请求,那不是故障。
				// 当成故障的话会刷 warn 日志,还顺着诊断通道报到服务端去
				if errors.Is(err, context.Canceled) || r.Context().Err() != nil {
					return
				}
				slog.Warn("连不上公会服务端", "path", r.URL.Path, "err", err)
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusBadGateway)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "连不上公会服务端(" + s.Upstream + ")。检查网络,或者问问服务端是不是在维护。",
				})
			},
		}
	})
	if s.proxy == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "服务端地址没配对", http.StatusInternalServerError)
		})
	}
	// ReverseProxy 自 Go 1.12 起原生支持 101 协议升级,WebSocket 直接穿
	return s.proxy
}
