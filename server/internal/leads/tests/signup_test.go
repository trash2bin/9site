// Тесты публичной формы заявок (POST /signup) через настоящий leads.Register:
// Origin, honeypot, лимитер, валидация и PRG. Проводка маршрутов — боевая,
// поэтому тест ловит и ошибку сборки, а не только в обработчике.
package leads_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/leads"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

const (
	testOrigin = "http://example.com"
	testLimit  = 5
)

// testServer поднимает срез заявок с реальной базой.
func testServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	log, err := logging.New(logging.Config{Output: io.Discard})
	if err != nil {
		t.Fatal(err)
	}

	limits, err := webutil.NewLimits(map[string]webutil.Budget{
		leads.BudgetSignup: {Limit: testLimit, Window: 10 * time.Minute},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	srv, err := leads.New(leads.Config{
		Store:    st,
		BaseURL:  testOrigin,
		Log:      log,
		Throttle: limits,
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	srv.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ts, st
}

// postSignup отправляет форму; дописывает заголовки поверх формы.
func postSignup(t *testing.T, ts *httptest.Server, form url.Values, header http.Header) *http.Response {
	t.Helper()

	req, err := http.NewRequest("POST", ts.URL+"/signup", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range header {
		req.Header[k] = v
	}

	// За редиректами не следуем: проверяем сами — как браузерный клиент
	// в auth-тестах.
	c := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func leadCount(t *testing.T, st *store.Store) int {
	t.Helper()
	all, err := st.ListLeads(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListLeads: %v", err)
	}
	return len(all)
}

func TestSignupValid(t *testing.T) {
	ts, st := testServer(t)

	res := postSignup(t, ts, url.Values{
		"name":  {"Иван Тестов"},
		"phone": {"+7 900 123-45-67"},
		"email": {"ivan@example.com"},
	}, http.Header{"Origin": {testOrigin}})

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("статус %d, ожидался 303", res.StatusCode)
	}
	if got := res.Header.Get("Location"); got != "/maket?sent=1#contact" {
		t.Fatalf("редирект %q, ожидался /maket?sent=1#contact", got)
	}
	if n := leadCount(t, st); n != 1 {
		t.Fatalf("заявок %d, ожидалась 1", n)
	}
}

func TestSignupHoneypot(t *testing.T) {
	ts, st := testServer(t)

	// Бот заполняет скрытое поле: отвечаем успехом, но не сохраняем.
	res := postSignup(t, ts, url.Values{
		"name":    {"Bot"},
		"phone":   {"+79001234567"},
		"email":   {"bot@spam.com"},
		"company": {"SpamCo"},
	}, nil)

	if got := res.Header.Get("Location"); got != "/maket#contact" {
		t.Fatalf("редирект %q, ожидался /maket#contact без «спасибо»", got)
	}
	if n := leadCount(t, st); n != 0 {
		t.Fatalf("бот не должен оставлять заявки, а их %d", n)
	}
}

func TestSignupForeignOrigin(t *testing.T) {
	ts, st := testServer(t)

	res := postSignup(t, ts, url.Values{
		"name":  {"X"},
		"phone": {"+79001234567"},
		"email": {"x@x.com"},
	}, http.Header{"Origin": {"http://evil.example"}})

	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("статус %d, ожидался 403", res.StatusCode)
	}
	if n := leadCount(t, st); n != 0 {
		t.Fatalf("заявка с чужого Origin не должна сохраняться")
	}
}

func TestSignupInvalid(t *testing.T) {
	ts, st := testServer(t)

	res := postSignup(t, ts, url.Values{
		"name":  {"X"},
		"phone": {"совсем не телефон"},
		"email": {"не почта"},
	}, nil)

	if got := res.Header.Get("Location"); got != "/maket#contact" {
		t.Fatalf("редирект %q, ожидался /maket#contact", got)
	}
	if n := leadCount(t, st); n != 0 {
		t.Fatalf("невалидная заявка не должна сохраняться")
	}
}

func TestSignupRateLimit(t *testing.T) {
	ts, _ := testServer(t)

	// Жжём лимит невалидными заявками (лимит — testLimit за окно).
	var last *http.Response
	for i := 0; i <= testLimit; i++ {
		last = postSignup(t, ts, url.Values{"name": {"X"}}, nil)
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("статус %d, ожидался 429 после исчерпания лимита", last.StatusCode)
	}
}
