//go:build windows

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ensureDesktopShortcut 让桌面上有一个指向当前 exe 的快捷方式。
// 在后台 goroutine 里跑,什么错都只记日志(warn 会跟着诊断报到服务端)。
func ensureDesktopShortcut() {
	// 一个顺手的小功能,出了意外也不能把抓包和窗口一起带走
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("桌面快捷方式:意外中止", "panic", r)
		}
	}()
	exe, err := os.Executable()
	if err != nil {
		slog.Warn("桌面快捷方式:取不到自身路径", "err", err)
		return
	}
	// 用 Known Folder 而不是拼 %USERPROFILE%\Desktop:开了 OneDrive 备份的机器上
	// 桌面被重定向到 OneDrive\Desktop(中文系统还可能是 OneDrive\桌面),拼出来的那个目录没人看
	desktop, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0)
	if err != nil {
		slog.Warn("桌面快捷方式:找不到桌面目录", "err", err)
		return
	}
	op, err := syncShortcut(longPath(exe), longPath(os.TempDir()), desktop, configDir())
	lnk := filepath.Join(desktop, shortcutFile)
	switch {
	case err != nil:
		slog.Warn("桌面快捷方式没建成", "path", lnk, "err", err)
	case op == opCreate:
		slog.Info("已在桌面建快捷方式", "path", lnk)
	case op == opUpdate:
		slog.Info("桌面快捷方式已改为指向当前 exe", "path", lnk, "exe", exe)
	}
}

// syncShortcut 把 desktop 下的快捷方式对齐到 exe,返回做了什么。
// cfgDir 是放标记的目录。拆出来是为了测试能拿临时目录当桌面跑完整流程。
func syncShortcut(exe, tempDir, desktop, cfgDir string) (shortcutOp, error) {
	if ok, why := shortcutEligible(exe, tempDir); !ok {
		slog.Info("不管桌面快捷方式", "原因", why, "exe", exe)
		return opNone, nil
	}
	lnk := filepath.Join(desktop, shortcutFile)
	marker := filepath.Join(cfgDir, shortcutMarker)

	var link linkState
	if _, err := os.Lstat(lnk); err == nil {
		link.Exists = true
		// 读不出来(文件坏了、不是快捷方式)就当它指向别处,下面会整个重写
		if link.Target, err = readShortcutTarget(lnk); err != nil {
			slog.Warn("桌面快捷方式:读不出现有的目标,重写", "path", lnk, "err", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return opNone, err
	}
	_, err := os.Stat(marker)
	marked := err == nil

	op := planShortcut(exe, link, marked)
	switch op {
	case opNone:
		return op, nil
	case opCreate, opUpdate:
		if err := writeShortcut(lnk, exe); err != nil {
			return op, err
		}
	}
	// 标记写不进去不算失败:快捷方式已经在了,代价只是成员删掉它之后下次又会冒出来
	note := lnk + "\r\n\r\n" +
		"客户端在桌面建过快捷方式。快捷方式被删掉而这个文件还在,就不会再建回来;\r\n" +
		"想让它重新建,删掉这个文件再开一次客户端。\r\n"
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		slog.Warn("桌面快捷方式:标记写不进去", "path", marker, "err", err)
	} else if err := os.WriteFile(marker, []byte(note), 0o644); err != nil {
		slog.Warn("桌面快捷方式:标记写不进去", "path", marker, "err", err)
	}
	return op, nil
}

// longPath 把 8.3 短路径展开成长路径。用户名带中文或者很长时,%TEMP% 常常是
// C:\Users\ABCDEF~1\AppData\Local\Temp 这种,而 os.Executable 给的是长路径,
// 不展开就认不出 exe 在临时目录里。展开失败(路径不存在等)原样返回。
func longPath(p string) string {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetLongPathName(u, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			return p
		}
		if int(n) < len(buf) {
			return windows.UTF16ToString(buf[:n])
		}
		buf = make([]uint16, n) // 缓冲区不够时 n 是需要的长度(含结尾 NUL)
	}
}

