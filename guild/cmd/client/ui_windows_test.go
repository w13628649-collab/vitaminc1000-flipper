//go:build windows

package main

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 窗口和任务栏图标靠 go-webview2 用 LoadImageW 按 iconResourceID 取。
// 测试二进制和正式 exe 一样链接了 rsrc_windows_amd64.syso,在这里就能验证
// 资源 ID 对得上、Windows 认得这些 PNG 图标项——不用把窗口真开出来。
func TestIconResourceLoads(t *testing.T) {
	for _, size := range []int{16, 20, 24, 32, 48, 256} {
		if loadIcon(size) == 0 {
			t.Errorf("取不到 %dpx 的图标组 #%d", size, iconResourceID)
		}
	}

	// go-webview2 自己那次调用:尺寸 0 + LR_DEFAULTSIZE
	var self windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &self); err != nil {
		t.Fatal(err)
	}
	const imageIcon, lrDefaultSize, lrShared = 1, 0x40, 0x8000
	h, _, err := procLoadImageW.Call(uintptr(self), iconResourceID, imageIcon, 0, 0, lrDefaultSize|lrShared)
	if h == 0 {
		t.Fatalf("go-webview2 的取法取不到图标: %v", err)
	}

	// 反过来确认没取到的时候真的是 0:不然上面的断言什么都证明不了
	if loadIconID(t, self, iconResourceID+100) != 0 {
		t.Fatal("不存在的资源 ID 居然也取到了图标")
	}
}

// exe 版本信息里的 FileDescription(任务管理器显示的名字)写在 cmd/genicon 里,
// 窗口标题和快捷方式名用的是这里的 appName。两边得是同一个名字。
func TestVersionInfoMatchesAppName(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	size, err := windows.GetFileVersionInfoSize(exe, nil)
	if err != nil {
		t.Fatalf("测试二进制里没有版本信息资源: %v", err)
	}
	buf := make([]byte, size)
	if err := windows.GetFileVersionInfo(exe, 0, size, unsafe.Pointer(&buf[0])); err != nil {
		t.Fatal(err)
	}
	var p *uint16
	var n uint32
	if err := windows.VerQueryValue(unsafe.Pointer(&buf[0]), `\StringFileInfo\000004b0\FileDescription`,
		unsafe.Pointer(&p), &n); err != nil {
		t.Fatalf("查不到 FileDescription: %v", err)
	}
	if got := windows.UTF16PtrToString(p); got != appName {
		t.Fatalf("版本信息的 FileDescription 是 %q,appName 是 %q:改一边要改另一边(cmd/genicon/syso.go)", got, appName)
	}
}

func loadIconID(t *testing.T, self windows.Handle, id uintptr) uintptr {
	t.Helper()
	const imageIcon = 1
	h, _, _ := procLoadImageW.Call(uintptr(self), id, imageIcon, 16, 16, 0)
	return h
}
