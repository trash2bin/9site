// Package healthz_test - ответ проверки по экспортному API.
package healthz_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/healthz"
)

func TestHandlerReportsVersionAndSchema(t *testing.T) {
	srv := httptest.NewServer(healthz.Handler("abc123", 4))
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("запрос: %v", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("код %d, ждали 200", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("тип %q, ждали application/json", got)
	}

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Schema  int    `json:"schema"`
		Uptime  string `json:"uptime"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if body.Status != "ok" || body.Version != "abc123" || body.Schema != 4 {
		t.Fatalf("тело %+v, ждали ok, abc123, 4", body)
	}
	if body.Uptime == "" {
		t.Fatal("uptime пустой, проверка должна рассказывать и о нём")
	}
}
