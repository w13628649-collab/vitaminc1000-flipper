package flip

import (
	"testing"

	"albion-guild/internal/catalog"
)

// 生产库 9 月同步过 ao-bin-dumps、那时还没有 source 这一行。升级到带内置目录的版本后,
// 只在库空时装内置的话这种库永远换不过来——这组用例钉住什么时候换、什么时候保留。
func TestPreferBuiltin(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		syncedAt string
		replace  bool
	}{
		{"当前内置版本不换", "client/" + catalog.ClientVersion, "2026-10-17T08:00:00Z", false},
		{"别的客户端版本换", "client/1.31.000.000000", "2026-10-17T08:00:00Z", true},
		{"没记来源的老库换(生产库就是这种)", "", "2026-09-26T15:43:54Z", true},
		{"内置出现之前的 ao-bin-dumps 换", "ao-bin-dumps", "2026-09-26T15:43:54Z", true},
		{"内置解出当天之后手动同步的保留", "ao-bin-dumps", catalog.ClientExtractedAt + "T00:00:00Z", false},
		{"内置之后手动同步的保留", "ao-bin-dumps", "2026-10-20T10:00:00Z", false},
		{"ao-bin-dumps 但同步时刻读不出来就换", "ao-bin-dumps", "", true},
		{"不认识的来源换", "something-else", "2026-10-20T10:00:00Z", true},
	}
	for _, c := range cases {
		got, why := preferBuiltin(c.source, c.syncedAt)
		if got != c.replace {
			t.Errorf("%s:replace 应是 %v,得到 %v(%s)", c.name, c.replace, got, why)
		}
		if why == "" {
			t.Errorf("%s:要给出理由,日志里排查要看", c.name)
		}
	}
}
