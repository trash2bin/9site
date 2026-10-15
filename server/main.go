package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/auth"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// app — состояние сервера: логгер, хранилище, последнее известное число
// визитов и ограничитель частоты заявок. Методы разложены по файлам:
// counter.go (счётчик), handlers.go (страницы), signup.go (форма),
// static.go (проверки статики на старте).
type app struct {
	log   *logging.Logger
	store *store.Store

	// baseURL — внешний адрес сайта; его же Origin присылает браузер
	// в заголовке Origin (проверка формы Sign Up).
	baseURL string

	// lastVisits — последнее удавшееся число счётчика: страница рисуется
	// с ним, если запись в базу не удалась. Атомик, потому что запросы
	// приходят параллельно.
	lastVisits atomic.Int64

	// limits — централизованные ограничители частоты: одна таблица
	// бюджетов на весь процесс (собирается в run), вешается на маршруты.
	limits *webutil.Limits
}

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
	// рестартом. Миграции накатываются при старте нового бинарника, поэтому
	// настоящий откат возможен только со снимком, снятым ДО старта. Копия
	// снимается на живой базе (VACUUM INTO) — сервис останавливать не нужно.
	if len(os.Args) == 3 && os.Args[1] == "backup" {
		return store.Snapshot(filepath.Join(dataDir, "site.db"), os.Args[2])
	}

	// База открывается первой: логгер пишет её ошибки и дублирует действия
	// пользователей в её журнал, поэтому создаётся уже с открытым store.
	// Ошибки на этом этапе уходят в stderr — логгера ещё нет.
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

	// Перенос счётчика из старого visits.dat — единственное, что остаётся
	// от прежних хранилищ (на деплое других старых данных и не было).
	// Файл после вливки уходит в .bak, поэтому без visits.dat вызов
	// безвреден и не делает ничего.
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
	// срезы присылают только имена (auth.BudgetLogin и т.п.), порогов в
	// своих пакетах больше нет. Порог любого бюджета переопределяется
	// переменной окружения с тем же именем в верхнем регистре
	// (LIMIT_AUTH_LOGIN=50), окно остаётся константой. Опечатка в имени
	// или нулевой порог валит запуск, а не включает молча «лимита нет».
	budgets := map[string]webutil.Budget{
		auth.BudgetLogin:    {Limit: 20, Window: 10 * time.Minute},
		auth.BudgetRegister: {Limit: 20, Window: 10 * time.Minute},
		auth.BudgetForgot:   {Limit: 20, Window: 10 * time.Minute},
		signupBudget:        {Limit: signupLimit, Window: signupWindow},
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

	a := &app{
		log:     logger,
		store:   st,
		baseURL: baseURL,
		limits:  limits,
	}

	if err := a.checkIndex(); err != nil {
		return err
	}
	if err := a.checkPages(); err != nil {
		return err
	}
	if err := a.checkStyles(); err != nil {
		return err
	}
	if err := a.checkExtraStatic(); err != nil {
		return err
	}

	// Закрытый раздел: регистрация, авторизация и восстановление пароля
	// (лабораторная N4). Данные — в общей базе (store), почта — в SMTP_*.
	// Если SMTP не настроен, письма сохраняются в файл и ссылка показывается
	// на экране.
	authServer, err := auth.New(auth.Config{
		Users:      st,
		Sessions:   st,
		AvatarsDir: filepath.Join(dataDir, "avatars"),
		OutboxDir:  filepath.Join(dataDir, "outbox"),
		BaseURL:    a.baseURL,
		Log:        logger,
		Throttle:   limits,
		SMTP: auth.SMTPConfig{
			// Провайдер (gmail|yandex|mailru) подставляет хост и порт
			// сам; свой сервер задаётся через SMTP_HOST + SMTP_PORT.
			Provider: os.Getenv("SMTP_PROVIDER"),
			Host:     os.Getenv("SMTP_HOST"),
			Port:     a.envInt("SMTP_PORT", 587),
			Username: os.Getenv("SMTP_USER"),
			Password: os.Getenv("SMTP_PASS"),
			From:     os.Getenv("SMTP_FROM"),
		},
	})
	if err != nil {
		return err
	}

	// Свой роутер, а не http.DefaultServeMux: маршруты видны в одном
	// месте, случайная регистрация «в никуда» невозможна.
	mux := http.NewServeMux()
	authServer.Register(mux)

	fileServer := http.FileServer(http.Dir(staticRoot))

	// "/" и "/index.html" обслуживает счётчик, всё остальное — статика.
	// В ServeMux точный шаблон побеждает шаблон-префикс "/", поэтому
	// порядок регистрации значения не имеет.
	mux.Handle("/", alwaysRevalidate(staticOnly(fileServer)))
	mux.HandleFunc("GET /{$}", a.indexHandler)
	mux.HandleFunc("GET /index.html", a.indexHandler)

	// Красивые адреса: /maket и /maket/ отдают ту же вёрстку.
	// Вариант с косой чертой нужен, чтобы ссылка вида /maket/ не падала в 404.
	for route, file := range pageRoutes {
		mux.HandleFunc("GET "+route, a.pageHandler(file))
		mux.HandleFunc("GET "+route+"/{$}", a.pageHandler(file))
	}

	// Форма Sign Up со страницы /maket — под бюджетом form.signup.
	mux.Handle("POST /signup", a.limits.Middleware(signupBudget,
		http.HandlerFunc(a.signupTooMany), http.HandlerFunc(a.signupHandler)))

	a.log.Info("Ivan's Little Web запущен", "addr", "http://localhost:8080")

	// Не хватает http.ListenAndServe только по одному аргументу: у него нет
	// таймаутов, и висящее соединение держит горутину вечно. Настройки
	// минимальные и продуманные: ReadHeaderTimeout гасит slowloris на
	// заголовках, IdleTimeout — забытые keep-alive-соединения. ReadTimeout
	// и WriteTimeout не ставим специально: общий таймер оборвал бы и
	// загрузку аватара, и отдачу статики на медленном канале; тело запроса
	// и так ограничивает nginx (client_max_body_size).
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
