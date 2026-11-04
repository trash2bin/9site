package logging_test

import (
	"io"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
)

func TestRegistrarMatchesDir(t *testing.T) {
	reg := logging.NewRegistrar("tests")
	ev := reg.Register("dir.match", "проверка каталога")

	if ev.Name() != "tests.dir.match" || ev.Desc() != "проверка каталога" {
		t.Fatalf("событие собрано криво: %q %q", ev.Name(), ev.Desc())
	}
}

func TestForeignRegistrarSkipsDir(t *testing.T) {
	ev := logging.NewForeignRegistrar("system").Register("thing", "чужой домен")
	if ev.Name() != "system.thing" {
		t.Fatalf("имя: %q", ev.Name())
	}
}

func TestWrongDirPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("префикс мимо каталога не упал")
		}
	}()
	logging.NewRegistrar("wrongdir")
}

func TestDuplicatePanics(t *testing.T) {
	reg := logging.NewForeignRegistrar("dup")
	reg.Register("once", "единожды")

	defer func() {
		if recover() == nil {
			t.Fatal("дубликат имени не упал")
		}
	}()
	reg.Register("once", "снова")
}

func TestEmptyDescPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("пустое описание не упало")
		}
	}()
	logging.NewForeignRegistrar("nodesc").Register("thing", "   ")
}

func TestBadNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("кривое имя не упало")
		}
	}()
	logging.NewForeignRegistrar("badname").Register("UPPER", "описание")
}

func TestEventsSortedAndDescribed(t *testing.T) {
	reg := logging.NewForeignRegistrar("sort")
	reg.Register("b.thing", "второе")
	reg.Register("a.thing", "первое")

	all := logging.Events()
	if len(all) < 2 {
		t.Fatalf("в реестре %d событий, ждали минимум два", len(all))
	}
	for i, ev := range all {
		if strings.TrimSpace(ev.Desc()) == "" {
			t.Fatalf("событие %q без описания", ev.Name())
		}
		if i > 0 && all[i-1].Name() >= ev.Name() {
			t.Fatalf("реестр не отсортирован: %q перед %q", all[i-1].Name(), ev.Name())
		}
	}
}

func TestActionWithoutEventSkipsJournal(t *testing.T) {
	sink := &fakeSink{}
	log, err := logging.New(logging.Config{Output: io.Discard, Actions: sink})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	log.Action(logging.Action{Login: "demo", Result: logging.ResultSuccess})

	if len(sink.entries) != 0 {
		t.Fatalf("действие без имени события уехало в журнал: %+v", sink.entries)
	}
}
