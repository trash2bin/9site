// Package auth закрытый раздел сайта: вход, регистрация, восстановление
// пароля, аватары. /vault закрыт, витрина /studio открыта намеренно.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// Cookie: sid токен сессии, csrf подпись форм анонимных посетителей.
const (
	cookieSID  = "sid"
	cookieCSRF = "csrf"
)

const (
	sessionTTL = 30 * 24 * time.Hour
	resetTTL   = time.Hour
)

// Имена бюджетов: контракт с main.go, порог поднимается переменной LIMIT_AUTH_LOGIN и т.п.
// Бюджет по одному на действие: общий счётчик гасил бы всё разом.
const (
	BudgetLogin    = "auth.login"
	BudgetRegister = "auth.register"
	BudgetForgot   = "auth.forgot"
)

// Алиасы типов store, чтобы код auth не зависел от пакета их объявления.
type (
	User    = store.User
	Session = store.Session
)

var errLoginTaken = store.ErrLoginTaken
var errEmailTaken = store.ErrEmailTaken

// Users минимум, что нужно от хранилища; интерфейс объявлен у потребителя.
type Users interface {
	CreateUser(ctx context.Context, login, passHash, fullName, email string) (*User, error)
	UserByLogin(ctx context.Context, login string) (*User, error)
	UserByID(ctx context.Context, id int64) (*User, error)
	UserByEmail(ctx context.Context, email string) (*User, error)
	UpdateProfile(ctx context.Context, id int64, fullName, email string) error
	UpdatePassword(ctx context.Context, id int64, passHash string) error
	UpdateAvatar(ctx context.Context, id int64, name string) error
	EnsureAdmin(ctx context.Context, login, passHash string) (bool, error)
	SetUserRole(ctx context.Context, login, role string) error
	ListUsers(ctx context.Context, limit, offset int) ([]User, error)
	CountUsers(ctx context.Context) (int, error)
	AdminCount(ctx context.Context) (int, error)
}

// Sessions шов подмены хранилища: ConsumeResetToken атомарен, DeleteUserSessions гасит все cookie при смене пароля.
type Sessions interface {
	CreateSession(ctx context.Context, userID int64, tokenHash, csrf string, expiresAt time.Time) error
	SessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteUserSessions(ctx context.Context, userID int64) error
	PurgeExpiredSessions(ctx context.Context, before time.Time) (int64, error)
	CreateResetToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	ConsumeResetToken(ctx context.Context, tokenHash string) (int64, error)
}

// Throttle ограничение частоты; реализация *webutil.Limits, собирается в main.
type Throttle interface {
	// Middleware пропускает запросы, пока хватает бюджета name, при
	// исчерпании отдаёт ответ over.
	Middleware(name string, over, next http.Handler) http.Handler
}

