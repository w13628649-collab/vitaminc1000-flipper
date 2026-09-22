//go:build !windows

// 本文件来自 ao-data/albiondata-client,MIT License,
// Copyright (c) 2017 The Albion Data Project。
// 许可证全文见仓库根目录 licenses/albiondata-client-LICENSE。
package pcapdriver

// Check always returns the zero Warning outside Windows - macOS and
// Linux use libpcap directly with no separate driver-install step to
// detect.
func Check() Warning { return Warning{} }
