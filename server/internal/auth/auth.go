// Package auth — закрытый раздел сайта: регистрация, авторизация,
// восстановление пароля (лабораторная N4).
//
// Механика в двух словах. Пароль хранится bcrypt-хэшем (а не md5, как в
// методичке — это уязвимо), в базе лежат пользователи, сессии и токены
// сброса пароля. При входе выдаётся cookie sid со случайным токеном: в базе
// хранится только sha256 от него, поэтому утечка базы не даёт готовых cookie,
// а кража cookie гасится флагами HttpOnly и SameSite. Весь вывод — через
// html/template (автоматическое экранирование от XSS).
//
// Доступ к данным идёт через два узких интерфейса, Users и Sessions,
// объявленные здесь же: auth говорит, что ему нужно, и не зависит от того,
// где это живёт. Сегодня интерфейсы выполняет пакет store (одна SQLite-
// база сайта), завтра сессии можно перетащить в Redis, не меняя ни строчки
// в этом пакете — только сборку в main.go.
//
// Закрытый раздел — /vault: по прямой ссылке без входа его уводит на
// окно авторизации. Витрина /studio открыта намеренно: авторизация —
// не забор, а доп. возможности. Восстановление пароля — письмом на почту:
// при настроенном SMTP оно отправляется по-настоящему, иначе письмо
// сохраняется в файл-«почтовый ящик» и ссылка показывается на экране
// (как разрешено методичкой).
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

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// Имена cookie. sid — токен сессии, csrf — подпись форм анонимных
// посетителей (регистрация, вход, сброс пароля).
const (
	cookieSID  = "sid"
	cookieCSRF = "csrf"
)

// Сроки жизни. Сессия живёт 30 дней: по условию лабораторной пользователь,
// не нажавший «выйти», должен считаться авторизованным и в следующем
// сеансе. Ссылка на сброс пароля живёт час.
const (
	sessionTTL = 30 * 24 * time.Hour
	resetTTL   = time.Hour
)

// Имена бюджетов ограничителей модуля — контракт с main.go: там, в одной
// таблице, объявлены все бюджеты процесса вместе с порогами, и каждый
// порог можно поднять переменной окружения (LIMIT_AUTH_LOGIN и т.п.)
// без пересборки.
//
// Бюджетов три — по одному на действие, а не один общий: иначе перебор
// пароля гасил бы и регистрацию, и восстановление пароля. В офисах и у
// мобильных операторов, где за одним адресом живёт много людей, общий
// счётчик выглядел бы как поломка сайта — «у всех ничего не работает
// десять минут». Это и есть простейшая защита от перебора вместо капчки
// из методички.
const (
	BudgetLogin    = "auth.login"
	BudgetRegister = "auth.register"
	BudgetForgot   = "auth.forgot"
)

// User и Session — доменные типы данных модуля. Живут они в пакете store
// (там же, где и SQL); здесь — алиасы, чтобы код auth и его тесты не
// зависели от того, в каком пакете тип объявлен.
type (
	User    = store.User
	Session = store.Session
)

// errLoginTaken — сигнал «логин уже занят» наверх, в форму.
var errLoginTaken = store.ErrLoginTaken

// errEmailTaken — сигнал «почта уже занята» наверх, в форму: без него
// второй аккаунт с чужим адресом получал письма со сбросом чужого пароля.
var errEmailTaken = store.ErrEmailTaken

// Users — минимум, что auth нужно от хранилища пользователей.
// Интерфейс объявлен у потребителя (в auth), а не в пакете store: так
// граница подмены рисуется там, где ею пользуются.
type Users interface {
	CreateUser(ctx context.Context, login, passHash, fullName, email string) (*User, error)
	UserByLogin(ctx context.Context, login string) (*User, error)
	UserByID(ctx context.Context, id int64) (*User, error)
	UserByEmail(ctx context.Context, email string) (*User, error)
	UpdateProfile(ctx context.Context, id int64, fullName, email string) error
	UpdatePassword(ctx context.Context, id int64, passHash string) error
	UpdateAvatar(ctx context.Context, id int64, name string) error
}

