// Package home — витрина: главная страница со счётчиком визитов и
// «красивые» адреса страниц (/maket, /catalog) поверх статики. Здесь же
// белый список статики и проверки вёрстки на старте.
package home

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
)

// Counter — минимум, что срезу нужно от хранилища счётчика визитов.
type Counter interface {
	Hit(ctx context.Context) (int, error)
}

// Logger — то, что срезу нужно от логгера сервера.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// Config — настройки среза.
type Config struct {
	StaticRoot string // каталог статики: index.html, design.html, style.css с css/ и assets/
	Log        Logger
	Store      Counter
}

// Server — состояние среза.
type Server struct {
	staticRoot string
	log        Logger
	store      Counter

	// lastVisits — последнее удавшееся число счётчика: страница рисуется
	// с ним, если запись в базу не удалась. Атомик: запросы параллельны.
	lastVisits atomic.Int64
}

// New собирает срез и проверяет вёрстку на старте. Пропавшая метка или
// слой стилей — это не 404 у посетителя, а явная ошибка запуска.
func New(cfg Config) (*Server, error) {
	if cfg.Log == nil {
		return nil, errors.New("home: нужен логгер (Log)")
	}
	if cfg.Store == nil {
		return nil, errors.New("home: нужно хранилище счётчика (Store)")
	}
	s := &Server{staticRoot: cfg.StaticRoot, log: cfg.Log, store: cfg.Store}
	for _, check := range []func() error{s.checkIndex, s.checkPages, s.checkStyles, s.checkExtraStatic} {
		if err := check(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// pageRoutes — адреса без .html: браузер просит короткий путь, сервер
// отдаёт страницу из staticRoot. Ключ — то, что видно в строке адреса.
var pageRoutes = map[string]string{
	"/maket":   "design.html",
	"/catalog": "catalog.html",
}

// pageMarkers — метки, которые обязаны быть в страницах: по ним сервер
// подставляет динамические куски. Пропавшая метка ломает запуск.
var pageMarkers = map[string]string{
	"design.html": thanksMark,
}

// Register вешает маршруты среза на общий роутер.
func (s *Server) Register(mux *http.ServeMux) {
	fileServer := http.FileServer(http.Dir(s.staticRoot))

	// "/" и "/index.html" обслуживает счётчик, всё остальное — статика.
	// В ServeMux точный шаблон побеждает шаблон-префикс "/".
	mux.Handle("/", alwaysRevalidate(staticOnly(fileServer)))
	mux.HandleFunc("GET /{$}", s.indexHandler)
	mux.HandleFunc("GET /index.html", s.indexHandler)

	// Красивые адреса: /maket и /maket/ отдают ту же вёрстку.
	for route, file := range pageRoutes {
		mux.HandleFunc("GET "+route, s.pageHandler(file))
		mux.HandleFunc("GET "+route+"/{$}", s.pageHandler(file))
	}
}