// SMTPConfig настройки почтового сервера для писем восстановления; пустой
// Host означает «SMTP не настроен», письма уходят в outbox.
type SMTPConfig struct {
	// Provider предустановка gmail | yandex | mailru: заполняет Host/Port,
	// вместе с Host ставить нельзя.
	Provider string
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Config настройки сервера авторизации.
type Config struct {
	Users    Users
	Sessions Sessions
	// AvatarsDir каталог файлов аватаров: аватар лежит на диске, не в базе.
	AvatarsDir string
	// OutboxDir куда складывать письма, когда SMTP не настроен.
	OutboxDir string
	// Throttle обязателен: без него вход, регистрация и сброс открыты для
	// перебора.
	Throttle Throttle
	// BaseURL внешний адрес сайта: в письмах ссылки должны быть абсолютными.
	BaseURL string
	Log     *logging.Logger
	SMTP    SMTPConfig
	// AdminLogin и AdminPassword секретный админ из окружения: на каждом
	// старте аккаунт создаётся или ему обновляются роль и пароль. Пусто
	// в обоих полях означает «админа из env нет».
	AdminLogin    string
	AdminPassword string
}

// Server всё состояние модуля авторизации.
type Server struct {
	users     Users
	sessions  Sessions
	cfg       Config
	log       *logging.Logger
	avatarDir string
	outboxDir string
	mailer    Mailer
	throttle  Throttle
}

// New готовит модуль: проверяет конфиг, создаёт каталоги, собирает почтовый
// тракт. Базу не открывает: сюда приходят готовые хранилища.
func New(cfg Config) (*Server, error) {
	if cfg.Users == nil || cfg.Sessions == nil {
		return nil, errors.New("auth: нужны хранилища Users и Sessions")
	}
	if cfg.Throttle == nil {
		return nil, errors.New("auth: нужен ограничитель частоты (Throttle)")
	}
	if cfg.Log == nil {
		return nil, errors.New("auth: нужен логгер (Log)")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:8080"
	}
	if cfg.AvatarsDir == "" || cfg.OutboxDir == "" {
		return nil, errors.New("auth: AvatarsDir и OutboxDir обязательны")
	}

	for _, dir := range []string{cfg.AvatarsDir, cfg.OutboxDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
	}

	// Конфиг почты проверяется на старте: иначе опечатку увидели бы только
	// при первом сбросе пароля.
	hint, err := cfg.SMTP.resolve()
	if err != nil {
		return nil, err
	}

	s := &Server{
		users:     cfg.Users,
		sessions:  cfg.Sessions,
		cfg:       cfg,
		log:       cfg.Log,
		avatarDir: cfg.AvatarsDir,
		outboxDir: cfg.OutboxDir,
		throttle:  cfg.Throttle,
	}
	s.mailer = s.buildMailer()

	if err := s.bootstrapAdmin(); err != nil {
		return nil, err
	}

	if s.cfg.SMTP.Configured() {
		provider := s.cfg.SMTP.Provider
		if provider == "" {
			provider = "(свой сервер)"
		}
		s.log.Info("почта: SMTP настроен",
			"провайдер", provider,
			"адрес", fmt.Sprintf("%s:%d", s.cfg.SMTP.Host, s.cfg.SMTP.Port))
		if hint != "" {
			s.log.Info("почта: нужен пароль приложения, не обычный пароль аккаунта", "где взять", hint)
		}
	} else {
		s.log.Info("почта: SMTP не настроен: письма копируются только в outbox")
	}

	return s, nil
}

// Register подключает маршруты авторизации; лимит вешается здесь, при
// регистрации маршрута, а не внутри обработчика.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /studio", s.handleStudio)
	mux.HandleFunc("GET /studio/{$}", s.handleStudio)
	mux.HandleFunc("GET /vault", s.handleVault)
	mux.Handle("GET /vault/users", s.RequireAdmin(http.HandlerFunc(s.handleUsers)))
	mux.Handle("POST /vault/users/role", s.RequireAdmin(http.HandlerFunc(s.handleUserRole)))
	mux.Handle("POST /auth/register", s.throttled(BudgetRegister, "register",
		"слишком много попыток регистрации: подождите несколько минут", s.handleRegister))
	mux.Handle("POST /auth/login", s.throttled(BudgetLogin, "login",
		"слишком много попыток входа: подождите несколько минут", s.handleLogin))
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("POST /auth/profile", s.handleProfile)
	mux.Handle("POST /auth/forgot", s.throttled(BudgetForgot, "forgot",
		"слишком много запросов: подождите несколько минут", s.handleForgot))
	mux.HandleFunc("GET /reset", s.handleResetForm)
	mux.HandleFunc("POST /reset", s.handleResetApply)
	mux.HandleFunc("GET /avatars/", s.handleAvatar)
}

// throttled оборачивает обработчик ограничителем: имя бюджета, вкладка
// и текст ошибки здесь; счёт попыток и ключ на стороне webutil.
func (s *Server) throttled(name, open, msg string, next http.HandlerFunc) http.Handler {
	over := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.renderStudio(w, studioData{Error: msg, Open: open, CSRF: s.csrfToken(w, r)})
	})
	return s.throttle.Middleware(name, over, next)
}

func token() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken sha256 токена: в базу попадает только хэш, поэтому утечка базы
// не даёт готовых cookie.
func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// isSecure нужен ли флаг Secure: на localhost по http он убил бы сессию
// в разработке.
func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// setCookie ставит cookie, maxAge < 0 удаляет её.
func setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isSecure(r),
	})
}
