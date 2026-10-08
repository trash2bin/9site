package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/ivan223/web1-personal-site/auth"
	"github.com/ivan223/web1-personal-site/logging"
)

// app — состояние сервера: центральный логгер и счётчик визитов. Методы
// разложены по файлам: counter.go (счётчик), handlers.go (страницы),
// static.go (проверки статики на старте).
type app struct {
	log *logging.Logger

	// Счётчик визитов. Мьютекс — потому что запросы приходят параллельно,
	// а число живёт в памяти и на диске одновременно.
	mu     sync.Mutex
	visits int
}

func main() {
	// Логгер создаётся здесь, в точке входа, и дальше раздаётся
	// компонентам: никаких log.Printf по всему коду.
	logger, err := logging.New(logging.Config{
		DataDir: envOr("DATA_DIR", "var"),
		Level:   envOr("LOG_LEVEL", "info"),
		Format:  envOr("LOG_FORMAT", "text"),
	})
	if err != nil {
		// Логгера ещё нет — остаётся только stderr.
		fmt.Fprintln(os.Stderr, "логгер:", err)
		os.Exit(1)
	}
	defer logger.Close()

	a := &app{log: logger}

	a.loadCounter()
	if err := a.checkIndex(); err != nil {
		a.fatal(err)
	}
	if err := a.checkPages(); err != nil {
		a.fatal(err)
	}
	if err := a.checkStyles(); err != nil {
		a.fatal(err)
	}
	if err := a.checkExtraStatic(); err != nil {
		a.fatal(err)
	}

	// Закрытый раздел: регистрация, авторизация и восстановление пароля
	// (лабораторная N4). Данные живут в DATA_DIR, почта — в SMTP_*. Если SMTP
	// не настроен, письма сохраняются в файл и ссылка показывается на экране.
	authServer, err := auth.New(auth.Config{
		DataDir: envOr("DATA_DIR", "var"),
		BaseURL: strings.TrimRight(envOr("BASE_URL", "http://localhost:8080"), "/"),
		Log:     logger,
		SMTP: auth.SMTPConfig{
			Host:     os.Getenv("SMTP_HOST"),
			Port:     a.envInt("SMTP_PORT", 587),
			Username: os.Getenv("SMTP_USER"),
			Password: os.Getenv("SMTP_PASS"),
			From:     os.Getenv("SMTP_FROM"),
		},
	})
	if err != nil {
		a.fatal(err)
	}
	defer authServer.Close()
	authServer.Register(http.DefaultServeMux)

	fileServer := http.FileServer(http.Dir(staticRoot))

	// "/" и "/index.html" обслуживает счётчик, всё остальное — статика.
	// В ServeMux точный шаблон побеждает шаблон-префикс "/", поэтому
	// порядок регистрации значения не имеет.
	http.Handle("/", alwaysRevalidate(staticOnly(fileServer)))
	http.HandleFunc("/{$}", a.indexHandler)
	http.HandleFunc("/index.html", a.indexHandler)

	// Красивые адреса: /maket и /maket/ отдают ту же вёрстку.
	// Вариант с косой чертой нужен, чтобы ссылка вида /maket/ не падала в 404.
	for route, file := range pageRoutes {
		http.HandleFunc(route, a.pageHandler(file))
		http.HandleFunc(route+"/{$}", a.pageHandler(file))
	}

	a.log.Info("Ivan's Little Web запущен", "addr", "http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		a.fatal(err)
	}
}

// fatal пишет ошибку в лог и завершает процесс: сервер без страниц или без
// базы запускать бессмысленно.
func (a *app) fatal(err error) {
	a.log.Error("стоп", "err", err)
	os.Exit(1)
}
