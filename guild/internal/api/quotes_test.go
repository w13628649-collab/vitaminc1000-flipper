package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"albion-guild/internal/hub"
	"albion-guild/internal/model"
)

type fakeQuotes struct {
	keys []model.QuoteKey
	out  []model.Quote
}

func (f *fakeQuotes) BestQuotes(_ context.Context, keys []model.QuoteKey, _ time.Duration) ([]model.Quote, error) {
	f.keys = keys
	return f.out, nil
}

// /api/quotes 是界面重连后补快照用的,必须和 WS 推送同一个来源(Server.Quotes),
// 不能再直接查库:以前那条 SQL 没有幽灵剔除,补回来的快照和之后的推送对不上
func TestQuotes_走和推送同一个来源(t *testing.T) {
	src := &fakeQuotes{out: []model.Quote{{Key: "T5_CLOTH|Lymhurst|1|0", Price: 2905, Depth: 20, Orders: 2, At: time.Now()}}}
	s := New(nil, nil, hub.NewHub(), time.Minute, nil)
	s.Quotes = src
	srv := httptest.NewServer(s.Routes())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/quotes?keys=T5_CLOTH|Lymhurst|1|0")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got []model.Quote
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Price != 2905 || len(src.keys) != 1 || src.keys[0].LocationID != "Lymhurst" {
		t.Fatalf("应原样返回来源给的报价,得到 %+v(来源收到 %+v)", got, src.keys)
	}

	// 没有来源(纯行情中转、没接 flip)时说清楚,不 panic
	bare := httptest.NewServer(New(nil, nil, hub.NewHub(), time.Minute, nil).Routes())
	defer bare.Close()
	resp2, err := http.Get(bare.URL + "/api/quotes?keys=T5_CLOTH|Lymhurst|1|0")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("没有报价来源应返回 503,得到 %d", resp2.StatusCode)
	}
}
