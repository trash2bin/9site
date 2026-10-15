// Package home витрина: главная со счётчиком визитов и «красивые» адреса
// страниц (/maket, /catalog) поверх статики. Проверки статики на старте.
package home

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
)

// Counter минимум, что срезу нужно от хранилища счётчика визитов.
type Counter interface {
	Hit(ctx context.Context) (int, error)
}

// Config настройки среза.
type Config struct {
	StaticRoot string // каталог статики
	Log        *logging.Logger
	Store      Counter
}

// Server состояние среза.
type Server struct {
	staticRoot string
	log        *logging.Logger
	store      Counter

	// lastVisits последнее удавшееся число счётчика: страница рисуется с ним,
	// если запись в базу не удалась. Атомик, запросы параллельны.
	lastVisits atomic.Int64
}

// New собирает срез и проверяет вёрстку на старте: пропавшая метка или
// слой стилей это явная ошибка запуска, а не 404 у посетителя.
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

// pageRoutes адреса без .html: браузер просит короткий путь, сервер
// отдаёт страницу из staticRoot.
var pageRoutes = map[string]string{
	"/maket":   "design.html",
	"/catalog": "catalog.html",
}

// pageMarkers метки, которые обязаны быть в страницах: по ним подставляются
// динамические куски, пропавшая метка валит запуск.
var pageMarkers = map[string]string{
	"design.html": thanksMark,
}

func (s *Server) Register(mux *http.ServeMux) {
	fileServer := http.FileServer(http.Dir(s.staticRoot))

	// "/" и "/index.html" обслуживает счётчик, всё остальное статика;
	// в ServeMux точный шаблон побеждает префикс "/".
	mux.Handle("/", alwaysRevalidate(staticOnly(fileServer)))
	mux.HandleFunc("GET /{$}", s.indexHandler)
	mux.HandleFunc("GET /index.html", s.indexHandler)

	for route, file := range pageRoutes {
		mux.HandleFunc("GET "+route, s.pageHandler(file))
		mux.HandleFunc("GET "+route+"/{$}", s.pageHandler(file))
	}
}
