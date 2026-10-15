// Тесты белого списка статики: наружу уходят только файлы сайта, не
// каталоги и не чужие файлы, даже если они лежат в корне статики.
package home_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/home"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
)

// staticServer сервер витрины с чужими файлами в корне статики: без
// белого списка FileServer отдал бы их наружу.
func staticServer(t *testing.T) *httptest.Server {
	t.Helper()

	root := writeStatic(t)
	for _, name := range []string{"main.go", "site.db", "visits.dat"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("секрет "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	log, err := logging.New(logging.Config{Output: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	st := openStore(t)
	t.Cleanup(func() { _ = st.Close() })
	srv, err := home.New(home.Config{StaticRoot: root, Log: log, Store: st})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	srv.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestStaticOnly(t *testing.T) {
	ts := staticServer(t)

	cases := []struct {
		path string
		code int
	}{
		// Разрешённая статика.
		{"/style.css", http.StatusOK},
		{"/design.css", http.StatusOK},
		{"/catalog.css", http.StatusOK},
		{"/print.css", http.StatusOK},
		{"/css/base.css", http.StatusOK},
		{"/css/auth/auth.css", http.StatusOK},
		{"/js/main.js", http.StatusOK},
		{"/assets/img/logo.png", http.StatusOK},

		// Чужие файлы и данные.
		{"/main.go", http.StatusNotFound},
		{"/site.db", http.StatusNotFound},
		{"/visits.dat", http.StatusNotFound},
		{"/var/site.db", http.StatusNotFound},
		{"/../etc/passwd", http.StatusNotFound},
		{"/assets/../main.go", http.StatusNotFound},
		{"/assets/%2e%2e/main.go", http.StatusNotFound},

		// Оглавления каталогов не отдаются, с косой чертой и без неё.
		{"/css/auth/", http.StatusNotFound},
		{"/assets/img/", http.StatusNotFound},
		{"/js/", http.StatusNotFound},
		{"/css/auth", http.StatusNotFound},
		{"/assets/img", http.StatusNotFound},
	}

	for _, c := range cases {
		// Клиент следует редиректам ServeMux: и они должны кончиться 404.
		res, err := http.Get(ts.URL + c.path)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != c.code {
			t.Errorf("%s: статус %d, ждали %d", c.path, res.StatusCode, c.code)
		}
	}
}
