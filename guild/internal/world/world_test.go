package world

import (
	"strings"
	"testing"
)

// 前五个是 2026-09-21 实抓数据里真实出现过的 location_id
func TestCity_市场id收敛成AODP的城市名(t *testing.T) {
	for raw, want := range map[string]string{
		"0007": "Thetford", "3008": "Martlock", "4002": "Fort Sterling",
		"2004": "Bridgewatch", "1002": "Lymhurst",
		"3005": "Caerleon", "3013-Auction2": "Caerleon", "5003": "Brecilien",
	} {
		if got := City(raw); got != want {
			t.Errorf("City(%q) = %q,应为 %q", raw, got, want)
		}
	}
}

func TestCity_认不出的原样放行(t *testing.T) {
	// 黑市、走私窝点不和 AODP 合并,但绝不能被错并到别的城市去
	for _, raw := range []string{"3003", "3005@0", "0000", "", "Black Market"} {
		if got := City(raw); got != raw {
			t.Errorf("City(%q) = %q,认不出的应该原样返回", raw, got)
		}
	}
}

func TestDisplayName_市场和城市本体都认(t *testing.T) {
	for raw, want := range map[string]string{
		"0000": "Thetford", "0007": "Thetford · 市场",
		"1000": "Lymhurst", "1002": "Lymhurst · 市场",
		"2000": "Bridgewatch", "2004": "Bridgewatch · 市场",
		"3004": "Martlock", "3008": "Martlock · 市场",
		"4000": "Fort Sterling", "4002": "Fort Sterling · 市场",
		"3003": "Caerleon", "3005": "Caerleon · 市场", "3013-Auction2": "Caerleon · 市场",
		"5000": "Brecilien", "5001": "Brecilien", "5003": "Brecilien · 市场",
	} {
		if got := DisplayName(raw); got != want {
			t.Errorf("DisplayName(%q) = %q,应为 %q", raw, got, want)
		}
	}
}

func TestDisplayName_认不出的原样返回(t *testing.T) {
	// 走私窝点、休息区这种 xxxx@yyyy 拿不准是哪,宁可原样显示
	for _, raw := range []string{"", "3005@0", "9999", "Black Market", "Thetford"} {
		if got := DisplayName(raw); got != raw {
			t.Errorf("DisplayName(%q) = %q,认不出的应该原样返回", raw, got)
		}
	}
}

// 每个能收敛的市场 id,显示名都要以它收敛成的城市名开头:
// 顶栏写的城市和扫描里用的城市对不上,成员会以为抓包串城了
func TestDisplayName_和City口径一致(t *testing.T) {
	for raw, city := range markets {
		if got := DisplayName(raw); !strings.HasPrefix(got, city) {
			t.Errorf("DisplayName(%q) = %q,应以 City 的结果 %q 开头", raw, got, city)
		}
	}
}

// DisplayName 收了城市本体 id,City 不能因此跟着变:0000/3003 并进城市会让
// 黑市求购冒充 Caerleon 的买单
func TestDisplayName_不影响City(t *testing.T) {
	for _, raw := range []string{"0000", "3003", "5000", "0006", "0301"} {
		if DisplayName(raw) == raw {
			t.Fatalf("前提不成立:DisplayName(%q) 应该认得", raw)
		}
		if got := City(raw); got != raw {
			t.Errorf("City(%q) = %q,城市本体 id 必须原样放行", raw, got)
		}
	}
}

func TestCity_重复收敛不会坏(t *testing.T) {
	for _, c := range []string{"Thetford", "Martlock", "Fort Sterling", "Caerleon"} {
		if got := City(City(c)); got != c {
			t.Errorf("City(City(%q)) = %q", c, got)
		}
	}
}
