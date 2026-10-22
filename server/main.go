// Команда site-server: окружение, хранилище, логгер, лимиты и HTTP-сервер.
// Единственный composition root: срезы связаны здесь явным DI через конфиг-структуры, store о срезах не знает.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/album"
	"github.com/ivan223/web1-personal-site/server/internal/auth"
	"github.com/ivan223/web1-personal-site/server/internal/healthz"
	"github.com/ivan223/web1-personal-site/server/internal/home"
	"github.com/ivan223/web1-personal-site/server/internal/leads"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/sdnotify"
	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// version - сборка, которую видно в /healthz: CI подставляет её флагом
// -ldflags "-X main.version=<sha>".
var version = "dev"

func main() {
	// Отдельный run, а не os.Exit внутри main: defer обязан отработать
	// даже на фатальной ошибке.
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "стоп:", err)
		os.Exit(1)
	}
}

func run() error {
	// Без переменной база молча создалась бы в рабочем каталоге: пустая база
	// со счётчиком в нуле выглядит как рабочий сайт. Лучше не подняться.
	dataDir := envOr("DATA_DIR", "")
	if dataDir == "" {
		return errors.New("DATA_DIR обязателен: без него база создалась бы в рабочем каталоге")
	}

	// Команда «снять копию базы» для деплоя: VACUUM INTO работает на живой
	// базе, сервис останавливать не нужно.
	if len(os.Args) == 3 && os.Args[1] == "backup" {
		return store.Snapshot(filepath.Join(dataDir, "site.db"), os.Args[2])
	}

	// «site-server check»: все startup-проверки без слушателя, деплой зовёт им
	// новый бинарник. Миграции идут на живую базу, поэтому деплой бэкапит раньше.
	checkOnly := len(os.Args) == 2 && os.Args[1] == "check"

	st, err := store.Open(dataDir)
	if err != nil {
		return fmt.Errorf("база: %w", err)
	}
	defer func() { _ = st.Close() }()

	logger, err := logging.New(logging.Config{
		Level:   envOr("LOG_LEVEL", "info"),
		Format:  envOr("LOG_FORMAT", "text"),
		Actions: journal{st: st},
	})
	if err != nil {
		return fmt.Errorf("логгер: %w", err)
	}

	// Импорт legacy-счётчика: без файла вызов безвреден и ничего не делает.
	if imported, err := st.ImportVisits(context.Background(), counterPath()); err != nil {
		logger.Warn("импорт счётчика не завершён", "err", err)
	} else if imported {
		logger.Info("счётчик перенесён из visits.dat в site.db")
	}

	// BASE_URL обязателен абсолютный: без схемы проверка Origin у формы
	// Sign Up отвергла бы все заявки (403).
	baseURL := strings.TrimRight(envOr("BASE_URL", "http://localhost:8080"), "/")
	if u, err := url.Parse(baseURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("BASE_URL должен быть абсолютным адресом вида https://example.com, получено %q", baseURL)
	}

	// Вся таблица бюджетов здесь: порог любого бюджета поднимается переменной
	// LIMIT_<БЮДЖЕТ> (LIMIT_AUTH_LOGIN=50), окно остаётся константой.
	budgets := map[string]webutil.Budget{
		auth.BudgetLogin:    {Limit: 20, Window: 10 * time.Minute},
		auth.BudgetRegister: {Limit: 20, Window: 10 * time.Minute},
		auth.BudgetForgot:   {Limit: 20, Window: 10 * time.Minute},
		leads.BudgetSignup:  {Limit: 5, Window: 10 * time.Minute},
		album.BudgetUpload:  {Limit: 20, Window: 10 * time.Minute},
	}
	for name, b := range budgets {
		budgets[name] = limitBudget(logger, name, b)
	}
	limits, err := webutil.NewLimits(budgets, logger)
	if err != nil {
		return fmt.Errorf("лимиты: %w", err)
	}
	names := make([]string, 0, len(budgets))
	for name := range budgets {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		b := budgets[name]
		logger.Info("лимит частоты", "бюджет", name, "лимит", b.Limit, "окно", b.Window)
	}

	authServer, err := auth.New(auth.Config{
		Users:         st,
		Sessions:      st,
		AvatarsDir:    filepath.Join(dataDir, "avatars"),
		OutboxDir:     filepath.Join(dataDir, "outbox"),
		BaseURL:       baseURL,
		Log:           logger,
		Throttle:      limits,
		AdminLogin:    os.Getenv("ADMIN_LOGIN"),
		AdminPassword: os.Getenv("ADMIN_PASSWORD"),
		SMTP: auth.SMTPConfig{
			Provider: os.Getenv("SMTP_PROVIDER"),
			Host:     os.Getenv("SMTP_HOST"),
			Port:     envInt("SMTP_PORT", 587, logger),
			Username: os.Getenv("SMTP_USER"),
			Password: os.Getenv("SMTP_PASS"),
			From:     os.Getenv("SMTP_FROM"),
		},
	})
	if err != nil {
		return err
	}

	leadsServer, err := leads.New(leads.Config{
		Store:    st,
		BaseURL:  baseURL,
		Log:      logger,
		Throttle: limits,
	})
	if err != nil {
		return err
	}

	// Проверки статики гоняются в home.New: битый staticRoot валит запуск.
	homeServer, err := home.New(home.Config{
		StaticRoot: envOr("STATIC_ROOT", publicRoot()),
		Log:        logger,
		Store:      st,
	})
	if err != nil {
		return err
	}

	// Закрытую страницу загрузки охраняет authServer: срез альбома про сессии
	// ничего не знает, ему передаётся готовый шов Access.
	albumServer, err := album.New(album.Config{
		Store:     st,
		PhotosDir: filepath.Join(dataDir, "photos"),
		Access:    authServer,
		Throttle:  limits,
		Log:       logger,
	})
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	authServer.Register(mux)
	leadsServer.Register(mux)
	homeServer.Register(mux)
	albumServer.Register(mux)
	mux.Handle("/healthz", healthz.Handler(version, store.Version()))

	if checkOnly {
		logger.Info("проверка старта пройдена: бинарник готов к рестарту")
		return nil
	}

	logger.Info("Ivan's Little Web запущен", "addr", "http://localhost:8080")

	// ReadTimeout/WriteTimeout не ставим специально: общий таймер оборвал бы
	// и загрузку аватара, и отдачу статики на медленном канале.
	srv := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	// Слушаем только localhost (наружу nginx): прямой доступ к порту
	// включал бы обход лимитеров подделкой X-Real-IP.
	// Слушатель отдельной строкой: менеджеру о готовности сказывают, когда порт
	// открыт (sd_notify, Type=notify), а не когда процесс вызван.
	ln, err := net.Listen("tcp", "127.0.0.1:8080")
	if err != nil {
		return fmt.Errorf("слушатель: %w", err)
	}
	if err := sdnotify.Ready(); err != nil {
		logger.Warn("sd_notify не ушёл менеджеру", "err", err)
	}
	return srv.Serve(ln)
}

