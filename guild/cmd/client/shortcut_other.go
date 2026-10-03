//go:build !windows

package main

// ensureDesktopShortcut 只有 Windows 有。开发机上什么都不做。
func ensureDesktopShortcut() {}
