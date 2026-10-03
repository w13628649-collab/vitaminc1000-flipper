package main

import (
	"path/filepath"
	"strings"
)

// 桌面快捷方式的决策。真正读写 .lnk 的部分在 shortcut_windows.go;
// 这里只有纯函数,任何平台上都能测。

const (
	// releaseExeName 是发给成员的文件名。只有以这个名字运行才建快捷方式:
	// dev-up 部署时把运行中的旧 exe 改名成 flipper-client.exe.old-HHMMSS,
	// go run 编出来的叫 client.exe,本地测试构建另有名字——
	// 这些都不该把桌面上的快捷方式抢过去
	releaseExeName = "flipper-client.exe"

	// shortcutFile 是桌面上的文件名,和窗口标题一致,成员认得出来
	shortcutFile = appName + ".lnk"

	// shortcutMarker 放在客户端自己的配置目录里,表示"桌面快捷方式建过了"。
	// 快捷方式不在了而它还在,说明是成员自己删的,不再建回来
	shortcutMarker = "desktop-shortcut.txt"
)

// shortcutEligible 判断以 exe 这个路径运行时该不该管桌面快捷方式。
// tempDir 是系统临时目录;从那里面运行的 exe 过后会被清掉。
func shortcutEligible(exe, tempDir string) (ok bool, why string) {
	if !strings.EqualFold(filepath.Base(exe), releaseExeName) {
		return false, "文件名不是 " + releaseExeName + "(改过名的旧版本、go run、测试构建)"
	}
	// 资源管理器里直接双击压缩包里的 exe,它会被解到临时目录再运行;
	// 指向那里的快捷方式过几天就失效了
	if tempDir != "" && within(exe, tempDir) {
		return false, "在临时目录里运行(多半是直接从压缩包里双击的)"
	}
	return true, ""
}

// linkState 是桌面上现有快捷方式的样子。
type linkState struct {
	Exists bool
	// Target 是它指向的程序。读不出来时为空,按"指向别处"处理
	Target string
}

type shortcutOp int

const (
	opNone     shortcutOp = iota
	opCreate              // 没有,建
	opUpdate              // 有,但指向的不是现在这个 exe(成员挪过 exe),改
	opMarkOnly            // 有、也对,只是没有标记(比如配置目录被清过),补标记
)

// planShortcut 决定这次启动对桌面快捷方式做什么。marked 表示配置目录里有标记。
func planShortcut(exe string, link linkState, marked bool) shortcutOp {
	switch {
	case !link.Exists && marked:
		return opNone // 建过、又没了:成员自己删的,尊重他
	case !link.Exists:
		return opCreate
	case !samePath(link.Target, exe):
		return opUpdate
	case !marked:
		return opMarkOnly
	default:
		return opNone
	}
}

// samePath 按 Windows 的规矩比路径:不分大小写。
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// within 判断 path 是否在 dir 里面(含子目录)。
func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false // 不同盘符
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
