package webutil

// Ограничители частоты: один реестр Limits на процесс, таблица бюджетов в main,
// сюда приходят имена. Окно скользящее, счёт в памяти: перезапуск его сбрасывает.

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
)

// Budget сколько действий разрешено одному ключу (обычно адресу клиента)
// за скользящее окно.
type Budget struct {
	Limit  int
	Window time.Duration
}

// Limits единый реестр ограничителей частоты: создаётся в main и
// раздаётся срезам через Middleware.
type Limits struct {
	// budgets неизменяемая после старта таблица: имя → порог.
	budgets map[string]Budget
	log     *logging.Logger

	mu sync.Mutex
	// hits имя бюджета → ключ → отметки попыток внутри окна.
	hits map[string]map[string][]time.Time
	// attempts сколько попыток записано всего, не ключей: счётчик
	// дешёвой периодической чистки карты от позабытых адресов.
	attempts int
}

// NewLimits собирает реестр по таблице бюджетов; невалидный порог валит
// запуск, а не оставляет прод без лимита.
func NewLimits(budgets map[string]Budget, log *logging.Logger) (*Limits, error) {
	if log == nil {
		return nil, errors.New("лимиты: нужен логгер")
	}
	if len(budgets) == 0 {
		return nil, errors.New("лимиты: нужен хотя бы один бюджет")
	}
	for name, b := range budgets {
		if name == "" {
			return nil, errors.New("лимиты: бюджет без имени")
		}
		if b.Limit < 1 || b.Window <= 0 {
			return nil, fmt.Errorf("лимиты: бюджет %q (limit=%d, window=%s): нужны положительные значения",
				name, b.Limit, b.Window)
		}
	}
	return &Limits{
		budgets: budgets,
		log:     log,
		hits:    map[string]map[string][]time.Time{},
	}, nil
}

// budget ищет бюджет по имени и падает, если его нет: опечатка означала бы
// проверку, которая не выполняется, дыру, невидимую и в логе, и в коде.
func (l *Limits) budget(name string) Budget {
	b, ok := l.budgets[name]
	if !ok {
		panic(fmt.Sprintf("лимиты: бюджет %q не объявлен (таблица бюджетов в main.go)", name))
	}
	return b
}

// Allow проверяет бюджет и фиксирует попытку: false значит лимит исчерпан.
// Ключ считает вызывающий, обычно ClientIP.
func (l *Limits) Allow(name, key string) bool {
	budget := l.budget(name)

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	fresh := l.freshLocked(name, key, budget, now)
	if len(fresh) >= budget.Limit {
		return false
	}

	l.hits[name][key] = append(fresh, now)
	l.attempts++
	if l.attempts > 10_000 {
		l.sweep(now)
	}
	return true
}

// RetryAfter сколько ждать до следующей разрешённой попытки, ноль можно прямо сейчас.
func (l *Limits) RetryAfter(name, key string) time.Duration {
	budget := l.budget(name)

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	fresh := l.freshLocked(name, key, budget, now)
	if len(fresh) < budget.Limit {
		return 0
	}
	// Отметки идут по порядку, первая самая старая.
	return fresh[0].Add(budget.Window).Sub(now)
}

// Middleware оборачивает обработчик маршрута: при исчерпании бюджета ставит
// Retry-After, пишет warn и вызывает over вместо next, nil даёт дежурный 429.
func (l *Limits) Middleware(name string, over, next http.Handler) http.Handler {
	// Бюджет проверяем сразу, а не при первом запросе: незаданный валит запуск.
	l.budget(name)
	if over == nil {
		over = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "слишком много запросов: попробуйте позже", http.StatusTooManyRequests)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := ClientIP(r)
		if l.Allow(name, key) {
			next.ServeHTTP(w, r)
			return
		}

		wait := l.RetryAfter(name, key)
		// Целые секунды вверх: меньше секунды клиент не ждёт, частота заголовка в секундах.
		secs := int((wait + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		l.log.Warn("лимит частоты", "бюджет", name, "ip", key, "ждал", wait.Round(time.Second))
		over.ServeHTTP(w, r)
	})
}

// freshLocked выкидывает протухшие отметки ключа; срезает на месте под
// мьютексом, чтобы память не росла выше budget.Limit.
func (l *Limits) freshLocked(name, key string, budget Budget, now time.Time) []time.Time {
	if l.hits[name] == nil {
		l.hits[name] = map[string][]time.Time{}
	}
	all := l.hits[name][key]
	fresh := all[:0]
	for _, t := range all {
		if now.Sub(t) < budget.Window {
			fresh = append(fresh, t)
		}
	}
	l.hits[name][key] = fresh
	return fresh
}

// sweep выкидывает ключи без свежих попыток: карта не растёт бесконечно под ботами.
func (l *Limits) sweep(now time.Time) {
	l.attempts = 0
	for name, keys := range l.hits {
		for key, times := range keys {
			budget, ok := l.budgets[name]
			if !ok {
				continue
			}
			fresh := times[:0]
			for _, t := range times {
				if now.Sub(t) < budget.Window {
					fresh = append(fresh, t)
				}
			}
			if len(fresh) == 0 {
				delete(keys, key)
				continue
			}
			keys[key] = fresh
		}
	}
}
