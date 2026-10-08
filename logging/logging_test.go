package logging

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestActionJournal — действия пользователей попадают в журнал и
// читаются обратно, свежие первыми.
func TestActionJournal(t *testing.T) {
	log, err := New(Config{DataDir: t.TempDir(), Output: io.Discard})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer log.Close()

	log.Action("demo", "вход", "127.0.0.1")
	log.Action("demo", "выход", "127.0.0.1")

	entries, err := log.Recent(10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("в журнале %d записей, ждали 2", len(entries))
	}
	if entries[0].Action != "выход" {
		t.Fatalf("первая свежая запись %q, ждали «выход»", entries[0].Action)
	}
	if entries[0].Login != "demo" || entries[0].IP != "127.0.0.1" {
		t.Fatalf("запись прочитана криво: %+v", entries[0])
	}
}

// TestLevel — уровень info прячет debug-записи из консоли.
func TestLevel(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(Config{Level: "info", Output: &buf})
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
