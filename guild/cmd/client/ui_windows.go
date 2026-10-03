//go:build windows

package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
)

// showWindow 开一个内嵌的应用窗口指向本地界面,阻塞到用户关掉它。
//
// 返回 false 表示这台机器开不出窗口——几乎只有一个原因:没装 WebView2 运行时。
// Win11 和打过补丁的 Win10 自带,老机器可能没有。这时候退回默认浏览器,
// 功能一样,只是多一个窗口。
func showWindow(ctx context.Context, title, url string) bool {
	// Win32 的消息循环必须一直待在同一个 OS 线程上。
	// 不锁的话 Go 的调度器可能把这个 goroutine 挪到别的线程,窗口直接失去响应
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		// WebView2 的缓存放到自己的目录,别污染用户的 Edge 配置
		DataPath: filepath.Join(configDir(), "webview"),
		WindowOptions: webview2.WindowOptions{
			Title: title, Width: 1440, Height: 920, Center: true,
			IconId: iconResourceID,
		},
	})
	if w == nil {
		slog.Warn("开不出应用窗口,退回浏览器",
			"可能原因", "这台机器没装 WebView2 运行时")
		return false
	}
	defer w.Destroy()
	setWindowIcons(uintptr(w.Window()))

	// 收到 Ctrl+C / SIGTERM 时得主动把窗口关掉,否则 Run() 永远不返回,
	// 进程卡在那里不退
	go func() {
		<-ctx.Done()
		w.Dispatch(w.Terminate)
	}()

	w.Navigate(url)
	w.Run() // 阻塞到窗口被关闭
	return true
}

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procSendMessageW     = user32.NewProc("SendMessageW")
)

// loadIcon 按指定像素尺寸从本 exe 的资源里取图标组 iconResourceID。
// 图标组里有这个尺寸就原样拿,没有才由系统缩放。返回 0 表示没取到。
func loadIcon(size int) uintptr {
	const imageIcon, lrShared = 1, 0x8000
	var self windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &self); err != nil {
		return 0
	}
	h, _, _ := procLoadImageW.Call(uintptr(self), iconResourceID, imageIcon,
		uintptr(size), uintptr(size), lrShared)
	return h
}

// setWindowIcons 给窗口分别设大小两个图标。
//
// go-webview2 只按 IconId 取一次默认尺寸(32px),窗口类的大图标和小图标
// 用的是同一个句柄,标题栏的 16px 就成了 32px 现场缩小的,发虚。
// 图标组里有专门对齐过像素的 16px,这里按系统要的尺寸各取一次。
// 取不到就算了,窗口类上那个 32px 的照样能用。
func setWindowIcons(hwnd uintptr) {
	const (
		wmSetIcon            = 0x0080
		iconSmall, iconBig   = 0, 1
		smCxIcon, smCxSmIcon = 11, 49
	)
	for _, k := range []struct{ which, metric uintptr }{
		{iconSmall, smCxSmIcon}, {iconBig, smCxIcon},
	} {
		size, _, _ := procGetSystemMetrics.Call(k.metric)
		if h := loadIcon(int(size)); h != 0 {
			procSendMessageW.Call(hwnd, wmSetIcon, k.which, h)
		}
	}
}

// alert 弹一个系统对话框。
//
// 窗口版是 -H windowsgui 编的,没有控制台。"Npcap 没装"这种
// 不解决就完全没用的问题必须当面告诉成员,而不是写进一个
// 他永远不会打开的日志文件。
func alert(title, body string) {
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	b, err := windows.UTF16PtrFromString(body)
	if err != nil {
		return
	}
	_, _ = windows.MessageBox(0, b, t, windows.MB_OK|windows.MB_ICONEXCLAMATION|windows.MB_SETFOREGROUND)
}

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "."
	}
	return filepath.Join(dir, "AlbionFlipper")
}
