// Package webui 是界面资源,编进二进制。
//
// 资源跟着**客户端**走,不是服务端:成员只开一个窗口,界面由客户端
// 本地伺服,服务端只管数据和运算。编进去还顺手解决了一个隐患——
// 原来服务端用 http.Dir("web") 读磁盘,路径是相对进程工作目录的,
// 换个启动方式(比如 systemd 没设 WorkingDirectory)界面就 404。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed assets
var assets embed.FS

// FS 是以 assets/ 为根的文件系统,直接喂给 http.FileServerFS。
func FS() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err) // 目录是编译期确定的,走不到这里
	}
	return sub
}