// Sessions — шов подмены хранилища сессий и токенов. Сегодня выполняет
// *store.Store (SQLite); адаптер поверх Redis реализует те же методы —
// код auth не меняется. Семантика методов зафиксирована здесь, а не
// додумывается реализацией:
//
//   - ConsumeResetToken атомарен: два параллельных перехода по ссылке
//     не смогут использовать токен дважды;
//   - DeleteUserSessions — отзыв ВСЕХ cookie при смене пароля; для Redis
//     это второй индекс user → tokens;
//   - PurgeExpiredSessions для Redis бессмысленна (там TTL) — можно
//     вернуть (0, nil).
type Sessions interface {
	CreateSession(ctx context.Context, userID int64, tokenHash, csrf string, expiresAt time.Time) error
	SessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteUserSessions(ctx context.Context, userID int64) error
	PurgeExpiredSessions(ctx context.Context, before time.Time) (int64, error)
	CreateResetToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error
	ConsumeResetToken(ctx context.Context, tokenHash string) (int64, error)
}

// Throttle — централизованное ограничение частоты. Граница объявлена у
// потребителя (как у вас принято): модулю нужно только уметь заворачивать
// свои обработчики, а где живёт таблица бюджетов и как считается ключ,
// его не касается. Реализация — *webutil.Limits, собирается в main.
type Throttle interface {
	// Middleware возвращает обработчик, пропускающий запросы, пока хватает
	// бюджета name; при исчерпании — вызывается over со своим ответом.
	Middleware(name string, over, next http.Handler) http.Handler
}

// SMTPConfig — параметры почтового сервера для писем восстановления.
// Пустой Host означает «SMTP не настроен»: письма уходят в файл-«почтовый
// ящик» и показываются на экране.
type SMTPConfig struct {
	// Provider — предустановка: gmail | yandex | mailru. Заполняет
	// Host/Port автоматически; вместе с Host ставить нельзя.
	Provider string
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// Logger — то, что нужно модулю от логгера сервера (принимаем интерфейс,
// а не конкретный пакет — так auth не зависит от реализации). Реализация —
// logging.Logger из точки входа; nil заменяется заглушкой, чтобы тесты
// работали без настройки.
type Logger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	// Action — действие пользователя: вход, регистрация и т.п. Попадает
	// и в общий лог, и в журнал действий для админ-панели.
	Action(login, action, ip string)
}

// nopLogger — заглушка для тестов и старых вызовов без логгера.
type nopLogger struct{}

func (nopLogger) Info(string, ...any)           {}
func (nopLogger) Warn(string, ...any)           {}
func (nopLogger) Error(string, ...any)          {}
func (nopLogger) Action(string, string, string) {}

// Config — настройки сервера авторизации.
type Config struct {
	// Users и Sessions — хранилища данных модуля (см. интерфейсы выше).
	Users    Users
	Sessions Sessions
	// AvatarsDir — каталог с файлами аватаров. Файлы, не база: аватар —
	// это картинка, которую отдаёт http.ServeFile.
	AvatarsDir string
	// OutboxDir — каталог «почтового ящика»: сюда письма складываются,
	// когда SMTP не настроен.
	OutboxDir string
	// Throttle — централизованные лимиты частоты (см. интерфейс выше).
	// Обязателен: без них вход, регистрация и сброс открыты для перебора.
	Throttle Throttle
	// BaseURL — внешний адрес сайта: в письмах ссылки должны быть абсолютными.
	BaseURL string
	// Log — центральный логгер сервера. Действия пользователей идут через
	// него, поэтому логи авторизации живут не в своём углу, а в общем журнале.
	Log  Logger
	SMTP SMTPConfig
}

