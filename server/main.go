// Команда site-server — единственная точка сборки сайта: здесь читаются
// переменные окружения, собираются хранилище, логгер, лимиты и срезы
// (auth, leads, home), после чего поднимается HTTP-сервер.
//
// Срезы не знают друг о друге: зависимости приходят сюда явным DI через
// конфиг-структуры. store — общий слой SQL, о срезах он не знает.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/auth"
	"github.com/ivan223/web1-personal-site/server/internal/home"
	"github.com/ivan223/web1-personal-site/server/internal/leads"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

func main() {
	// Отдельный run, а не os.Exit внутри main: defer (закрытие базы)
	// обязан отработать даже на фатальной ошибке.
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "стоп:", err)
		os.Exit(1)
	}
}

func run() error {
	dataDir := envOr("DATA_DIR", "var")

	// Отдельная команда «снять копию базы»: ею пользуется деплой перед
	// рестартом. Копия снимается на живой базе (VACUUM INTO) — сервис
	// останавливать не нужно.
	if len(os.Args) == 3 && os.Args[1] == "backup" {
		return store.Snapshot(filepath.Join(dataDir, "site.db"), os.Args[2])
	}

	// База открывается первой: логгер пишет её ошибки и дублирует действия
	// пользователей в её журнал, поэтому создаётся уже с открытым store.
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

	// Перенос счётчика из старого visits.dat — единственное, что осталось
	// от прежних хранилищ. Без файла вызов безвреден и ничего не делает.
	if imported, err := st.ImportVisits(context.Background(), counterPath()); err != nil {
		// Сайт работать может и без импорта — разберёмся руками.
		logger.Warn("импорт счётчика не завершён", "err", err)
	} else if imported {
		logger.Info("счётчик перенесён из visits.dat в site.db")
	}

	// BASE_URL обязан быть абсолютным: на нём держится проверка Origin
	// у формы Sign Up — при BASE_URL без схемы все заявки ушли бы в 403.
	baseURL := strings.TrimRight(envOr("BASE_URL", "http://localhost:8080"), "/")
	if u, err := url.Parse(baseURL); err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("BASE_URL должен быть абсолютным адресом вида https://example.com, получено %q", baseURL)
	}

	// Ограничители частоты: вся таблица бюджетов здесь, в одном месте —
	// срезы присылают только имена (auth.BudgetLogin и т.п.). Порог любого
	// бюджета поднимается переменной LIMIT_<БЮДЖЕТ> (LIMIT_AUTH_LOGIN=50),
	// окно остаётся константой. Опечатка в имени или нулевой порог валит
	// запуск, а не включает молча «лимита нет».
	budgets := map[string]webutil.Budget{
		auth.BudgetLogin:    {Limit: 20, Window: 10 * time.Minute},
		auth.BudgetRegister: {Limit: 20, Window: 10 * time.Minute},
		auth.BudgetForgot:   {Limit: 20, Window: 10 * time.Minute},
		leads.BudgetSignup:  {Limit: 5, Window: 10 * time.Minute},
	}
	for name, b := range budgets {
		budgets[name] = limitBudget(logger, name, b)
	}
	limits, err := webutil.NewLimits(budgets, logger)
	if err != nil {
		return fmt.Errorf("лимиты: %w", err)
	}
	// Список бюджетов в лог при старте: что ограничено и насколько —
	// видно сразу, а не из кода.
	names := make([]string, 0, len(budgets))
	for name := range budgets {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		b := budgets[name]
		logger.Info("лимит частоты", "бюджет", name, "лимит", b.Limit, "окно", b.Window)
	}

	// Закрытый раздел: регистрация, авторизация и восстановление пароля
	// (лабораторная N4). Данные — в общей базе (store), почта — в SMTP_*.
	authServer, err := auth.New(auth.Config{
		Users:      st,
		Sessions:   st,
		AvatarsDir: filepath.Join(dataDir, "avatars"),
		OutboxDir:  filepath.Join(dataDir, "outbox"),
		BaseURL:    baseURL,
		Log:        logger,
		Throttle:   limits,
		SMTP: auth.SMTPConfig{
			// Провайдер (gmail|yandex|mailru) подставляет хост и порт
			// сам; свой сервер задаётся через SMTP_HOST + SMTP_PORT.
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

	// Публичная форма заявок со страницы /maket.
	leadsServer, err := leads.New(leads.Config{
		Store:    st,
		BaseURL:  baseURL,
		Log:      logger,
		Throttle: limits,
	})
	if err != nil {
		return err
	}

	// Витрина: главная со счётчиком и «красивые» адреса. Проверки вёрстки
	// прогоняются в home.New — битый staticRoot валит запуск, а не отдаёт
	// посетителю пустую страницу.
	homeServer, err := home.New(home.Config{
		StaticRoot: envOr("STATIC_ROOT", "public"),
		Log:        logger,
		Store:      st,
	})
	if err != nil {
		return err
	}

	// Свой роутер, а не http.DefaultServeMux: маршруты видны в одном месте,
	// случайная регистрация «в никуда» невозможна.
	mux := http.NewServeMux()
	authServer.Register(mux)
	leadsServer.Register(mux)
	homeServer.Register(mux)

	logger.Info("Ivan's Little Web запущен", "addr", "http://localhost:8080")

	// Не хватает http.ListenAndServe только по одному аргументу: у него нет
	// таймаутов, и висящее соединение держит горутину вечно. ReadTimeout
	// и WriteTimeout не ставим специально: общий таймер оборвал бы и
	// загрузку аватара, и отдачу статики на медленном канале.
	srv := &http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	// Слушаем только localhost: наружу сайт открывает nginx. Прямой доступ
	// к порту включал бы обход лимитеров подделкой X-Real-IP.
	return srv.ListenAndServe()
}

// --- окружение и адаптеры: живут только в точке сборки ---

// journal — адаптер «журнал действий» (logging.ActionSink) поверх store.
// Живёт здесь, в main, потому что main — единственное место, где
// собираются конкретные реализации: пакет logging об интерфейсе знает,
// о базе — нет.
type journal struct {
	st *store.Store
}

func (j journal) Action(login, action, ip string) error {
	// Действие уже случилось; обрыв соединения браузером не должен его
	// отменить — поэтому контекст без отмены.
	return j.st.Record(
		context.WithoutCancel(context.Background()),
		store.Action{Login: login, Action: action, IP: ip},
	)
}

// envOr читает переменную окружения, подставляя значение по умолчанию.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// envInt читает числовую переменную окружения. Мусор вместо числа — не
// повод не запускаться: берём запасное значение и предупреждаем в лог.
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

// limitBudget — порог бюджета ограничителей с переопределением из
// окружения. Имя переменной — бюджет в верхнем регистре, с точками на
// подчёркивания: auth.login → LIMIT_AUTH_LOGIN. Мусор не роняет запуск:
// берём дефолт и пишем warn (как envInt).
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

// counterPath — старый файл со счётчиком (visits.dat). Читается только
// при импорте legacy-данных в site.db; в рабочем процессе участия не
// принимает. В проде systemd подставляет путь из StateDirectory.
func counterPath() string {
	if p := os.Getenv("VISITS_FILE"); p != "" {
		return p
	}
	return "visits.dat"
}
