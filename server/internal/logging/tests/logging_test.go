package logging_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
)

// fakeSink — журнал в памяти: проверяем, что действия доходят, а ошибка
// не роняет логгер.
type fakeSink struct {
	entries []string
	fail    bool
}

func (f *fakeSink) Action(login, action, ip string) error {
	if f.fail {
		return errors.New("журнал недоступен")
	}
	f.entries = append(f.entries, login+"/"+action+"/"+ip)
	return nil
}

// TestActionToSink — действия пользователей уходят в журнал действий.
func TestActionToSink(t *testing.T) {
	sink := &fakeSink{}
	log, err := logging.New(logging.Config{Output: io.Discard, Actions: sink})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Action("demo", "вход", "127.0.0.1")
	log.Action("demo", "выход", "127.0.0.1")

	if len(sink.entries) != 2 {
		t.Fatalf("в журнале %d записей, ждали 2", len(sink.entries))
	}
	if sink.entries[0] != "demo/вход/127.0.0.1" || sink.entries[1] != "demo/выход/127.0.0.1" {
		t.Fatalf("журнал не тот: %v", sink.entries)
	}
}

// TestActionSinkError — сломанный журнал логируется, но не ломает вызов.
func TestActionSinkError(t *testing.T) {
	var buf bytes.Buffer
	log, err := logging.New(logging.Config{Output: &buf, Actions: &fakeSink{fail: true}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Action("demo", "вход", "127.0.0.1") // не должно упасть

	if !strings.Contains(buf.String(), "запись не сохранена") {
		t.Fatalf("ошибка журнала не попала в лог:\n%s", buf.String())
	}
}

// TestActionWithoutSink — без журнала Action работает как обычная запись.
func TestActionWithoutSink(t *testing.T) {
	log, err := logging.New(logging.Config{Output: io.Discard})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Action("demo", "вход", "127.0.0.1")
}

// TestLevel — уровень info прячет debug-записи из консоли.
func TestLevel(t *testing.T) {
	var buf bytes.Buffer
	log, err := logging.New(logging.Config{Level: "info", Output: &buf})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Debug("невидимая запись")
	log.Info("видимая запись")

	out := buf.String()
	if strings.Contains(out, "невидимая") {
		t.Fatalf("debug-запись прошла при уровне info:\n%s", out)
	}
	if !strings.Contains(out, "видимая запись") {
		t.Fatalf("info-запись потерялась:\n%s", out)
	}
}
