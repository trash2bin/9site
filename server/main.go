package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

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

	// signupLimit — ограничитель частоты заявок с публичной формы.
	signupLimit *webutil.Limiter
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

	a := &app{
		log:         logger,
		store:       st,
		baseURL:     baseURL,
		signupLimit: webutil.NewLimiter(signupLimit, signupWindow),
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

	// Форма Sign Up со страницы /maket.
	mux.HandleFunc("POST /signup", a.signupHandler)

	a.log.Info("Ivan's Little Web запущен", "addr", "http://localhost:8080")
	// Слушаем только localhost: наружу сайт открывает nginx. Прямой доступ
	// к порту включал бы обход лимитеров подделкой X-Real-IP.
	return http.ListenAndServe("127.0.0.1:8080", mux)
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
