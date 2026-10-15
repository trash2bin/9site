package main

// Тесты формы Sign Up (POST /signup) и фолбэка счётчика: ровно те места,
// которые ревью отметило как непокрытые рискованные — Origin, honeypot,
// лимитер, валидация, PRG и поведение главной страницы при сбое базы.

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

const testOrigin = "http://example.com"

// testApp поднимает минимальный сервер с формой Sign Up и реальной базой.
func testApp(t *testing.T) (*app, *httptest.Server) {
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

	a := &app{
		log:         log,
		store:       st,
		baseURL:     testOrigin,
		signupLimit: webutil.NewLimiter(signupLimit, signupWindow),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /signup", a.signupHandler)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return a, ts
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

func leadCount(t *testing.T, a *app) int {
	t.Helper()
	leads, err := a.store.ListLeads(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListLeads: %v", err)
	}
	return len(leads)
}

func TestSignupValid(t *testing.T) {
	a, ts := testApp(t)

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
	if n := leadCount(t, a); n != 1 {
		t.Fatalf("заявок %d, ожидалась 1", n)
	}
}

func TestSignupHoneypot(t *testing.T) {
	a, ts := testApp(t)

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
	if n := leadCount(t, a); n != 0 {
		t.Fatalf("бот не должен оставлять заявки, а их %d", n)
	}
}

func TestSignupForeignOrigin(t *testing.T) {
	a, ts := testApp(t)

	res := postSignup(t, ts, url.Values{
		"name":  {"X"},
		"phone": {"+79001234567"},
		"email": {"x@x.com"},
	}, http.Header{"Origin": {"http://evil.example"}})

	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("статус %d, ожидался 403", res.StatusCode)
	}
	if n := leadCount(t, a); n != 0 {
		t.Fatalf("заявка с чужого Origin не должна сохраняться")
	}
}

func TestSignupInvalid(t *testing.T) {
	a, ts := testApp(t)

	res := postSignup(t, ts, url.Values{
		"name":  {"X"},
		"phone": {"совсем не телефон"},
		"email": {"не почта"},
	}, nil)

	if got := res.Header.Get("Location"); got != "/maket#contact" {
		t.Fatalf("редирект %q, ожидался /maket#contact", got)
	}
	if n := leadCount(t, a); n != 0 {
		t.Fatalf("невалидная заявка не должна сохраняться")
	}
}

func TestSignupRateLimit(t *testing.T) {
	_, ts := testApp(t)

	// Жжём лимит невалидными заявками (лимит — signupLimit за окно).
	var last *http.Response
	for i := 0; i <= signupLimit; i++ {
		last = postSignup(t, ts, url.Values{"name": {"X"}}, nil)
	}
	if last.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("статус %d, ожидался 429 после исчерпания лимита", last.StatusCode)
	}
}

func TestValidateLead(t *testing.T) {
	cases := []struct {
		lead store.Lead
		ok   bool
	}{
		{store.Lead{Name: "Иван", Phone: "+7 900 123-45-67", Email: "i@example.com"}, true},
		{store.Lead{Name: "Иван", Phone: "8(900)123-45-67", Email: "i@example.com"}, true},
		{store.Lead{Name: "И", Phone: "+79001234567", Email: "i@example.com"}, false},                // имя короче 2
		{store.Lead{Name: "Иван", Phone: "", Email: "i@example.com"}, false},                         // без телефона
		{store.Lead{Name: "Иван", Phone: "+79001234567", Email: "почта"}, false},                     // мусор
		{store.Lead{Name: strings.Repeat("а", 101), Phone: "+79001234567", Email: "i@x.com"}, false}, // имя длиннее 100
	}

	for _, c := range cases {
		errText := validateLead(c.lead)
		if c.ok && errText != "" {
			t.Fatalf("%+v: неожиданная ошибка %q", c.lead, errText)
		}
		if !c.ok && errText == "" {
			t.Fatalf("%+v: ожидалась ошибка валидации", c.lead)
		}
	}
}

// TestHitFallback — сбой базы не должен ронять главную страницу: страница
// рисуется с последним известным числом визитов.
func TestHitFallback(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log, err := logging.New(logging.Config{Output: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	a := &app{log: log, store: st}

	// Работающая база: число растёт.
	if n := a.hit(context.Background()); n != 1 {
		t.Fatalf("первый визит %d, ожидался 1", n)
	}

	// База «умерла»: страница получает последнее известное число.
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	a.lastVisits.Store(41)
	if n := a.hit(context.Background()); n != 41 {
		t.Fatalf("после сбоя счётчик %d, ожидался фолбэк 41", n)
	}
}
