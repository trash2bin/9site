// Тесты security-заголовков: они обязаны быть на любом ответе приложения.
package webutil_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

func TestSecurityHeaders(t *testing.T) {
	want := map[string]string{
		"Strict-Transport-Security": "max-age=31536000",
		"X-Frame-Options":           "DENY",
		"X-Content-Type-Options":    "nosniff",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Permissions-Policy":        "camera=(), microphone=(), geolocation=(), payment=()",
		"Content-Security-Policy":   "frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'",
	}

	cases := []struct {
		name string
		h    http.HandlerFunc
	}{
		{"страница", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}},
		{"редирект", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/next", http.StatusSeeOther)
		}},
		{"ошибка", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "нет", http.StatusNotFound)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			webutil.SecurityHeaders(c.h).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

			for name, value := range want {
				if got := rec.Header().Get(name); got != value {
					t.Errorf("%s = %q, ожидалось %q", name, got, value)
				}
			}
		})
	}
}

// Обработчик ниже может заменить заголовок своим (точка расширения для
// будущей CSP с nonce): базовые значения ставятся до вызова цепочки.
func TestSecurityHeadersHandlerCanOverride(t *testing.T) {
	rec := httptest.NewRecorder()
	h := webutil.SecurityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))

	if got := rec.Header().Get("Content-Security-Policy"); got != "default-src 'self'" {
		t.Fatalf("CSP = %q, ожидалась замена обработчиком", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, базовый заголовок пропал", got)
	}
}
