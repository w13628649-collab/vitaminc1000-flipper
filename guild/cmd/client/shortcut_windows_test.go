//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// 全程在临时目录里:临时目录当桌面、当配置目录,绝不碰真桌面。

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

func TestWriteShortcutRoundTrip(t *testing.T) {
	root := t.TempDir()
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
	root := t.TempDir()
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
