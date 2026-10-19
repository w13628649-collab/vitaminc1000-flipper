package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"albion-guild/internal/conf"
	"albion-guild/internal/flip"
)

// 不连库、不连 AODP 的服务:只验参数解析和回包形状
func blackMarketServer() *httptest.Server {
	return httptest.NewServer(New(nil, nil, nil, 30*time.Minute, &flip.Service{Cfg: conf.Default()}).Routes())
}

func TestBlackMarket_参数和回包(t *testing.T) {
	srv := blackMarketServer()
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/blackmarket?bm_age=30&city_age=120&cities=Martlock,+Caerleon")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	var plan flip.BlackMarketPlan
	if err := json.Unmarshal(body, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.BMMaxAgeMin != 30 || plan.CityMaxAgeMin != 120 || !slices.Equal(plan.Cities, []string{"Martlock", "Caerleon"}) ||
		plan.Rows == nil || plan.Tax != conf.Default().Economics.MarketTax {
		t.Fatalf("回包: %s", body)
	}

	for _, q := range []string{"bm_age=abc", "city_age=0", "bm_age=-5", "cities=Atlantis", "cities=Black+Market", "bm_age=2000"} {
		resp, err := http.Get(srv.URL + "/api/blackmarket?" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s 该 400,得到 %d", q, resp.StatusCode)
		}
	}
}
