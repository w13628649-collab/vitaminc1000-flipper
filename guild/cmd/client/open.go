package main

import (
	"log/slog"
	"os/exec"
	"runtime"
)

// openBrowser 把界面在默认浏览器里打开。
//
// 客户端本身没有窗口——它只负责抓包上传。界面跑在服务端上,
// 成员双击 exe 之后应该直接看到界面,而不是对着一个黑窗口发呆
// 再自己去记 IP 和端口。
//
// 用 rundll32 而不是 `cmd /c start`:后者会闪一个命令行窗口出来。
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	// 打不开不是错误,成员自己敲地址也一样能用
	if err := cmd.Start(); err != nil {
		slog.Debug("没能自动打开浏览器", "err", err)
		return
	}
	// 不等它退出,也不留僵尸进程
	go func() { _ = cmd.Wait() }()
}
