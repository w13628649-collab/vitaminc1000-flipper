package capture

import "testing"

func TestCategorize(t *testing.T) {
	for _, c := range []struct {
		name, desc string
		want       Category
	}{
		{`\Device\NPF_{A}`, "Realtek PCIe GbE Family Controller", CategoryEthernet},
		{`\Device\NPF_{B}`, "Intel(R) Wi-Fi 6 AX201 160MHz", CategoryWiFi},
		{`\Device\NPF_{C}`, "ExitLag Tap Adapter", CategoryExitLag},              // 带 tap 也先认成 ExitLag
		{`\Device\NPF_{D}`, "Hyper-V Virtual Ethernet Adapter", CategoryVirtual}, // 带 Ethernet 也先认成虚拟
		{`\Device\NPF_{E}`, "WireGuard Tunnel", CategoryVPN},
		{"wlan0", "", CategoryWiFi},
		{"enp3s0", "", CategoryEthernet},
		{"lo", "", CategoryVirtual},
		{`\Device\NPF_{F}`, "某个说不清的网卡", CategoryOther},
		{"", "", CategoryOther},
	} {
		if got := Categorize(c.name, c.desc); got != c.want {
			t.Errorf("Categorize(%q, %q) = %s,想要 %s", c.name, c.desc, got, c.want)
		}
	}
	if !HasExitLag([]Device{{Category: CategoryEthernet}, {Category: CategoryExitLag}}) ||
		HasExitLag([]Device{{Category: CategoryWiFi}}) {
		t.Fatal("HasExitLag")
	}
}
