package clientui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func statusOf(t *testing.T, st Status) Status {
	t.Helper()
	s := &Server{Status: func() Status { return st }}
	rec := httptest.NewRecorder()
	s.handleStatus(rec, httptest.NewRequest(http.MethodGet, "/local/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 %d", rec.Code)
	}
	var got Status
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("解码失败: %v", err)
	}
	return got
}

// 顶栏以前直接显示原始 id,成员看到的是 "3008" 而不是 Martlock
func TestStatus_地点id翻成地名(t *testing.T) {
	for raw, want := range map[string]string{
		"3008": "Martlock · 市场",
		"0000": "Thetford",
		"5003": "Brecilien · 市场",
	} {
		got := statusOf(t, Status{Character: "a", Location: raw})
		if got.City != want {
			t.Errorf("location %q → city %q,应为 %q", raw, got.City, want)
		}
		if got.Location != raw {
			t.Errorf("location 要原样保留给排查用,得到 %q", got.Location)
		}
	}
}

func TestStatus_认不出的地点原样给出(t *testing.T) {
	for _, raw := range []string{"", "3005@0", "9999"} {
		if got := statusOf(t, Status{Location: raw}); got.City != raw {
			t.Errorf("location %q → city %q,认不出的应原样给出", raw, got.City)
		}
	}
}

func TestStatus_主程序给了地名就不覆盖(t *testing.T) {
	if got := statusOf(t, Status{Location: "3008", City: "自定义"}); got.City != "自定义" {
		t.Errorf("city = %q,主程序填过的不该被覆盖", got.City)
	}
}