// Server — всё состояние модуля авторизации.
type Server struct {
	users     Users
	sessions  Sessions
	cfg       Config
	log       Logger
	avatarDir string
	outboxDir string
	mailer    Mailer
	// throttle — обёртка маршрутов централизованным ограничителем.
	throttle Throttle
}

// New готовит модуль: проверяет конфиг, создаёт каталоги, собирает почтовый
// тракт. Базу не открывает — её владелец снаружи (main), сюда приходят
// готовые хранилища.
func New(cfg Config) (*Server, error) {
	if cfg.Users == nil || cfg.Sessions == nil {
		return nil, errors.New("auth: нужны хранилища Users и Sessions")
	}
	if cfg.Throttle == nil {
		return nil, errors.New("auth: нужен ограничитель частоты (Throttle)")
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

	log := cfg.Log
	if log == nil {
		log = nopLogger{}
	}

	// Конфиг почты проверяется на старте: нерабочий SMTP не падает — он
	// молча пишет в outbox, иначе опечатку было бы видно только при
	// первом сбросе пароля.
	hint, err := cfg.SMTP.resolve()
	if err != nil {
		return nil, err
	}

	s := &Server{
		users:     cfg.Users,
		sessions:  cfg.Sessions,
		cfg:       cfg,
		log:       log,
		avatarDir: cfg.AvatarsDir,
		outboxDir: cfg.OutboxDir,
		throttle:  cfg.Throttle,
	}
	s.mailer = s.buildMailer()

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
		s.log.Info("почта: SMTP не настроен — письма копируются только в outbox")
	}

	return s, nil
}

// Register подключает маршруты авторизации к общему роутеру сервера.
// Вход, регистрация и сброс заворачиваются здесь, при регистрации
// маршрута, — а не внутри обработчиков: лимит виден рядом с адресом
// и не может быть забыт.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /studio", s.handleStudio)
	mux.HandleFunc("GET /studio/{$}", s.handleStudio)
	mux.HandleFunc("GET /vault", s.handleVault)
	mux.Handle("POST /auth/register", s.throttled(BudgetRegister, "register",
		"слишком много попыток регистрации — подождите несколько минут", s.handleRegister))
	mux.Handle("POST /auth/login", s.throttled(BudgetLogin, "login",
		"слишком много попыток входа — подождите несколько минут", s.handleLogin))
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("POST /auth/profile", s.handleProfile)
	mux.Handle("POST /auth/forgot", s.throttled(BudgetForgot, "forgot",
		"слишком много запросов — подождите несколько минут", s.handleForgot))
	mux.HandleFunc("GET /reset", s.handleResetForm)
	mux.HandleFunc("POST /reset", s.handleResetApply)
	mux.HandleFunc("GET /avatars/", s.handleAvatar)
}

// throttled оборачивает обработчик централизованным ограничителем. Здесь —
// только имя бюджета, вкладка, которую открыть, и текст ошибки; счёт
// попыток и ключ (адрес клиента) — на стороне реестра в webutil. Раньше
// эта проверка жила внутри каждого обработчика: забытая строка с ключом
// превращала лимит в общий для всех посетителей — лок, которого на
// локальной разработке не видно. Открытая вкладка теперь та же, что и
// у самой формы (раньше перебор регистрации показывал окно входа).
func (s *Server) throttled(name, open, msg string, next http.HandlerFunc) http.Handler {
	over := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.renderStudio(w, studioData{Error: msg, Open: open, CSRF: s.csrfToken(w, r)})
	})
	return s.throttle.Middleware(name, over, next)
}

// token выдаёт криптостойкое значение для cookie или подписи форм.
func token() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken — sha256 токена: в базу попадает только хэш, поэтому утечка
// базы не даёт готовых cookie.
func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// isSecure — стоит ли ставить cookie с флагом Secure: на localhost по
// http флаг убил бы сессию в разработке.
func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// setCookie ставит или сбрасывает cookie. maxAge < 0 — удалить.
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
