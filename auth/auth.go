// Package auth — закрытый раздел сайта: регистрация, авторизация,
// восстановление пароля (лабораторная N4).
//
// Механика в двух словах. Пароль хранится bcrypt-хэшем (а не md5, как в
// методичке — это уязвимо), в базе лежат пользователи, сессии и токены
// сброса пароля. При входе выдаётся cookie sid со случайным токеном: в базе
// хранится только sha256 от него, поэтому утечка базы не даёт готовых cookie,
// а кража cookie гасится флагами HttpOnly и SameSite. Все запросы к базе —
// с параметрами (никакой склейки SQL), весь вывод — через html/template
// (автоматическое экранирование от XSS).
//
// Закрытый раздел — /studio: без авторизации сервер перенаправляет на
// /studio?auth=login, где открывается всплывающее окно входа/регистрации.
// Восстановление пароля — письмом на почту: при настроенном SMTP оно
// отправляется по-настоящему, иначе письмо сохраняется в файл-«почтовый
// ящик» и ссылка показывается на экране (как разрешено методичкой).
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Имена cookie. sid — токен сессии, csrf — подпись форм анонимных
// посетителей (регистрация, вход, сброс пароля).
const (
	cookieSID  = "sid"
	cookieCSRF = "csrf"
)

// Сроки жизни. Сессия живёт 30 дней: по условию лабораторной пользователь,
// не нажавший «выйти», должен считаться авторизованным и в следующем сеансе.
// Ссылка на сброс пароля живёт час.
const (
	sessionTTL = 30 * 24 * time.Hour
	resetTTL   = time.Hour
)

// Ограничение частоты: не больше attemptsLimit попыток входа/регистрации с
// одного адреса за attemptsWindow. Простейшая защита от перебора пароля —
// вместо капчи из методички, которая требует внешнего сервиса.
const (
	attemptsLimit  = 20
	attemptsWindow = 10 * time.Minute
)

// SMTPConfig — параметры почтового сервера для писем восстановления.
// Пустой Host означает «SMTP не настроен»: письма уходят в файл-«почтовый
// ящик» и показываются на экране.
type SMTPConfig struct {
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
	// DataDir — каталог для базы, аватаров и исходящих писем. В проде это
	// /var/lib/9site (см. deploy/site.service), локально — var/ в проекте.
	DataDir string
	// BaseURL — внешний адрес сайта: в письмах ссылки должны быть абсолютными.
	BaseURL string
	// Log — центральный логгер сервера. Действия пользователей идут через
	// него, поэтому логи авторизации живут не в своём углу, а в общем журнале.
	Log  Logger
	SMTP SMTPConfig
}

// Server — всё состояние модуля авторизации.
type Server struct {
	db        *sql.DB
	cfg       Config
	log       Logger
	avatarDir string
	outboxDir string

	// attempts — история неудачных попыток по адресам клиентов.
	mu       sync.Mutex
	attempts map[string][]time.Time
}

// New открывает базу, готовит каталоги и проверяет схему.
func New(cfg Config) (*Server, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = "var"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:8080"
	}

	for _, dir := range []string{cfg.DataDir, filepath.Join(cfg.DataDir, "avatars"), filepath.Join(cfg.DataDir, "outbox")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
	}

	db, err := openDB(filepath.Join(cfg.DataDir, "auth.db"))
	if err != nil {
		return nil, err
	}

	log := cfg.Log
	if log == nil {
		log = nopLogger{}
	}

	s := &Server{
		db:        db,
		cfg:       cfg,
		log:       log,
		avatarDir: filepath.Join(cfg.DataDir, "avatars"),
		outboxDir: filepath.Join(cfg.DataDir, "outbox"),
		attempts:  map[string][]time.Time{},
	}

	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// Register подключает маршруты авторизации к общему роутеру сервера.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /studio", s.handleStudio)
	mux.HandleFunc("GET /studio/{$}", s.handleStudio)
	mux.HandleFunc("GET /vault", s.handleVault)
	mux.HandleFunc("GET /vault/{$}", s.handleVault)
	mux.HandleFunc("POST /auth/register", s.handleRegister)
	mux.HandleFunc("POST /auth/login", s.handleLogin)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)
	mux.HandleFunc("POST /auth/profile", s.handleProfile)
	mux.HandleFunc("POST /auth/forgot", s.handleForgot)
	mux.HandleFunc("GET /reset", s.handleResetForm)
	mux.HandleFunc("POST /reset", s.handleResetApply)
	mux.HandleFunc("GET /avatars/", s.handleAvatar)
}

// Close закрывает базу.
func (s *Server) Close() error {
	return s.db.Close()
}

// --- общие помощники ---

// token возвращает случайную строку, пригодную для cookie и ссылок.
// crypto/rand, а не math/rand: токен нельзя предсказать.
func token() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashToken — то, что лежит в базе. Хэш, а не сам токен: утечка базы не
// даёт злоумышленнику готовые cookie (контрольный вопрос N5).
func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return fmt.Sprintf("%x", sum[:])
}

// isSecure — ставить ли cookie с флагом Secure. Локально сайт по HTTP, в
// проде nginx заканчивает TLS и передаёт X-Forwarded-Proto.
func isSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// setCookie ставит cookie с безопасными флагами.
func setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,                 // JavaScript не видит cookie — кража через XSS невозможна
		SameSite: http.SameSiteLaxMode, // чужой сайт не пришлёт нашу cookie вместе с POST
		Secure:   isSecure(r),          // по HTTPS cookie не уходит в открытом виде
	})
}

// clientIP — адрес клиента для ограничения частоты. X-Forwarded-For берём
// только если запрос пришёл с loopback (через nginx), иначе заголовок
// подделывается и ограничение обходится.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
			return fwd
		}
	}
	return host
}

// throttled фиксирует попытку и говорит, не слишком ли часто клиент ломится
// в формы. Старые записи вычищаются заодно.
func (s *Server) throttled(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-attemptsWindow)

	kept := s.attempts[ip][:0]
	for _, at := range s.attempts[ip] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}
	if len(kept) >= attemptsLimit {
		s.attempts[ip] = kept
		return true
	}
	s.attempts[ip] = append(kept, now)

	// Заодно прибираем адреса, которые давно не пытались войти: список не
	// должен расти бесконечно.
	if len(s.attempts)%50 == 0 {
		for key, times := range s.attempts {
			if len(times) == 0 || times[len(times)-1].Before(cutoff) {
				delete(s.attempts, key)
			}
		}
	}

	return false
}
