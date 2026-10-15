// Package leads публичная форма заявок со страницы /maket (POST /signup):
// валидация, защита от спама (honeypot + Origin) и запись в таблицу leads.
// Хранилище и лимитер приходят извне, сборка в main.
package leads

import (
	"context"
	"errors"
	"net/http"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// BudgetSignup имя бюджета лимитера для POST /signup; порог и окно в
// таблице бюджетов main (LIMIT_FORM_SIGNUP).
const BudgetSignup = "form.signup"

// Store минимум, что срезу нужно от хранилища заявок.
type Store interface {
	SaveLead(ctx context.Context, l store.Lead) error
}

// Throttle ограничитель частоты; реализация webutil.Limits, собирается в main.
type Throttle interface {
	Middleware(name string, over, next http.Handler) http.Handler
}

// Config настройки среза.
type Config struct {
	Store    Store
	BaseURL  string // внешний адрес сайта: по нему проверяется Origin формы
	Log      *logging.Logger
	Throttle Throttle
}

// Server состояние среза заявок.
type Server struct {
	store    Store
	log      *logging.Logger
	baseURL  string
	throttle Throttle
}

func New(cfg Config) (*Server, error) {
	if cfg.Store == nil {
		return nil, errors.New("leads: нужно хранилище (Store)")
	}
	if cfg.Throttle == nil {
		return nil, errors.New("leads: нужен ограничитель частоты (Throttle)")
	}
	if cfg.Log == nil {
		return nil, errors.New("leads: нужен логгер (Log)")
	}
	return &Server{
		store:    cfg.Store,
		log:      cfg.Log,
		baseURL:  cfg.BaseURL,
		throttle: cfg.Throttle,
	}, nil
}

// Register вешает форму на общий роутер; лимит заворачивается здесь же,
// при регистрации маршрута, а не внутри обработчика.
func (s *Server) Register(mux *http.ServeMux) {
	mux.Handle("POST /signup", s.throttle.Middleware(BudgetSignup,
		http.HandlerFunc(s.tooMany), http.HandlerFunc(s.handleSignup)))
}