// --- окружение и адаптеры ---

// journal адаптер журнала действий (logging.ActionSink) поверх store;
// конкретные реализации собираются в main.
type journal struct {
	st *store.Store
}

func (j journal) Action(login, action, ip string) error {
	// Действие уже случилось: обрыв соединения браузером не должен его
	// отменить, поэтому контекст без отмены.
	return j.st.Record(
		context.WithoutCancel(context.Background()),
		store.Action{Login: login, Action: action, IP: ip},
	)
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// publicRoot ищет каталог статики, когда STATIC_ROOT не задан: локально запускают
// и из корня, и из server/, в проде путь абсолютный из юнита.
func publicRoot() string {
	for _, dir := range []string{"public", filepath.Join("..", "public")} {
		if fi, err := os.Stat(filepath.Join(dir, "index.html")); err == nil && !fi.IsDir() {
			return dir
		}
	}
	return "public"
}

func envInt(name string, fallback int, log *logging.Logger) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		log.Warn("переменная окружения не число", "name", name, "value", v, "fallback", fallback)
		return fallback
	}
	return n
}

// limitBudget порог бюджета ограничителей с переопределением из окружения:
// auth.login → LIMIT_AUTH_LOGIN. Мусор не роняет запуск, берём дефолт.
func limitBudget(log *logging.Logger, name string, def webutil.Budget) webutil.Budget {
	env := "LIMIT_" + strings.ToUpper(strings.ReplaceAll(name, ".", "_"))
	v := os.Getenv(env)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		log.Warn("лимит: переменная окружения не положительное число",
			"переменная", env, "значение", v, "взят дефолт", def.Limit)
		return def
	}
	return webutil.Budget{Limit: n, Window: def.Window}
}

// counterPath старый файл со счётчиком: читается только при импорте, в проде
// путь подставляет systemd из StateDirectory.
func counterPath() string {
	if p := os.Getenv("VISITS_FILE"); p != "" {
		return p
	}
	return "visits.dat"
}
