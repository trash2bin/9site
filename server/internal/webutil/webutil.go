// Package webutil общие для нескольких пакетов мелочи: адрес клиента
// (ClientIP), ограничители частоты (Limits) и проверка почты (ValidEmail).
package webutil

import (
	"net"
	"net/http"
	"regexp"
	"strings"
)

// ClientIP возвращает реальный адрес клиента: за nginx RemoteAddr всегда 127.0.0.1.
// Из X-Forwarded-For берём только последний адрес: свой префикс в начало подставляет клиент, чтобы обойти лимит.
func ClientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
		return ip
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// emailPattern почта, одна на все формы (auth и leads): без крайностей
// RFC, но мусор отсекает.
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// ValidEmail похож ли адрес на почту; 254 предел длины.
func ValidEmail(email string) bool {
	return len(email) <= 254 && emailPattern.MatchString(email)
}
