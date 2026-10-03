//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 全程在临时目录里:临时目录当桌面、当配置目录,绝不碰真桌面。

// tempRoot 是展开成长路径的 t.TempDir()。
//
// 用户名带中文或者很长时 %TEMP% 是 C:\Users\ABCDEF~1\... 这种 8.3 短路径。
// 生产代码对 exe 和临时目录两边都做了 longPath;测试里拼出来的路径也得是长路径,
// 不然"exe 在不在临时目录里"两边对不上,该跳过的就去真建了。
func tempRoot(t *testing.T) string {
	t.Helper()
	return longPath(t.TempDir()) // TempDir 返回时目录已经在了,GetLongPathName 才展得开
}

func fakeExe(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("MZ"), 0o755); err != nil {
		t.Fatal(err)
	}
	return longPath(p)
}

func assertTarget(t *testing.T, lnk, want string) {
	t.Helper()
	got, err := readShortcutTarget(lnk)
	if err != nil {
		t.Fatalf("读 %s: %v", lnk, err)
	}
	if !samePath(longPath(got), want) {
		t.Fatalf("快捷方式指向 %q,想要 %q", got, want)
	}
}

func TestTempRootIsLong(t *testing.T) {
	root := tempRoot(t)
	if longPath(root) != root {
		t.Fatalf("tempRoot 没展开:%q", root)
	}
}

