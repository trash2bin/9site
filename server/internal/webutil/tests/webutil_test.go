package webutil_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// TestClientIP: реальный адрес клиента за nginx. XFF целиком брать
// нельзя: клиент подставляет себе префикс и получает сколько угодно
// «адресов» для обхода лимита.
func TestClientIP(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		hdr    map[string]string
		want   string
	}{
		{
			name:   "без заголовков: из RemoteAddr",
			remote: "192.0.2.1:1234",
			want:   "192.0.2.1",
		},
		{
			name:   "X-Real-IP от nginx",
			remote: "127.0.0.1:8080",
			hdr:    map[string]string{"X-Real-IP": "198.51.100.7"},
			want:   "198.51.100.7",
		},
		{
			name:   "XFF: подделанный префикс клиента не работает, берётся последний адрес",
			remote: "127.0.0.1:8080",
			hdr: map[string]string{
				"X-Forwarded-For": "6.6.6.6, 198.51.100.7",
			},
			want: "198.51.100.7",
		},
		{
			name:   "X-Real-IP важнее XFF",
			remote: "127.0.0.1:8080",
			hdr: map[string]string{
				"X-Real-IP":       "198.51.100.7",
				"X-Forwarded-For": "6.6.6.6",
			},
			want: "198.51.100.7",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			for k, v := range c.hdr {
				r.Header.Set(k, v)
			}
			if got := webutil.ClientIP(r); got != c.want {
				t.Fatalf("ClientIP = %q, ожидалось %q", got, c.want)
			}
		})
	}
}

// TestValidEmail: одна проверка почты на все формы сайта (auth и leads).
func TestValidEmail(t *testing.T) {
	for _, c := range []struct {
		email string
		ok    bool
	}{
		{"i@example.com", true},
		{"ivan.petrov+tag@mail.ru", true},
		{"почта", false},
		{"no-at.example.com", false},
		{"a@b", false},
		{"", false},
		{strings.Repeat("a", 250) + "@example.com", false},
	} {
		if got := webutil.ValidEmail(c.email); got != c.ok {
			t.Fatalf("ValidEmail(%q) = %v, ожидалось %v", c.email, got, c.ok)
		}
	}
}

// testLimits собирает реестр с маленьким окном для тестов.
func testLimits(t *testing.T, budgets map[string]webutil.Budget) *webutil.Limits {
	t.Helper()
	l, err := webutil.NewLimits(budgets, testLogger(t))
	if err != nil {
		t.Fatalf("NewLimits: %v", err)
	}
	return l
}

// TestLimitsAllow: базовое поведение: бюджет вычитается, другие ключи и
// другие бюджеты не страдают, протухшее окно освобождает лимит.
func TestLimitsAllow(t *testing.T) {
	l := testLimits(t, map[string]webutil.Budget{
		"auth.login":  {Limit: 3, Window: 60 * time.Millisecond},
		"form.signup": {Limit: 1, Window: time.Minute},
	})

	for i := 0; i < 3; i++ {
		if !l.Allow("auth.login", "a") {
			t.Fatalf("попытка %d не должна быть в лимите", i+1)
		}
	}

	// Четвёртая уже за лимитом; другой ключ и другой бюджет свободны.
	if l.Allow("auth.login", "a") {
		t.Fatal("4-я попытка должна быть в лимите")
	}
	if !l.Allow("auth.login", "b") {
		t.Fatal("ключ b не должен зависеть от a")
	}
	if !l.Allow("form.signup", "a") {
		t.Fatal("бюджет form.signup не должен зависеть от auth.login")
	}
	if l.Allow("form.signup", "a") {
		t.Fatal("form.signup с лимитом 1 должен отказать со второй попытки")
	}

	// Окно протухло, попытки снова разрешены.
	time.Sleep(70 * time.Millisecond)
	if !l.Allow("auth.login", "a") {
		t.Fatal("после протухания окна лимит должен сброситься")
	}
}

// TestLimitsRetryAfter: сколько ждать: ноль, пока бюджет цел, и около
// окна, когда исчерпан. Заголовок Retry-After без этого бессмыслен.
func TestLimitsRetryAfter(t *testing.T) {
	window := 500 * time.Millisecond
	l := testLimits(t, map[string]webutil.Budget{"auth.login": {Limit: 1, Window: window}})

	if d := l.RetryAfter("auth.login", "a"); d != 0 {
		t.Fatalf("до первой попытки ждать нечего, получили %s", d)
	}
	if !l.Allow("auth.login", "a") {
		t.Fatal("первая попытка должна проходить")
	}
	d := l.RetryAfter("auth.login", "a")
	if d <= 0 || d > window {
		t.Fatalf("после исчерпания ждать должны до окна (%s), получили %s", window, d)
	}
}

