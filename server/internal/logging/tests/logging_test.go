package logging_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
)

var (
	testEvents = logging.NewRegistrar("tests")
	evLogin    = testEvents.Register("login", "вход в тесте")
	evLogout   = testEvents.Register("logout", "выход в тесте")
)

// fakeSink - журнал в памяти, fail имитирует поломку журнала.
type fakeSink struct {
	entries []logging.Action
	fail    bool
}

func (f *fakeSink) Action(action logging.Action) error {
	if f.fail {
		return errors.New("journal unavailable")
	}
	f.entries = append(f.entries, action)
	return nil
}

func TestActionToSink(t *testing.T) {
	sink := &fakeSink{}
	log, err := logging.New(logging.Config{Output: io.Discard, Actions: sink})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Action(logging.Action{Login: "demo", Event: evLogin, Result: "success", IP: "127.0.0.1"})
	log.Action(logging.Action{Login: "demo", Event: evLogout, Result: "success", IP: "127.0.0.1"})

	if len(sink.entries) != 2 {
		t.Fatalf("в журнале %d записей, ждали 2", len(sink.entries))
	}
	if sink.entries[0].Event.Name() != evLogin.Name() || sink.entries[1].Event.Name() != evLogout.Name() {
		t.Fatalf("журнал не тот: %v", sink.entries)
	}
	if sink.entries[0].Login != "demo" || sink.entries[0].IP != "127.0.0.1" {
		t.Fatalf("запись прочитана криво: %+v", sink.entries[0])
	}
}

func TestActionSinkError(t *testing.T) {
	var buf bytes.Buffer
	log, err := logging.New(logging.Config{Output: &buf, Actions: &fakeSink{fail: true}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Action(logging.Action{Login: "demo", Event: evLogin, Result: "success", IP: "127.0.0.1"})

	if !strings.Contains(buf.String(), "action journal write failed") {
		t.Fatalf("ошибка журнала не попала в лог:\n%s", buf.String())
	}
}

func TestActionWithoutSink(t *testing.T) {
	log, err := logging.New(logging.Config{Output: io.Discard})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Action(logging.Action{Login: "demo", Event: evLogin, Result: "success", IP: "127.0.0.1"})
}

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
	if !strings.Contains(out, "видимая") {
		t.Fatalf("info-запись потерялась:\n%s", out)
	}
}