func TestWriteShortcutRoundTrip(t *testing.T) {
	root := tempRoot(t)
	exe := fakeExe(t, filepath.Join(root, "带 空格 的目录"), releaseExeName)
	lnk := filepath.Join(root, shortcutFile)

	if err := writeShortcut(lnk, exe); err != nil {
		t.Fatal(err)
	}
	assertTarget(t, lnk, exe)

	// 已有的 .lnk 直接覆盖
	other := fakeExe(t, filepath.Join(root, "别处"), releaseExeName)
	if err := writeShortcut(lnk, other); err != nil {
		t.Fatal(err)
	}
	assertTarget(t, lnk, other)

	// 不是快捷方式的文件读不出目标,而不是读出垃圾
	junk := filepath.Join(root, "坏的.lnk")
	if err := os.WriteFile(junk, []byte("not a link"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := readShortcutTarget(junk); err == nil {
		t.Fatalf("坏文件居然读出了目标 %q", got)
	}
}

func TestSyncShortcutLifecycle(t *testing.T) {
	root := tempRoot(t)
	desktop := filepath.Join(root, "Desktop")
	cfg := filepath.Join(root, "AlbionFlipper")
	// t.TempDir 本身就在系统临时目录里;另给一个不相干的目录当"临时目录",
	// 不然每个 exe 都会被当成从压缩包里双击的
	notTemp := filepath.Join(root, "not-temp")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	lnk := filepath.Join(desktop, shortcutFile)
	marker := filepath.Join(cfg, shortcutMarker)

	sync := func(exe string, want shortcutOp) {
		t.Helper()
		op, err := syncShortcut(exe, notTemp, desktop, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if op != want {
			t.Fatalf("syncShortcut(%s) = %v,想要 %v", exe, op, want)
		}
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	exe := fakeExe(t, filepath.Join(root, "dist"), releaseExeName)
	sync(exe, opCreate)
	assertTarget(t, lnk, exe)
	if !exists(marker) {
		t.Fatal("建完没写标记")
	}
	sync(exe, opNone) // 第二次启动什么都不做

	// dev-up 部署时运行中的旧 exe 被改名,它不该去动快捷方式
	old := fakeExe(t, filepath.Join(root, "dist"), releaseExeName+".old-120301")
	sync(old, opNone)
	assertTarget(t, lnk, exe)

	// 成员把 exe 挪了个地方
	moved := fakeExe(t, filepath.Join(root, "games"), releaseExeName)
	sync(moved, opUpdate)
	assertTarget(t, lnk, moved)

	// 浏览器里再下一次,存成了 "flipper-client (1).exe":快捷方式跟着新版本走
	redownload := fakeExe(t, filepath.Join(root, "games"), "flipper-client (1).exe")
	sync(redownload, opUpdate)
	assertTarget(t, lnk, redownload)
	sync(moved, opUpdate)

	// 成员删掉快捷方式:之后每次启动都不再建回来
	if err := os.Remove(lnk); err != nil {
		t.Fatal(err)
	}
	sync(moved, opNone)
	sync(moved, opNone)
	if exists(lnk) {
		t.Fatal("成员删掉的快捷方式又被建回来了")
	}

	// 删掉标记 = 想要回来
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	sync(moved, opCreate)
	assertTarget(t, lnk, moved)

	// 从临时目录运行(直接双击压缩包里的 exe)不碰快捷方式
	if err := os.Remove(lnk); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	inZip := fakeExe(t, filepath.Join(notTemp, "Temp1_flipper-client.zip"), releaseExeName)
	sync(inZip, opNone)
	if exists(lnk) || exists(marker) {
		t.Fatal("从临时目录运行时建了快捷方式")
	}
}

// 挪了 exe 之后改快捷方式,成员自己在上面做的设置都得留着。
func TestRetargetKeepsMemberSettings(t *testing.T) {
	root := tempRoot(t)
	a := fakeExe(t, filepath.Join(root, "dist"), releaseExeName)
	b := fakeExe(t, filepath.Join(root, "games"), releaseExeName)
	lnk := filepath.Join(root, shortcutFile)
	if err := writeShortcut(lnk, a); err != nil {
		t.Fatal(err)
	}

	// 成员加了参数、设了快捷键、勾了"以管理员身份运行"
	const args = `-device 3 -server http://10.0.0.2:18420`
	const hotkey = 0x0641 // Ctrl+Alt+A:低字节是虚拟键 'A',高字节 HOTKEYF_CONTROL|HOTKEYF_ALT
	editLink(t, lnk, func(sl *comObject) {
		must(t, sl.callStr(slSetArguments, "SetArguments", args))
		r, _, _ := syscall.SyscallN(sl.vtbl[slSetHotkey], uintptr(unsafe.Pointer(sl)), hotkey)
		must(t, hresult("SetHotkey", r))
		dl := dataList(t, sl)
		defer dl.release()
		flags := linkFlags(t, dl)
		r, _, _ = syscall.SyscallN(dl.vtbl[dlSetFlags], uintptr(unsafe.Pointer(dl)), uintptr(flags|sldfRunAsUser))
		must(t, hresult("SetFlags", r))
	})

	if err := retargetShortcut(lnk, b); err != nil {
		t.Fatal(err)
	}
	got := inspectLink(t, lnk)
	if !samePath(longPath(got.path), b) {
		t.Errorf("目标 %q,想要 %q", got.path, b)
	}
	if !samePath(got.dir, filepath.Dir(b)) {
		t.Errorf("工作目录 %q 没跟着 exe 走", got.dir)
	}
	if !samePath(got.icon, b) {
		t.Errorf("图标 %q 没跟着 exe 走", got.icon)
	}
	if got.args != args {
		t.Errorf("参数丢了:%q", got.args)
	}
	if got.hotkey != hotkey {
		t.Errorf("快捷键丢了:0x%04x", got.hotkey)
	}
	if got.flags&sldfRunAsUser == 0 {
		t.Error("「以管理员身份运行」被抹掉了")
	}

	// 成员自己换过的工作目录和图标不动,只改目标
	customDir := filepath.Join(root, "work")
	customIcon := filepath.Join(os.Getenv("SystemRoot"), "System32", "shell32.dll")
	editLink(t, lnk, func(sl *comObject) {
		must(t, sl.callStr(slSetWorkingDirectory, "SetWorkingDirectory", customDir))
		must(t, sl.callStr(slSetIconLocation, "SetIconLocation", customIcon, 12))
	})
	if err := retargetShortcut(lnk, a); err != nil {
		t.Fatal(err)
	}
	got = inspectLink(t, lnk)
	if !samePath(longPath(got.path), a) || !samePath(got.dir, customDir) || !samePath(got.icon, customIcon) {
		t.Errorf("改目标时动了成员的设置:%+v", got)
	}

	// 坏文件读不进来:从头写一个
	junk := filepath.Join(root, "坏的.lnk")
	if err := os.WriteFile(junk, []byte("not a link"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := retargetShortcut(junk, a); err != nil {
		t.Fatal(err)
	}
	assertTarget(t, junk, a)
}

// 桌面不让写(受控文件夹访问)时只试一次:记下标记,之后启动不再重试。
// 这里用一个拒绝"新建文件"的 ACL 模拟,只加在测试自己的临时目录上。
func TestCreateDeniedIsNotRetried(t *testing.T) {
	root := tempRoot(t)
	desktop := filepath.Join(root, "Desktop")
	cfg := filepath.Join(root, "AlbionFlipper")
	if err := os.MkdirAll(desktop, 0o755); err != nil {
		t.Fatal(err)
	}
	setDACL(t, desktop, "D:P(D;;0x2;;;WD)(A;OICI;FA;;;WD)") // 拒绝 FILE_ADD_FILE
	t.Cleanup(func() { setDACL(t, desktop, "D:P(A;OICI;FA;;;WD)") })

	exe := fakeExe(t, filepath.Join(root, "dist"), releaseExeName)
	notTemp := filepath.Join(root, "not-temp")
	op, err := syncShortcut(exe, notTemp, desktop, cfg)
	if op != opCreate || !isAccessDenied(err) {
		t.Fatalf("syncShortcut = %v, %v;想要 opCreate + 拒绝访问", op, err)
	}
	if _, err := os.Stat(filepath.Join(cfg, shortcutMarker)); err != nil {
		t.Fatal("被拒之后没写标记,下次启动还会再试、再弹通知")
	}
	if op, err := syncShortcut(exe, notTemp, desktop, cfg); op != opNone || err != nil {
		t.Fatalf("第二次启动 = %v, %v;想要什么都不做", op, err)
	}
}

// ── 测试才用到的 COM 调用 ──────────────────────────────────────────────

const (
	slGetArguments = 10
	slSetArguments = 11
	slGetHotkey    = 12
	slSetHotkey    = 13

	dlGetFlags    = 6
	dlSetFlags    = 7
	sldfRunAsUser = 0x2000
)

var iidIShellLinkDataList = windows.GUID{Data1: 0x45e2b4ae, Data2: 0xb1c3, Data3: 0x11d0,
	Data4: [8]byte{0xb9, 0x2f, 0x00, 0xa0, 0xc9, 0x03, 0x12, 0xe1}}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// 指向 Go 内存的参数直接写在 SyscallN 的参数列表里,理由见 shortcut_windows.go。

func linkFlags(t *testing.T, dl *comObject) uint32 {
	t.Helper()
	var flags uint32
	r, _, _ := syscall.SyscallN(dl.vtbl[dlGetFlags], uintptr(unsafe.Pointer(dl)), uintptr(unsafe.Pointer(&flags)))
	must(t, hresult("IShellLinkDataList.GetFlags", r))
	return flags
}

func linkArguments(sl *comObject) (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	r, _, _ := syscall.SyscallN(sl.vtbl[slGetArguments], uintptr(unsafe.Pointer(sl)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return bufString("IShellLink.GetArguments", r, buf)
}

func linkHotkey(sl *comObject) (uint16, error) {
	var hk uint16
	r, _, _ := syscall.SyscallN(sl.vtbl[slGetHotkey], uintptr(unsafe.Pointer(sl)), uintptr(unsafe.Pointer(&hk)))
	return hk, hresult("IShellLink.GetHotkey", r)
}

func dataList(t *testing.T, sl *comObject) *comObject {
	t.Helper()
	var dl *comObject
	r, _, _ := syscall.SyscallN(sl.vtbl[vtQueryInterface], uintptr(unsafe.Pointer(sl)),
		uintptr(unsafe.Pointer(&iidIShellLinkDataList)), uintptr(unsafe.Pointer(&dl)))
	must(t, hresult("QueryInterface(IShellLinkDataList)", r))
	return dl
}

// editLink 读进 lnk、交给 fn 改、存回去。
func editLink(t *testing.T, lnk string, fn func(sl *comObject)) {
	t.Helper()
	must(t, withShellLink(func(sl, pf *comObject) error {
		if err := pf.callStr(pfLoad, "IPersistFile.Load", lnk, stgmReadWrite); err != nil {
			return err
		}
		fn(sl)
		return pf.callStr(pfSave, "IPersistFile.Save", lnk, 1)
	}))
}

type linkInfo struct {
	path, dir, icon, args string
	hotkey                uint16
	flags                 uint32
}

func inspectLink(t *testing.T, lnk string) (li linkInfo) {
	t.Helper()
	must(t, withShellLink(func(sl, pf *comObject) error {
		if err := pf.callStr(pfLoad, "IPersistFile.Load", lnk, stgmRead); err != nil {
			return err
		}
		var err error
		if li.path, err = sl.linkPath(); err != nil {
			return err
		}
		if li.dir, err = sl.linkWorkingDirectory(); err != nil {
			return err
		}
		if li.icon, err = sl.linkIconLocation(); err != nil {
			return err
		}
		if li.args, err = linkArguments(sl); err != nil {
			return err
		}
		if li.hotkey, err = linkHotkey(sl); err != nil {
			return err
		}
		dl := dataList(t, sl)
		defer dl.release()
		li.flags = linkFlags(t, dl)
		return nil
	}))
	return li
}

func setDACL(t *testing.T, path, sddl string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(sddl)
	must(t, err)
	dacl, _, err := sd.DACL()
	must(t, err)
	must(t, windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil))
}
