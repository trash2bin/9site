// Тесты витрины, поднятой через настоящий home.Register: главная со
// счётчиком, фолбэк при сбое базы и красивые адреса страниц.
package home_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/home"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// writeStatic собирает минимальный staticRoot: все файлы, которых требуют
// проверки home.New, плюс вёрстка с метками счётчика и «спасибо».
func writeStatic(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	files := map[string]string{
		"index.html":          `<html><!--visits--></html>`,
		"design.html":         `<!--sent-->`,
		"catalog.html":        `<html></html>`,
		"style.css":           ``,
		"design.css":          ``,
		"catalog.css":         ``,
		"print.css":           ``,
		"js/main.js":          ``,
		"css/base.css":        ``,
		"css/auth/auth.css":   ``,
		"assets/img/logo.png": ``,
	}
	for name, body := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func testMux(t *testing.T, counter home.Counter) *http.ServeMux {
	t.Helper()

	log, err := logging.New(logging.Config{Output: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := home.New(home.Config{StaticRoot: writeStatic(t), Log: log, Store: counter})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.Register(mux)
	return mux
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// TestIndexCounter: главная отдаётся с подставленным числом визитов.
func TestIndexCounter(t *testing.T) {
	st := openStore(t)
	t.Cleanup(func() { _ = st.Close() })

	w := get(t, testMux(t, st), "/")
	if w.Code != http.StatusOK {
		t.Fatalf("статус %d, ожидался 200", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "<span>0</span><span>0</span><span>0</span><span>0</span><span>1</span>") {
		t.Fatalf("счётчик не подставлен: %q", body)
	}
}

// TestIndexFallbackOnDBFailure: сбой базы не должен ронять главную:
// страница рисуется с последним известным числом визитов.
func TestIndexFallbackOnDBFailure(t *testing.T) {
	st := openStore(t)
	mux := testMux(t, st)

	get(t, mux, "/") // счётчик = 1, lastVisits = 1
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	w := get(t, mux, "/")
	if w.Code != http.StatusOK {
		t.Fatalf("статус %d, ожидался 200 при сбое базы", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "<span>0</span><span>0</span><span>0</span><span>0</span><span>1</span>") {
		t.Fatalf("после сбоя базы ожидалось последнее число 1: %q", body)
	}
}

// TestPageRoutes: красивые адреса и подстановка «спасибо» по ?sent=1.
func TestPageRoutes(t *testing.T) {
	st := openStore(t)
	t.Cleanup(func() { _ = st.Close() })
	mux := testMux(t, st)

	if body := get(t, mux, "/maket").Body.String(); !strings.Contains(body, "<!--sent-->") {
		t.Fatalf("/maket: метка «спасибо» потерялась: %q", body)
	}
	if body := get(t, mux, "/maket?sent=1").Body.String(); !strings.Contains(body, "Спасибо") {
		t.Fatalf("/maket?sent=1: блок «спасибо» не подставлен: %q", body)
	}
	if w := get(t, mux, "/catalog"); w.Code != http.StatusOK {
		t.Fatalf("/catalog: статус %d, ожидался 200", w.Code)
	}
	if w := get(t, mux, "/maket/"); w.Code != http.StatusOK {
		t.Fatalf("/maket/: статус %d, ожидался 200", w.Code)
	}
}
