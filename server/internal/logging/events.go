package logging

import (
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Action одно действие пользователя, скопированное в журнал действий.
type Action struct {
	Login  string
	Event  Event
	Target string
	Result string
	IP     string
}

// Event имя события журнала и описание для админки; создаётся только регистрацией,
// пустой Event не равен ни одному зарегистрированному событию.
type Event struct {
	name string
	desc string
}

// Name стабильное имя события: лежит в базе, переименование требует миграции строк.
func (e Event) Name() string { return e.name }

// Desc описание события для фильтра и подсказок админки.
func (e Event) Desc() string { return e.desc }

// Registrar namespace среза, в котором заводятся события.
type Registrar struct {
	prefix string
}

// Stable action results.
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

var (
	// eventsRegistry пишется на инициализации пакетов, читается по запросам.
	registryMu sync.RWMutex
	registry   = map[string]Event{}
)

var (
	prefixRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	nameRE   = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

// NewRegistrar namespace, обязанный совпадать с каталогом файла-объявителя:
// опечатка в префиксе или чужой домен валят старт, а не разъезжается с базой
// молча. Основной способ заводить события.
func NewRegistrar(prefix string) *Registrar {
	r := newRegistrar(prefix)
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		panic("logging: не достать путь к файлу события")
	}
	if dir := filepath.Base(filepath.Dir(file)); dir != prefix {
		panic(fmt.Sprintf("logging: префикс %q не совпадает с каталогом %q (%s)", prefix, dir, file))
	}
	return r
}

// NewForeignRegistrar namespace вне каталога объявления: для имен, чей домен
// отличается от пакета-носителя, но уже закреплён в базе (lead, system).
func NewForeignRegistrar(prefix string) *Registrar {
	return newRegistrar(prefix)
}

func newRegistrar(prefix string) *Registrar {
	if !prefixRE.MatchString(prefix) {
		panic(fmt.Sprintf("logging: кривой префикс %q", prefix))
	}
	return &Registrar{prefix: prefix}
}

// Register ставит событие prefix.suffix в реестр; описание обязательно, оно
// уходит в фильтр админки. Дубликат имени и пустое описание валят старт:
// два события с одним именем молча перетёрли бы друг друга в журнале.
func (r *Registrar) Register(suffix, desc string) Event {
	name := r.prefix + "." + suffix
	if !nameRE.MatchString(name) {
		panic(fmt.Sprintf("logging: кривое имя события %q", name))
	}
	desc = strings.TrimSpace(desc)
	if desc == "" {
		panic(fmt.Sprintf("logging: событие %q без описания", name))
	}
	e := Event{name: name, desc: desc}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, ok := registry[name]; ok {
		panic(fmt.Sprintf("logging: событие %q уже зарегистрировано", name))
	}
	registry[name] = e
	return e
}

// Events снимок реестра, отсортирован по имени: выпадайка фильтра стабильна.
func Events() []Event {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Event, 0, len(registry))
	for _, e := range registry {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}
