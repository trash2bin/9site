// Package logging — централизованное логирование сайта: одна точка входа
// вместо разбросанных log.Printf по всему коду.
//
// Logger создаётся один раз в main() и передаётся компонентам. Обычные
// события идут структурированными записями log/slog (текстом локально,
// JSON в проде — их понимают journalctl и агрегаторы). Действия
// пользователей — вход, регистрация, смена пароля — дополнительно
// пишутся в журнал действий (SQLite), который потом показывает
// админ-панель.
package logging

import (
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	// Чистый Go-драйвер SQLite: без CGO, деплой остаётся одним бинарником.
	_ "modernc.org/sqlite"
)

// Config — настройки логгера.
type Config struct {
	// DataDir — каталог для журнала действий (actions.db). Пусто —
	// действия пишутся только в консоль (удобно для тестов).
	DataDir string
	// Level — уровень записей: debug|info|warn|error. По умолчанию info.
	Level string
	// Format — формат консоли: text (по умолчанию) или json.
	Format string
	// Output — куда писать консоль; по умолчанию os.Stderr.
	Output io.Writer
}

// Logger — центральная точка логирования сервера.
type Logger struct {
	base *slog.Logger
	db   *sql.DB // nil, если журнал действий не настроен
}

// New собирает логгер по настройкам. Ошибка — только про журнал действий:
// нерабочий уровень или формат отваливаются на старте, а не в проде.
func New(cfg Config) (*Logger, error) {
	out := cfg.Output
	if out == nil {
		out = os.Stderr
	}

	level := slog.LevelInfo
	if cfg.Level != "" {
		if err := level.UnmarshalText([]byte(strings.ToLower(cfg.Level))); err != nil {
			return nil, fmt.Errorf("уровень лога %q: %w", cfg.Level, err)
		}
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.EqualFold(cfg.Format, "json") {
		handler = slog.NewJSONHandler(out, opts)
	} else {
		handler = slog.NewTextHandler(out, opts)
	}

	l := &Logger{base: slog.New(handler)}

	if cfg.DataDir != "" {
		db, err := openActions(filepath.Join(cfg.DataDir, "actions.db"))
		if err != nil {
			return nil, err
		}
		l.db = db
	}

	return l, nil
}

// openActions открывает журнал действий и готовит схему. IF NOT EXISTS
// делает запуск идемпотентным, как и в auth-базе.
func openActions(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	const schema = `
CREATE TABLE IF NOT EXISTS actions (
  id     INTEGER PRIMARY KEY AUTOINCREMENT,
  at     TEXT NOT NULL DEFAULT (datetime('now')),
  login  TEXT NOT NULL,
  action TEXT NOT NULL,
  ip     TEXT NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("схема журнала действий: %w", err)
	}
	return db, nil
}

// Debug пишет отладочную запись.
func (l *Logger) Debug(msg string, args ...any) { l.base.Debug(msg, args...) }

// Info пишет обычную запись.
func (l *Logger) Info(msg string, args ...any) { l.base.Info(msg, args...) }

// Warn пишет предупреждение.
func (l *Logger) Warn(msg string, args ...any) { l.base.Warn(msg, args...) }

// Error пишет ошибку.
func (l *Logger) Error(msg string, args ...any) { l.base.Error(msg, args...) }

// Action записывает действие пользователя: структурированная строка в
// общий лог плюс строка в журнале действий для будущей админ-панели.
// Ошибка журнала не роняет запрос — она сама уходит в лог.
func (l *Logger) Action(login, action, ip string) {
	l.base.Info("действие", "login", login, "action", action, "ip", ip)

	if l.db == nil {
		return
	}
	if _, err := l.db.Exec(
		`INSERT INTO actions (login, action, ip) VALUES (?, ?, ?)`,
		login, action, ip,
	); err != nil {
		l.base.Error("журнал действий: запись не сохранена", "err", err)
	}
}

// Entry — строка журнала действий.
type Entry struct {
	At     string
	Login  string
	Action string
	IP     string
}

// Recent отдаёт последние действия пользователей, свежие первыми. Пока
// журнал смотрят через sqlite3; когда появится админ-панель — она будет
// брать данные именно отсюда.
func (l *Logger) Recent(limit int) ([]Entry, error) {
	if l.db == nil {
		return nil, nil
	}

	rows, err := l.db.Query(
		`SELECT at, login, action, ip FROM actions ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.At, &e.Login, &e.Action, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Close закрывает журнал действий.
func (l *Logger) Close() error {
	if l.db == nil {
		return nil
	}
	return l.db.Close()
}
