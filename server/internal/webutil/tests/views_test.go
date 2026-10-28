// Тесты журнала просмотров: предикт PageView и middleware ViewLog.
package webutil_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// TestPageView: просмотром считается только навигация браузера на HTML-страницу,
// healthz, API, картинки и файлы с расширением отсекаются.
func TestPageView(t *testing.T) {
	cases := []struct {
		name   string
		method string
		accept string
		path   string
		want   bool
	}{
		{"навигация", "GET", "text/html,application/xhtml+xml", "/studio", true},
		{"корень", "GET", "text/html", "/", true},
		{"вложенная страница", "GET", "text/html", "/vault/users", true},
		{"без Accept", "GET", "*/*", "/studio", false},
		{"POST", "POST", "text/html", "/studio", false},
		{"healthz", "GET", "text/html", "/healthz", false},
		{"API в адресной строке", "GET", "text/html", "/api/photos", false},
		{"аватар", "GET", "text/html", "/avatars/a.gif", false},
		{"фото", "GET", "text/html", "/photos/cover.jpg", false},
		{"статика", "GET", "text/html", "/assets/img/tux.gif", false},
		{"JSON в адресной строке", "GET", "text/html", "/vault/openapi.json", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(c.method, c.path, nil)
			r.Header.Set("Accept", c.accept)
			if got := webutil.PageView(r); got != c.want {
				t.Fatalf("PageView = %v, ожидалось %v", got, c.want)
			}
		})
	}
}

// viewSink подменная запись просмотров: собирает вызовы и умеет падать.
type viewSink struct {
	views []webutil.View
	err   error
}

func (s *viewSink) RecordView(_ context.Context, v webutil.View) error {
	s.views = append(s.views, v)
	return s.err
}

// TestViewLogMiddleware: запись идёт с данными запроса и логином из резолвера,
// ошибка записи не роняет запрос, не-страницы не пишутся.
func TestViewLogMiddleware(t *testing.T) {
	log, err := logging.New(logging.Config{Output: io.Discard})
	if err != nil {
		t.Fatal(err)
	}

	sink := &viewSink{}
	vl := webutil.ViewLog{
		Sink:  sink,
		Login: func(*http.Request) string { return "ivan" },
		Log:   log,
	}
	called := false
	h := vl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	r := httptest.NewRequest("GET", "/studio", nil)
	r.Header.Set("Accept", "text/html")
	r.Header.Set("User-Agent", "Firefox")
	r.Header.Set("Referer", "https://example.com/")
	r.Header.Set("X-Real-IP", "192.0.2.1")
	h.ServeHTTP(httptest.NewRecorder(), r)

	if !called {
		t.Fatal("запрос не дошёл до обработчика")
	}
	if len(sink.views) != 1 {
		t.Fatalf("записей: %d, ожидалась 1", len(sink.views))
	}
	v := sink.views[0]
	if v.Login != "ivan" || v.Path != "/studio" || v.IP != "192.0.2.1" ||
		v.UA != "Firefox" || v.Referer != "https://example.com/" {
		t.Fatalf("запись с неполными данными: %+v", v)
	}

	// Статика и POST не пишутся, но до обработчика доходят.
	for _, req := range []*http.Request{
		httptest.NewRequest("GET", "/assets/a.css", nil),
		httptest.NewRequest("POST", "/studio", nil),
	} {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if len(sink.views) != 1 {
		t.Fatalf("после не-страниц записей: %d, ожидалась 1", len(sink.views))
	}
	if !called {
		t.Fatal("не-страницы не дошли до обработчика")
	}

	// Ошибка записи не должна ронять запрос.
	failing := &viewSink{err: io.ErrUnexpectedEOF}
	vl2 := webutil.ViewLog{Sink: failing, Log: log}
	h2 := vl2.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h2.ServeHTTP(rec, r2)
	if rec.Code != http.StatusOK {
		t.Fatalf("код при ошибке записи: %d", rec.Code)
	}
}
