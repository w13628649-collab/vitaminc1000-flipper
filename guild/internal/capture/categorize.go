// 网卡分类改写自 Nouuu/Albion-Online-OpenRadar 的 internal/capture/categorize.go,
// MIT License,Copyright (c) 2025 Nouuu。许可证全文见仓库根目录 licenses/openradar-LICENSE。

package capture

import (
	"regexp"
	"strings"
)

// Category 是按网卡名和描述认出来的类别。只用来排序和给提示,不决定抓不抓。
type Category string

const (
	CategoryEthernet Category = "ethernet"
	CategoryWiFi     Category = "wifi"
	CategoryExitLag  Category = "exitlag"
	CategoryVPN      Category = "vpn"
	CategoryVirtual  Category = "virtual"
	CategoryOther    Category = "other"
)

// 顺序有讲究:虚拟网卡先判,否则 "Hyper-V Virtual Ethernet" 会被当成有线;
// ExitLag 先于 VPN,它的适配器名里常带 tap/tun
var categoryRules = []struct {
	cat Category
	re  *regexp.Regexp
}{
	{CategoryVirtual, regexp.MustCompile(`virtualbox|vmware|hyper-v|virtual switch|vethernet|teredo|loopback pseudo|software loopback|wi-fi direct|mobile hotspot|\bdocker\d|\bbr-|\bvirbr\d|\bvmnet\d|\bveth|\blo\b`)},
	{CategoryExitLag, regexp.MustCompile(`exit\s*lag`)},
	{CategoryVPN, regexp.MustCompile(`vpn|wireguard|wintun|tap-windows|openvpn|\btun\d|\btap\d|\bwg\d|\bppp\d`)},
	{CategoryWiFi, regexp.MustCompile(`wi-?fi|wireless|802\.11|\bwlan\d|\bwlp\d|\bwifi\d`)},
	{CategoryEthernet, regexp.MustCompile(`ethernet|gigabit|family controller|\beth\d|\benp\d|\beno\d|\bens\d`)},
}

// Categorize 按名字和描述给网卡归类,认不出的是 other。
func Categorize(name, description string) Category {
	hay := strings.ToLower(name + " " + description)
	if strings.TrimSpace(hay) == "" {
		return CategoryOther
	}
	for _, r := range categoryRules {
		if r.re.MatchString(hay) {
			return r.cat
		}
	}
	return CategoryOther
}

// ExitLagHint 是检测到 ExitLag 网卡时给用户的提示。
//
// ExitLag 默认的转发方式(WFP)在 NDIS 层之上截走 Albion 的流量,Npcap 看不到,
// Wireshark 也看不到。表现是"游戏开着、一条包都没有"
const ExitLagHint = "检测到 ExitLag。如果游戏开着却抓不到包:在 ExitLag 里打开 " +
	"Settings → Advanced → Packet redirection method,改成 NDIS (Legacy)"

// HasExitLag 判断这批网卡里有没有 ExitLag 的。
func HasExitLag(devs []Device) bool {
	for _, d := range devs {
		if d.Category == CategoryExitLag {
			return true
		}
	}
	return false
}
