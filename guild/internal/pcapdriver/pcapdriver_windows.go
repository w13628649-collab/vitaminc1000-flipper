//go:build windows

// 本文件来自 ao-data/albiondata-client,MIT License,
// Copyright (c) 2017 The Albion Data Project。
// 许可证全文见仓库根目录 licenses/albiondata-client-LICENSE。
package pcapdriver

import (
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const npcapDownloadURL = "https://npcap.com/#download"

// npcapGuidance 是下面两种"去装一下"情形共用的说明。
//
// 默认推荐全用户模式:这样客户端不用以管理员身份跑就能抓包。
// 代价是这台机器上任何程序都能读网络流量、同样不需要管理员权限——
// 这个取舍说清楚,让用户自己选,不替他默默决定。
const npcapGuidance = "请安装 Npcap 来启用抓包。安装过程中,建议不要勾选 " +
	"\"Restrict Npcap driver's access to Administrators only\"(限制仅管理员访问)," +
	"这样本程序不用以管理员身份运行也能抓包;代价是这台电脑上其他程序也能读网络流量、" +
	"同样不需要管理员权限。如果你介意,就勾上它,但以后每次都要右键“以管理员身份运行”本程序。"

// Check 看一眼系统的抓包驱动。需要用户做点什么就返回 Warning,
// 一切正常就返回零值。
func Check() Warning {
	npcapPresent, npcapAdminOnly := npcapState()
	elevated := windows.GetCurrentProcessToken().IsElevated()
	return decide(npcapPresent, npcapAdminOnly, winPcapPresent(), elevated)
}

// decide 是 Check 的判断逻辑,从上面读注册表/令牌的部分里拆出来,
// 这样测试不依赖真实机器的驱动状态。
func decide(npcapPresent, npcapAdminOnly, winPcapPresent, processElevated bool) Warning {
	if npcapPresent {
		if npcapAdminOnly && !processElevated {
			return Warning{
				Message: "Npcap 装成了仅管理员模式,但本程序不是以管理员身份运行的。" +
					"这样抓不到任何数据。解决办法二选一:右键本程序选“以管理员身份运行”," +
					"或者重装 Npcap 时不勾选 \"Restrict Npcap driver's access to " +
					"Administrators only\"。",
			}
		}
		return Warning{}
	}

	if winPcapPresent {
		return Warning{
			Message: "只检测到老旧的 WinPcap 驱动。WinPcap 从 2013 年起就没人维护了," +
				"而且抓不到现在的 VPN 虚拟网卡(Wintun/WireGuard)。" + npcapGuidance,
			HelpURL: npcapDownloadURL,
		}
	}

	return Warning{
		Message: "没有检测到抓包驱动。" + npcapGuidance,
		HelpURL: npcapDownloadURL,
	}
}

// npcapState 报告 Npcap 装没装,以及是不是装成了仅管理员模式
// (注册表里的 AdminOnly 值)。WOW6432Node 那条路径也要查:
// 本进程可能是 64 位系统上跑的 32 位程序,Npcap 自己的安装器也这么查。
func npcapState() (present bool, adminOnly bool) {
	for _, path := range []string{`SOFTWARE\Npcap`, `SOFTWARE\WOW6432Node\Npcap`} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		v, _, verr := k.GetIntegerValue("AdminOnly")
		k.Close()
		return true, verr == nil && v != 0
	}
	return false, false
}

// winPcapPresent 报告老的 WinPcap 装没装。查它安装器自己的注册表键,
// 而不是某个版本特有的细节,这样不会绑死在 4.1.3 上。
func winPcapPresent() bool {
	for _, path := range []string{`SOFTWARE\WinPcap`, `SOFTWARE\WOW6432Node\WinPcap`} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err == nil {
			k.Close()
			return true
		}
	}
	return false
}
