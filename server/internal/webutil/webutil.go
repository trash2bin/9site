// Package webutil — общие для нескольких пакетов HTTP-мелочи: определение
// адреса клиента (ClientIP) и централизованные ограничители частоты
// (Limits, отдельный файл limits.go). Живут здесь, а не копируются:
// и то, и другое нужно и авторизации (перебор пароля), и публичной форме
// Sign Up (спам-заявки).
package webutil

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP возвращает реальный адрес клиента.
//
// За nginx RemoteAddr всегда 127.0.0.1, поэтому смотрим заголовки.
// X-Real-IP надёжнее X-Forwarded-For: nginx дописывает реальный адрес к
// клиентской строке ($proxy_add_x_forwarded_for), и полная строка XFF
// подделывается клиентом — атакующий варьирует свой префикс и получает
// сколько угодно «адресов» для обхода лимита. Поэтому из XFF берём только
// последний адрес (шлёт ближайший прокси), а лучше — X-Real-IP.
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
