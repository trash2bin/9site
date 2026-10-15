// Package leads — публичная форма заявок со страницы /maket (POST /signup):
// валидация полей, защита от спама (honeypot + проверка Origin) и запись
// заявки в таблицу leads.
//
// Это не вход и не регистрация аккаунта — за них отвечает срез auth;
// здесь только заявки. Хранилище приходит извне готовым (интерфейс Store
// объявлен тут, у потребителя), лимитер — из общего реестра webutil.Limits,
// собранного в main.
package leads

import (
	"context"
	"errors"
	"net/http"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// BudgetSignup — имя бюджета лимитера для POST /signup. Порог и окно
// объявлены в таблице бюджетов в main (переопределяются LIMIT_FORM_SIGNUP).
// Имя — контракт с main: опечатка тут означала бы проверку, которой нет.
const BudgetSignup = "form.signup"

// Store — минимум, что срезу нужно от хранилища заявок.
type Store interface {
	SaveLead(ctx context.Context, l store.Lead) error
}

// Logger — то, что срезу нужно от логгера сервера.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Action(login, action, ip string)
}

// Throttle — централизованный ограничитель частоты. Граница объявлена у
// потребителя: срезу важно лишь уметь завернуть свой маршрут, а где живёт
// таблица бюджетов и как считается ключ — не его дело.
type Throttle interface {
	Middleware(name string, over, next http.Handler) http.Handler
}

// Config — настройки среза.
type Config struct {
	Store    Store
	BaseURL  string // внешний адрес сайта: по нему проверяется Origin формы
	Log      Logger
	Throttle Throttle
}

// Server — состояние среза заявок.
type Server struct {
	store    Store
	log      Logger
	baseURL  string
	throttle Throttle
}

// New собирает срез и проверяет обязательные зависимости.
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

// Register вешает форму на общий роутер. Лимит заворачивается здесь, при
// регистрации маршрута: он виден рядом с адресом и не может быть забыт.
func (s *Server) Register(mux *http.ServeMux) {
	mux.Handle("POST /signup", s.throttle.Middleware(BudgetSignup,
		http.HandlerFunc(s.tooMany), http.HandlerFunc(s.handleSignup)))
}
