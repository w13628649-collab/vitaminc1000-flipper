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
	case isAccessDenied(err):
		slog.Warn("桌面快捷方式没建成:桌面不让写", "path", lnk, "err", err,
			"可能原因", "Windows 安全中心开了受控文件夹访问")
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
		// 读不出来(文件坏了、不是快捷方式)就当它指向别处,下面 retargetShortcut 会重写
		if link.Target, err = readShortcutTarget(lnk); err != nil {
			slog.Warn("桌面快捷方式:读不出现有的目标", "path", lnk, "err", err)
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
	case opCreate:
		err = writeShortcut(lnk, exe)
	case opUpdate:
		err = retargetShortcut(lnk, exe)
	}
	switch {
	case err == nil:
		writeMarker(cfgDir, lnk+"\r\n\r\n"+
			"客户端在桌面建过快捷方式。快捷方式被删掉而这个文件还在,就不会再建回来;\r\n"+
			"想让它重新建,删掉这个文件再开一次客户端。\r\n")
	case op == opCreate && isAccessDenied(err):
		// 桌面不让写,多半是 Windows 安全中心的"受控文件夹访问"(桌面是受保护目录)。
		// 不记下来的话每次启动都重试,每次都弹一条"已阻止未经授权的更改"
		writeMarker(cfgDir, lnk+"\r\n\r\n"+
			"在桌面建快捷方式被拒绝了(多半是 Windows 安全中心的「受控文件夹访问」),\r\n"+
			"所以之后不再每次重试。放行本程序之后删掉这个文件,再开一次客户端就会重新建。\r\n")
	}
	return op, err
}

// writeMarker 写"桌面快捷方式这件事处理过了"的标记。
// 写不进去不算失败:代价只是成员删掉快捷方式之后下次又会冒出来。
func writeMarker(cfgDir, note string) {
	marker := filepath.Join(cfgDir, shortcutMarker)
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		slog.Warn("桌面快捷方式:标记写不进去", "path", marker, "err", err)
	} else if err := os.WriteFile(marker, []byte(note), 0o644); err != nil {
		slog.Warn("桌面快捷方式:标记写不进去", "path", marker, "err", err)
	}
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
	slGetWorkingDirectory = 8
	slSetWorkingDirectory = 9
	slGetIconLocation     = 16
	slSetIconLocation     = 17
	slSetPath             = 20

	pfLoad = 5
	pfSave = 6

	stgmRead      = 0
	stgmReadWrite = 2
)

// comObject 是 COM 接口指针指向的东西,第一个字段是虚表指针。
// 数组长度只是给下标用的上限,不会真去读超出虚表的部分。
type comObject struct{ vtbl *[32]uintptr }

// hrError 是一个失败的 HRESULT。
type hrError struct {
	what string
	hr   uint32
}

func (e *hrError) Error() string { return fmt.Sprintf("%s: HRESULT 0x%08X", e.what, e.hr) }

func hresult(what string, r uintptr) error {
	if int32(r) < 0 {
		return &hrError{what, uint32(r)}
	}
	return nil
}

// isAccessDenied:E_ACCESSDENIED,也就是 HRESULT_FROM_WIN32(ERROR_ACCESS_DENIED)。
func isAccessDenied(err error) bool {
	var h *hrError
	return errors.As(err, &h) && h.hr == 0x80070005
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

// 下面几个 Get 方法把字符串写进调用方给的缓冲区,没有值时返回 S_FALSE、缓冲区是空串。
//
// 指向 Go 内存的参数一律直接写在 syscall.SyscallN 的参数列表里:编译器只对那个
// 位置的 uintptr(unsafe.Pointer(…)) 保证对象逃逸到堆上、调用期间不被回收。
// 先拼进一个 []uintptr 再传的话,64KB 的缓冲区可能留在栈上,栈一扩容就挪走了。

// linkPath 不传 WIN32_FIND_DATA、标志位 0:只要存着的路径,不去碰目标文件。
func (o *comObject) linkPath() (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	r, _, _ := syscall.SyscallN(o.vtbl[slGetPath], uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0)
	return bufString("IShellLink.GetPath", r, buf)
}

func (o *comObject) linkWorkingDirectory() (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	r, _, _ := syscall.SyscallN(o.vtbl[slGetWorkingDirectory], uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return bufString("IShellLink.GetWorkingDirectory", r, buf)
}

func (o *comObject) linkIconLocation() (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	var idx int32
	r, _, _ := syscall.SyscallN(o.vtbl[slGetIconLocation], uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&idx)))
	return bufString("IShellLink.GetIconLocation", r, buf)
}

func bufString(what string, r uintptr, buf []uint16) (string, error) {
	if err := hresult(what, r); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf), nil
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

// retargetShortcut 把已有的 lnk 改成指向 exe(成员挪了 exe),别的尽量不动。
//
// 成员可能在快捷方式上加过参数(-device、-server)、勾过"以管理员身份运行"
// (Npcap 装成仅管理员模式时就靠它抓包)、设过快捷键或运行方式。新建一个对象
// 整个覆盖会把这些全抹掉,抓包悄悄变成 0 张网卡,成员还看不出为什么。
// 所以先把现有的读进来,只改目标;工作目录和图标原来跟着旧 exe 走的才跟着改,
// 成员自己换过的不动。读不进来(文件坏了、不是快捷方式)才从头写。
func retargetShortcut(lnk, exe string) error {
	loaded := false
	err := withShellLink(func(sl, pf *comObject) error {
		if pf.callStr(pfLoad, "IPersistFile.Load", lnk, stgmReadWrite) != nil {
			return nil // 交给下面从头写
		}
		loaded = true
		// 读不出来就当空的:空的工作目录和图标照样改成跟着新 exe
		oldExe, _ := sl.linkPath()
		oldDir, _ := sl.linkWorkingDirectory()
		oldIcon, _ := sl.linkIconLocation()
		if err := sl.callStr(slSetPath, "IShellLink.SetPath", exe); err != nil {
			return err
		}
		if oldDir == "" || samePath(oldDir, filepath.Dir(oldExe)) {
			if err := sl.callStr(slSetWorkingDirectory, "IShellLink.SetWorkingDirectory", filepath.Dir(exe)); err != nil {
				return err
			}
		}
		if oldIcon == "" || samePath(oldIcon, oldExe) {
			if err := sl.callStr(slSetIconLocation, "IShellLink.SetIconLocation", exe, 0); err != nil {
				return err
			}
		}
		return pf.callStr(pfSave, "IPersistFile.Save", lnk, 1)
	})
	if err == nil && !loaded {
		slog.Warn("桌面快捷方式:读不进现有的,重写", "path", lnk)
		return writeShortcut(lnk, exe)
	}
	return err
}

// readShortcutTarget 读出 .lnk 指向的程序路径。
func readShortcutTarget(lnk string) (string, error) {
	var target string
	err := withShellLink(func(sl, pf *comObject) error {
		if err := pf.callStr(pfLoad, "IPersistFile.Load", lnk, stgmRead); err != nil {
			return err
		}
		var err error
		target, err = sl.linkPath()
		return err
	})
	return target, err
}