// ── IShellLinkW / IPersistFile ──────────────────────────────────────────
//
// .lnk 用 Shell 自己的 COM 对象写,不调 PowerShell 的 WScript.Shell:
// 一个抓包程序在后台拉起隐藏的 PowerShell 去写文件,正是杀毒软件启发式最爱拦的样子,
// 而且 PowerShell 冷启动要一两秒。

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidShellLink  = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIShellLinkW  = windows.GUID{Data1: 0x000214F9, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidIPersistFile = windows.GUID{Data1: 0x0000010B, Data4: [8]byte{0xC0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// 虚表槽位,顺序照 ShObjIdl_core.h / ObjIdl.h。
const (
	vtQueryInterface = 0
	vtRelease        = 2

	slGetPath             = 3
	slSetDescription      = 7
	slSetWorkingDirectory = 9
	slSetIconLocation     = 17
	slSetPath             = 20

	pfLoad = 5
	pfSave = 6
)

// comObject 是 COM 接口指针指向的东西,第一个字段是虚表指针。
// 数组长度只是给下标用的上限,不会真去读超出虚表的部分。
type comObject struct{ vtbl *[32]uintptr }

func hresult(what string, r uintptr) error {
	if int32(r) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(r))
	}
	return nil
}

func (o *comObject) release() {
	syscall.SyscallN(o.vtbl[vtRelease], uintptr(unsafe.Pointer(o)))
}

// callStr 调一个第一个参数是字符串的方法,extra 是后面的整数参数。
func (o *comObject) callStr(slot int, what, s string, extra ...uintptr) error {
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	args := append([]uintptr{uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(p))}, extra...)
	r, _, _ := syscall.SyscallN(o.vtbl[slot], args...)
	runtime.KeepAlive(p) // p 只以 uintptr 的形式进了 args,调用结束前不能被回收
	return hresult(what, r)
}

func (o *comObject) persistFile() (*comObject, error) {
	var pf *comObject
	r, _, _ := syscall.SyscallN(o.vtbl[vtQueryInterface], uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(&iidIPersistFile)), uintptr(unsafe.Pointer(&pf)))
	if err := hresult("QueryInterface(IPersistFile)", r); err != nil {
		return nil, err
	}
	return pf, nil
}

// withShellLink 在一个初始化好 COM 的线程上建一个 ShellLink 对象交给 fn。
func withShellLink(fn func(sl, pf *comObject) error) error {
	// COM 的初始化是按线程的,整个过程必须待在同一个 OS 线程上
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const rpcEChangedMode = 0x80010106
	switch err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); {
	case err == nil, err == syscall.Errno(1): // S_FALSE:这个线程早初始化过,同样要配对 Uninitialize
		defer windows.CoUninitialize()
	case err == syscall.Errno(rpcEChangedMode):
		// 线程已经是多线程套间。ShellLink 两种套间都能用,不用我们配对
	default:
		return fmt.Errorf("CoInitializeEx: %w", err)
	}

	if err := procCoCreateInstance.Find(); err != nil {
		return err // Addr() 找不到会 panic
	}
	var sl *comObject
	const clsctxInprocServer = 1
	r, _, _ := syscall.SyscallN(procCoCreateInstance.Addr(),
		uintptr(unsafe.Pointer(&clsidShellLink)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIShellLinkW)), uintptr(unsafe.Pointer(&sl)))
	if err := hresult("CoCreateInstance(ShellLink)", r); err != nil {
		return err
	}
	defer sl.release()
	pf, err := sl.persistFile()
	if err != nil {
		return err
	}
	defer pf.release()
	return fn(sl, pf)
}

// writeShortcut 在 lnk 写一个启动 exe 的快捷方式,已有就覆盖。
// 工作目录是 exe 所在目录;图标取 exe 里的第一个图标组(exe,0),也就是 genicon 编进去的那个。
func writeShortcut(lnk, exe string) error {
	return withShellLink(func(sl, pf *comObject) error {
		steps := []struct {
			slot  int
			what  string
			s     string
			extra []uintptr
		}{
			{slSetPath, "SetPath", exe, nil},
			{slSetWorkingDirectory, "SetWorkingDirectory", filepath.Dir(exe), nil},
			{slSetIconLocation, "SetIconLocation", exe, []uintptr{0}},
			{slSetDescription, "SetDescription", appName, nil},
		}
		for _, s := range steps {
			if err := sl.callStr(s.slot, "IShellLink."+s.what, s.s, s.extra...); err != nil {
				return err
			}
		}
		return pf.callStr(pfSave, "IPersistFile.Save", lnk, 1) // fRemember = TRUE
	})
}

// readShortcutTarget 读出 .lnk 指向的程序路径。
func readShortcutTarget(lnk string) (string, error) {
	var target string
	err := withShellLink(func(sl, pf *comObject) error {
		if err := pf.callStr(pfLoad, "IPersistFile.Load", lnk, 0); err != nil { // STGM_READ
			return err
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		// 不传 WIN32_FIND_DATA、标志位 0:只要存着的路径,不去碰目标文件
		r, _, _ := syscall.SyscallN(sl.vtbl[slGetPath], uintptr(unsafe.Pointer(sl)),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
		if err := hresult("IShellLink.GetPath", r); err != nil {
			return err
		}
		target = windows.UTF16ToString(buf) // 没有路径时返回 S_FALSE,buf 是空串
		return nil
	})
	return target, err
}