// TestLimitsUnknownBudget: опечатка в имени бюджета обязана быть слышна
// сразу, а не превращаться в лимит, которого нет.
func TestLimitsUnknownBudget(t *testing.T) {
	l := testLimits(t, map[string]webutil.Budget{"auth.login": {Limit: 1, Window: time.Minute}})

	defer func() {
		if recover() == nil {
			t.Fatal("неизвестный бюджет должен паниковать, а не молча пропускать")
		}
	}()
	l.Allow("auth.loging", "a")
}

// testLogger логгер в никуда: реестру он обязателен, тестам не нужен.
func testLogger(t *testing.T) *logging.Logger {
	t.Helper()
	log, err := logging.New(logging.Config{Output: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return log
}

// TestNewLimitsRejectsBadBudget: нулевой порог не должен дожить до прода.
func TestNewLimitsRejectsBadBudget(t *testing.T) {
	for name, budgets := range map[string]map[string]webutil.Budget{
		"пусто":      {},
		"лимит ноль": {"x": {Limit: 0, Window: time.Minute}},
		"окно ноль":  {"x": {Limit: 1, Window: 0}},
		"без имени":  {"": {Limit: 1, Window: time.Minute}},
	} {
		if _, err := webutil.NewLimits(budgets, testLogger(t)); err == nil {
			t.Fatalf("%s: ожидалась ошибка", name)
		}
	}
}

// TestLimitsMiddleware: ключ считает сам middleware (адрес клиента, а не
// хендлер), при лимите идёт Retry-After и вызывается свой обработчик
// ответа, а не следующий по маршруту.
func TestLimitsMiddleware(t *testing.T) {
	l := testLimits(t, map[string]webutil.Budget{"auth.login": {Limit: 2, Window: time.Minute}})

	var reached int
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached++
		_, _ = w.Write([]byte("ок"))
	})
	over := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "перебор", http.StatusTooManyRequests)
	})
	h := l.Middleware("auth.login", over, next)

	// Ключ берётся из X-Real-IP, как за nginx.
	post := func(ip string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/auth/login", nil)
		r.Header.Set("X-Real-IP", ip)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := post("198.51.100.1"); w.Code != http.StatusOK {
		t.Fatalf("1-я попытка: статус %d", w.Code)
	}
	if w := post("198.51.100.1"); w.Code != http.StatusOK {
		t.Fatalf("2-я попытка: статус %d", w.Code)
	}
	w := post("198.51.100.1")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("3-я попытка: статус %d, ждали 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("при лимите обязан стоять Retry-After")
	}
	if reached != 2 {
		t.Fatalf("next вызван %d раз, ждали 2", reached)
	}

	// Другой адрес не задет.
	if w := post("198.51.100.2"); w.Code != http.StatusOK {
		t.Fatalf("другой адрес: статус %d", w.Code)
	}
}

// TestLimitsConcurrent: реестр общий на весь процесс: параллельные
// запросы не должны ломать счётчик (гонку ловит -race).
func TestLimitsConcurrent(t *testing.T) {
	l := testLimits(t, map[string]webutil.Budget{"auth.login": {Limit: 1000, Window: time.Minute}})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				l.Allow("auth.login", "key")
				l.RetryAfter("auth.login", "key")
			}
		}(i)
	}
	wg.Wait()
}

// TestMiddlewareUnknownBudgetPanicsAtStart: незаданный бюджет должен
// упасть при регистрации маршрута, а не при первом посетителе.
func TestMiddlewareUnknownBudgetPanicsAtStart(t *testing.T) {
	l := testLimits(t, map[string]webutil.Budget{"auth.login": {Limit: 1, Window: time.Minute}})
	defer func() {
		if recover() == nil {
			t.Fatal("незаданный бюджет должен паниковать на старте")
		}
	}()
	_ = l.Middleware("auth.forgot", nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
}

// TestMiddlewareDefaultOver: без своего обработчика дежурный текст и 429.
func TestMiddlewareDefaultOver(t *testing.T) {
	l := testLimits(t, map[string]webutil.Budget{"form.signup": {Limit: 1, Window: time.Minute}})
	h := l.Middleware("form.signup", nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	r := httptest.NewRequest("POST", "/signup", nil)
	if !l.Allow("form.signup", webutil.ClientIP(r)) {
		t.Fatal("первая заявка должна проходить")
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("статус %d, ждали 429", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "слишком много запросов") {
		t.Fatalf("дежурный текст не тот: %q", body)
	}
}
