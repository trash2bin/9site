package webutil

import "net/http"

// securityHeaders базовая обвязка ответов. Источник правды один: продублировать
// в nginx нельзя, два HSTS-заголовка браузер склеит запятой и перестанет
// применять оба.
var securityHeaders = map[string]string{
	"Strict-Transport-Security": "max-age=31536000",
	"X-Frame-Options":           "DENY",
	"X-Content-Type-Options":    "nosniff",
	"Referrer-Policy":           "strict-origin-when-cross-origin",
	"Permissions-Policy":        "camera=(), microphone=(), geolocation=(), payment=()",
	// Лёгкая часть CSP: не трогает инлайн-скрипты. script-src с nonce
	// придёт отдельной итерацией, пока он сломал бы страницы.
	"Content-Security-Policy": "frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none'",
}

// SecurityHeaders вешает заголовки на все ответы ниже по стеку, включая
// редиректы и 404.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, value := range securityHeaders {
			w.Header().Set(name, value)
		}
		next.ServeHTTP(w, r)
	})
}
