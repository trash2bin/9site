package webutil

// Централизованные ограничители частоты.
//
// Раньше у каждой фичи был свой экземпляр лимитера со своим const'ом
// порога, а ключ (адрес клиента) считался руками в каждом обработчике:
// список лимитов жил в голове и в коде, а забытая строка ClientIP(r)
// превращала лимит в «один общий на всех» — невидимую на локальной
// разработке аварию. Теперь счётчиков один — реестр Limits; что ограничено
// и насколько — таблица бюджетов в main.go, а сюда приходят только имена.
//
// Алгоритм — скользящее окно на отметках времени (не фиксированное: в
// фиксированном окне на границе сброса атакующий проталкивает двойную
// дозу). Всё живёт в памяти одного процесса: ни одной записи в базу на
// запрос. Перезапуск сервиса закономерно сбрасывает лимиты — для
// персонального сайта это свойство, а не недостаток: застрявший адрес
// не наказывается навечно.

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Budget — сколько действий разрешено одному ключу (обычно адресу клиента)
// за скользящее окно.
type Budget struct {
	Limit  int
	Window time.Duration
}

// BudgetLogger — что нужно реестру от логгера: одно предупреждение на
// каждое срабатывание лимита, чтобы «почему у меня ничего не работает»
// было видно в журнале, а не только на экране. Интерфейс объявлен здесь,
// у потребителя: реализация (logging.Logger) собирается в main.
type BudgetLogger interface {
	Warn(msg string, args ...any)
}

// nopBudgetLogger — заглушка, когда логгер не передан (тесты).
type nopBudgetLogger struct{}

func (nopBudgetLogger) Warn(string, ...any) {}

// Limits — единый реестр ограничителей частоты процесса. Один на всю
// программу, создаётся в main и раздаётся срезам через Middleware.
type Limits struct {
	// budgets — неизменяемая после старта таблица: имя → порог.
	budgets map[string]Budget
	log     BudgetLogger

	mu sync.Mutex
	// hits — имя бюджета → ключ → отметки попыток внутри окна.
	hits map[string]map[string][]time.Time
	// attempts — сколько попыток записано всего (не ключей): счётчик
	// дешёвой периодической чистки карты от позабытых адресов.
	attempts int
}

// NewLimits собирает реестр по таблице бюджетов. Невалидный порог — ошибка
// на старте, а не лимит «ноль минус бесконечность» в проде.
func NewLimits(budgets map[string]Budget, log BudgetLogger) (*Limits, error) {
	if len(budgets) == 0 {
		return nil, errors.New("лимиты: нужен хотя бы один бюджет")
	}
	for name, b := range budgets {
		if name == "" {
			return nil, errors.New("лимиты: бюджет без имени")
		}
		if b.Limit < 1 || b.Window <= 0 {
			return nil, fmt.Errorf("лимиты: бюджет %q — limit=%d, window=%s: нужны положительные значения",
				name, b.Limit, b.Window)
		}
	}
	if log == nil {
		log = nopBudgetLogger{}
	}
	return &Limits{
		budgets: budgets,
		log:     log,
		hits:    map[string]map[string][]time.Time{},
	}, nil
}

// budget ищет бюджет по имени и падает, если его нет: опечатка в имени
// означала бы проверку, которая не выполняется вовсе, — дыру, невидимую
// и в логе, и при чтении кода. Таблица объявлена в main и обязана знать
// обо всех именах, которые зовут срезы.
func (l *Limits) budget(name string) Budget {
	b, ok := l.budgets[name]
	if !ok {
		panic(fmt.Sprintf("лимиты: бюджет %q не объявлен (таблица бюджетов — в main.go)", name))
	}
	return b
}

// Allow проверяет бюджет и фиксирует попытку: false — лимит исчерпан,
// действию лучше отказать. Ключ считает вызывающий (обычно ClientIP).
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

// RetryAfter — сколько ждать до следующей разрешённой попытки; ноль —
// можно прямо сейчас. Нужен для заголовка Retry-After: без него клиент
// не знает, когда вернуться, и просто повторяет запрос, снова упираясь
// в лимит.
func (l *Limits) RetryAfter(name, key string) time.Duration {
	budget := l.budget(name)

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	fresh := l.freshLocked(name, key, budget, now)
	if len(fresh) < budget.Limit {
		return 0
	}
	// Отметки идут по порядку, первая — самая старая.
	return fresh[0].Add(budget.Window).Sub(now)
}

// Middleware оборачивает обработчик маршрута: ключ — ClientIP запроса
// (в одном месте, а не в каждом обработчике), при исчерпании бюджета
// ставит Retry-After, пишет warn в лог и вызывает over вместо next.
//
// over рисует свой ответ: срез авторизации возвращает страницу студии
// со своим текстом и CSRF, публичная форма — обычный 429. nil — дежурный
// текст для тех, кому достаточно статуса.
func (l *Limits) Middleware(name string, over, next http.Handler) http.Handler {
	// Проверка здесь, а не при первом запросе: незаданный бюджет должен
	// упасть на старте, пока сервер ещё не начал обслуживать людей.
	l.budget(name)
	if over == nil {
		over = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "слишком много запросов — попробуйте позже", http.StatusTooManyRequests)
		})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := ClientIP(r)
		if l.Allow(name, key) {
			next.ServeHTTP(w, r)
			return
		}

		wait := l.RetryAfter(name, key)
		// Целые секунды вверх: меньше секунды клиент не ждёт, а частоту
		// заголовка он понимает только в секундах.
		secs := int((wait + time.Second - 1) / time.Second)
		if secs < 1 {
			secs = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		l.log.Warn("лимит частоты", "бюджет", name, "ip", key, "ждал", wait.Round(time.Second))
		over.ServeHTTP(w, r)
	})
}

// freshLocked выкидывает протухшие отметки ключа и возвращает свежие.
// Вызывается под мьютексом; срезает на месте, чтобы память на ключ не
// росла выше budget.Limit.
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

// sweep выкидывает ключи, у которых не осталось свежих попыток ни в одном
// бюджете, — карта не растёт бесконечно под разнотравьем ботов.
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
