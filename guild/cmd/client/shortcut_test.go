package main

import (
	"path/filepath"
	"testing"
)

func TestShortcutEligible(t *testing.T) {
	root := t.TempDir() // 只拿来拼路径,不建文件
	tmp := filepath.Join(root, "Temp")
	cases := []struct {
		name string
		exe  string
		want bool
	}{
		{"正式文件名", filepath.Join(root, "dist", "flipper-client.exe"), true},
		{"大小写不同也算", filepath.Join(root, "dist", "Flipper-Client.EXE"), true},
		{"Chrome / Edge 重名下载", filepath.Join(root, "Downloads", "flipper-client (1).exe"), true},
		{"Firefox 重名下载", filepath.Join(root, "Downloads", "flipper-client(12).exe"), true},
		{"成员自己复制的", filepath.Join(root, "Downloads", "flipper-client - 副本.exe"), false},
		{"序号后面又改了名", filepath.Join(root, "Downloads", "flipper-client (1).exe.old-120301"), false},
		{"括号里不是序号", filepath.Join(root, "Downloads", "flipper-client (old).exe"), false},
		{"dev-up 改名的旧版本", filepath.Join(root, "dist", "flipper-client.exe.old-120301"), false},
		{"go run 的临时产物", filepath.Join(tmp, "go-build123", "b001", "exe", "client.exe"), false},
		{"本地测试构建", filepath.Join(root, "scratch", "flipper-client-test.exe"), false},
		{"从压缩包里直接双击", filepath.Join(tmp, "Temp1_flipper-client.zip", "flipper-client.exe"), false},
		{"临时目录名只是前缀相同", filepath.Join(root, "TempTools", "flipper-client.exe"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ok, why := shortcutEligible(c.exe, tmp)
			if ok != c.want {
				t.Fatalf("shortcutEligible(%q) = %v (%s),想要 %v", c.exe, ok, why, c.want)
			}
			if !ok && why == "" {
				t.Fatal("跳过时要给原因,日志里要看")
			}
		})
	}
	// 拿不到临时目录时不该把什么都判成"在临时目录里"
	if ok, _ := shortcutEligible(filepath.Join(root, "flipper-client.exe"), ""); !ok {
		t.Fatal("tempDir 为空时正式文件名应该放行")
	}
}

func TestPlanShortcut(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "dist", "flipper-client.exe")
	moved := filepath.Join(root, "games", "flipper-client.exe")
	cases := []struct {
		name   string
		link   linkState
		marked bool
		want   shortcutOp
	}{
		{"第一次运行", linkState{}, false, opCreate},
		{"成员删掉了快捷方式", linkState{}, true, opNone},
		{"一切正常", linkState{true, exe}, true, opNone},
		{"路径大小写不同也算同一个", linkState{true, filepath.Join(root, "DIST", "FLIPPER-CLIENT.exe")}, true, opNone},
		{"成员挪了 exe", linkState{true, moved}, true, opUpdate},
		{"挪了 exe 且没有标记", linkState{true, moved}, false, opUpdate},
		{"读不出目标就重写", linkState{true, ""}, true, opUpdate},
		{"快捷方式对、标记丢了", linkState{true, exe}, false, opMarkOnly},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := planShortcut(exe, c.link, c.marked); got != c.want {
				t.Fatalf("planShortcut(%+v, marked=%v) = %v,想要 %v", c.link, c.marked, got, c.want)
			}
		})
	}
}

func TestWithin(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a")
	cases := []struct {
		path string
		want bool
	}{
		{filepath.Join(dir, "x.exe"), true},
		{filepath.Join(dir, "b", "c", "x.exe"), true},
		{filepath.Join(root, "ab", "x.exe"), false},
		{filepath.Join(root, "x.exe"), false},
		{dir, true},
	}
	for _, c := range cases {
		if got := within(c.path, dir); got != c.want {
			t.Errorf("within(%q, %q) = %v,想要 %v", c.path, dir, got, c.want)
		}
	}
}
