// Package album_test тесты среза альбома по экспортному API.
package album_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/album"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// testEnv поднятый сервер альбома с настоящей базой во временном каталоге.
type testEnv struct {
	ts        *httptest.Server
	st        *store.Store
	photosDir string
}

// fakeAccess изображает закрытый раздел auth: по умолчанию пускает всех.
type fakeAccess struct {
	guest bool // Require уводит на вход
	csrf  bool // CSRFOK отвергает подпись
	login string
}

func (a fakeAccess) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.guest {
			http.Redirect(w, r, "/studio?auth=login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a fakeAccess) Login(*http.Request) string { return a.login }

func (a fakeAccess) CSRFToken(http.ResponseWriter, *http.Request) string { return "test-csrf" }

func (a fakeAccess) CSRFOK(*http.Request) bool { return a.csrf }

// fakeThrottle пропускает всё: счёт попыток проверяют тесты webutil.
type fakeThrottle struct{}

func (fakeThrottle) Middleware(_ string, _, next http.Handler) http.Handler { return next }

func testServer(t *testing.T) *testEnv {
	t.Helper()
	return testServerWith(t, fakeAccess{csrf: true, login: "tester"})
}

func testServerWith(t *testing.T, access album.Access) *testEnv {
	t.Helper()

	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	log, err := logging.New(logging.Config{Output: io.Discard})
	if err != nil {
		t.Fatal(err)
	}

	photosDir := filepath.Join(dir, "photos")
	srv, err := album.New(album.Config{
		Store:     st,
		PhotosDir: photosDir,
		Access:    access,
		Throttle:  fakeThrottle{},
		Log:       log,
	})
	if err != nil {
		t.Fatalf("album.New: %v", err)
	}

	mux := http.NewServeMux()
	srv.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &testEnv{ts: ts, st: st, photosDir: photosDir}
}

// noRedirect клиент, который не ходит по редиректам: статус 303 проверяется как есть.
var noRedirect = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// get запрашивает адрес и отдаёт ответ с прочитанным телом.
func get(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()

	res, err := noRedirect.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return res, body
}
