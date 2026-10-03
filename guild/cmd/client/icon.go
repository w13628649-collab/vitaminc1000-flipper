package main

// 图标的生成物——rsrc_windows_amd64.syso、icon/ 目录、界面的 logo.svg——
// 都由 cmd/genicon 从同一份几何生成,提交在仓库里。改图标在 guild 目录下跑
//
//	go generate ./cmd/client
//
//go:generate go run ../genicon -client . -webui ../../internal/webui/assets

// iconResourceID 是 exe 里图标组的整数资源 ID,和 cmd/genicon 的 iconGroupID 是同一个数。
//
// go-webview2 的 WindowOptions.IconId 按它去 exe 资源里取窗口和任务栏图标;
// 留 0 的话它用系统默认的 IDI_APPLICATION,也就是以前那个空白图标。
const iconResourceID = 1

// appName 是这个程序对成员的名字:窗口标题、桌面快捷方式、exe 版本信息里的
// FileDescription(任务管理器显示的就是它)用的都是这一个。
const appName = "Albion 行情终端"
