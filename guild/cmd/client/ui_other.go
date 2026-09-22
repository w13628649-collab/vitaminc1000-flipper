//go:build !windows

package main

import (
	"context"
	"os"
	"path/filepath"
)

// showWindow 只有 Windows 有内嵌窗口。开发机上退回浏览器就行,
// 成员用的都是 Windows。
func showWindow(ctx context.Context, title, url string) bool { return false }

func alert(title, body string) {}

func configDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "."
	}
	return filepath.Join(dir, "AlbionFlipper")
}
