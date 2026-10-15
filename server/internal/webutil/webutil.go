// Package webutil — общие для нескольких пакетов HTTP-мелочи: определение
// адреса клиента и ограничитель частоты. Живут здесь, а не копируются:
// лимитер нужен и авторизации (перебор пароля), и публичной форме Sign Up
// (спам-заявки).
package webutil

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
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

// Limiter — простейший ограничитель частоты: не больше limit действий за
// window с одного ключа (обычно адреса клиента). Заменяет капчу: капча
// требует внешнего сервиса, а лимитер — десятка строк и таблицы в памяти.
type Limiter struct {
	limit  int
	window time.Duration

	mu   sync.Mutex
	hits map[string][]time.Time
	// attempts — сколько попыток записано всего (не ключей): счётчик
	// дешёвой периодической чистки карты от позабытых адресов.
	attempts int
}

// NewLimiter создаёт ограничитель: limit действий за window.
func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:  limit,
		window: window,
		hits:   map[string][]time.Time{},
	}
}

// Throttle проверяет и записывает попытку: true — лимит исчерпан,
// действию лучше отказать. Протухшие попытки выкидываются на лету,
// а примерно каждые 10 тысяч записанных попыток выкидываются и вовсе
// позабытые адреса, чтобы карта не росла бесконечно под разнотравьем
// ботов.
func (l *Limiter) Throttle(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	fresh := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			fresh = append(fresh, t)
		}
	}

	if len(fresh) >= l.limit {
		l.hits[key] = fresh
		return true
	}

	l.hits[key] = append(fresh, now)
	l.attempts++
	if l.attempts > 10_000 {
		l.sweep(now)
	}
	return false
}

// sweep выкидывает ключи, у которых не осталось свежих попыток.
func (l *Limiter) sweep(now time.Time) {
	l.attempts = 0
	for key, times := range l.hits {
		fresh := times[:0]
		for _, t := range times {
			if now.Sub(t) < l.window {
				fresh = append(fresh, t)
			}
		}
		if len(fresh) == 0 {
			delete(l.hits, key)
			continue
		}
		l.hits[key] = fresh
	}
}
