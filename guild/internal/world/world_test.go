package world

import "testing"

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

func TestCity_重复收敛不会坏(t *testing.T) {
	for _, c := range []string{"Thetford", "Martlock", "Fort Sterling", "Caerleon"} {
		if got := City(City(c)); got != c {
			t.Errorf("City(City(%q)) = %q", c, got)
		}
	}
}
