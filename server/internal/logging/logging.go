// Package logging — централизованное логирование сайта: одна точка входа
// вместо разбросанных log.Printf по всему коду.
//
// Logger создаётся один раз в main() и передаётся компонентам. Обычные
// события идут структурированными записями log/slog (текстом локально,
// JSON в проде — их понимают journalctl и агрегаторы). Действия
// пользователей — вход, регистрация, смена пароля — дополнительно
// дублируются в журнал действий для админ-панели через интерфейс
// ActionSink: реализация (таблица actions в пакете store) подключается
// в main. Сам пакет ничего не знает про базы — только slog и интерфейс.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// ActionSink — куда дублируются действия пользователей. Сегодня это
// пакет store (таблица actions); подменяется или отключается в main.
type ActionSink interface {
	// Action сохраняет одно действие. Ошибка не должна ронять запрос —
	// логгер её просто запишет.
	Action(login, action, ip string) error
}

// Config — настройки логгера.
type Config struct {
	// Level — уровень записей: debug|info|warn|error. По умолчанию info.
	Level string
	// Format — формат консоли: text (по умолчанию) или json.
	Format string
	// Output — куда писать консоль; по умолчанию os.Stderr.
	Output io.Writer
	// Actions — журнал действий пользователей. Пусто — действия пишутся
	// только в консоль (удобно для тестов).
	Actions ActionSink
}

// Logger — центральная точка логирования сервера.
type Logger struct {
	base    *slog.Logger
	actions ActionSink
}

// New собирает логгер по настройкам. Ошибка — только при неправильном
// уровне или формате: плохая конфигурация падает на старте, а не в проде.
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

	return &Logger{base: slog.New(handler), actions: cfg.Actions}, nil
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
// общий лог плюс дубль в журнал действий для будущей админ-панели.
// Ошибка журнала не роняет запрос — она сама уходит в лог.
func (l *Logger) Action(login, action, ip string) {
	l.base.Info("действие", "login", login, "action", action, "ip", ip)

	if l.actions == nil {
		return
	}
	if err := l.actions.Action(login, action, ip); err != nil {
		l.base.Error("журнал действий: запись не сохранена", "err", err)
	}
}
