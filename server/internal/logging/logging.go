// Package logging - централизованное логирование сайта: события идут slog (текст локально,
// JSON в проде), действия дублируются в журнал ActionSink; о базах пакет не знает, сборка в main.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// ActionSink куда дублируются действия пользователей; реализация в main.
type ActionSink interface {
	// Action сохраняет одно действие; ошибка не должна ронять запрос.
	Action(login, action, ip string) error
}

// Config настройки логгера.
type Config struct {
	// Level уровень записей: debug|info|warn|error, по умолчанию info.
	Level string
	// Format формат консоли: text (по умолчанию) или json.
	Format string
	// Output куда писать консоль, по умолчанию os.Stderr.
	Output io.Writer
	// Actions журнал действий; пусто значит только консоль.
	Actions ActionSink
}

// Logger центральная точка логирования сервера.
type Logger struct {
	base    *slog.Logger
	actions ActionSink
}

// New собирает логгер; ошибка только при неправильном уровне или формате.
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

func (l *Logger) Debug(msg string, args ...any) { l.base.Debug(msg, args...) }

func (l *Logger) Info(msg string, args ...any) { l.base.Info(msg, args...) }

func (l *Logger) Warn(msg string, args ...any) { l.base.Warn(msg, args...) }

func (l *Logger) Error(msg string, args ...any) { l.base.Error(msg, args...) }

// Action записывает действие в общий лог и дубль в журнал; ошибка журнала
// не роняет запрос, она сама уходит в лог.
func (l *Logger) Action(login, action, ip string) {
	l.base.Info("действие", "login", login, "action", action, "ip", ip)

	if l.actions == nil {
		return
	}
	if err := l.actions.Action(login, action, ip); err != nil {
		l.base.Error("журнал действий: запись не сохранена", "err", err)
	}
}
