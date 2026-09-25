package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"albion-guild/internal/conf"
	"albion-guild/internal/flip"
)

// 路由能注册(和 GET /api/lookup 不冲突),参数校验在碰库之前就挡掉坏请求。
// Store 是 nil:走到读库那一步就会 panic,所以这里只测 400 的分支。
func TestLookupRoutesValidateParams(t *testing.T) {
	s := New(nil, nil, nil, 30*time.Minute, flip.New(nil, conf.Default()))
	mux := s.Routes()

	cases := []struct {
		url  string
		code int
	}{
		{"/api/lookup/grid", http.StatusBadRequest},
		{"/api/lookup/grid?item=%20", http.StatusBadRequest},
		{"/api/lookup/book?item=T4_LEATHER", http.StatusBadRequest},
		{"/api/lookup/book?city=Martlock", http.StatusBadRequest},
		{"/api/lookup/book?item=T4_LEATHER&city=Martlock&quality=9", http.StatusBadRequest},
		{"/api/lookup/book?item=T4_LEATHER&city=Martlock&quality=0", http.StatusBadRequest},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.url, nil))
		if rec.Code != c.code {
			t.Errorf("%s: 状态 %d,期望 %d(%s)", c.url, rec.Code, c.code, rec.Body.String())
		}
	}

	// 旧接口仍然挂着,没有被新路由吞掉
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/lookup", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("旧 /api/lookup 缺参数应是 400,得到 %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/lookup/grid?item=x", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("只接受 GET,得到 %d", rec.Code)
	}
}
