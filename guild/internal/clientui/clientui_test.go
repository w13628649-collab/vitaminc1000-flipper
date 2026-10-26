package clientui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// 主程序没报加密判定时,顶栏也要给一句准确的"未确认",不能是空白或"明文"
func TestStatus_没报加密判定时是未确认(t *testing.T) {
	got := statusOf(t, Status{})
	if got.MarketCrypto != "unknown" || got.MarketCryptoText != "市场数据:未确认(去市场刷一次)" {
		t.Fatalf("得到 %q / %q", got.MarketCrypto, got.MarketCryptoText)
	}
	got = statusOf(t, Status{MarketCrypto: "encrypted"})
	if got.MarketCryptoText != "市场数据:被加密,这个号的挂单传不出来" {
		t.Fatalf("文案按状态补,得到 %q", got.MarketCryptoText)
	}
}

// 界面点「我还在 X」:走路由和来源检查,交给主程序的 AssumeLocation;位置已经抓到了回 409
func TestLocation_确认位置(t *testing.T) {
	var got string
	known := false
	s := &Server{AssumeLocation: func(loc string) (int, bool) {
		if known {
			return 0, false
		}
		got, known = loc, true
		return 7, true
	}}
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:5000/local/location", strings.NewReader(body))
		req.Header.Set("Origin", "http://127.0.0.1:5000")
		rec := httptest.NewRecorder()
		s.routes().ServeHTTP(rec, req)
		return rec
	}
	rec := post(`{"location":"1002"}`)
	var res struct {
		City string `json:"city"`
		Sent int    `json:"sent"`
	}
	if rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&res) != nil || got != "1002" ||
		res.Sent != 7 || res.City != "Lymhurst · 市场" {
		t.Fatalf("确认要成功、补发 7 条:%d %+v %q", rec.Code, res, got)
	}
	if rec := post(`{"location":"1002"}`); rec.Code != http.StatusConflict {
		t.Fatalf("位置已经知道了要回 409,得到 %d", rec.Code)
	}
	if rec := post(`{"location":""}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("空地点要回 400,得到 %d", rec.Code)
	}
}

func TestStatus_上次位置翻成地名(t *testing.T) {
	if got := statusOf(t, Status{LastLocation: "1002"}); got.LastCity != "Lymhurst · 市场" {
		t.Errorf("last_city = %q", got.LastCity)
	}
}

func TestStatus_主程序给了地名就不覆盖(t *testing.T) {
	if got := statusOf(t, Status{Location: "3008", City: "自定义"}); got.City != "自定义" {
		t.Errorf("city = %q,主程序填过的不该被覆盖", got.City)
	}
}
