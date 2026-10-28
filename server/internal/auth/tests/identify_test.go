// Тесты Identify: логин на обычных HTML-страницах и мета сессии.
package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// pageGet запрос страницы с Accept браузера: Identify пропускает всё остальное.
func pageGet(t *testing.T, c *http.Client, base, path string) string {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("User-Agent", "TestBrowser/1.0")

	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	page, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d\n%s", path, res.StatusCode, page)
	}
	return string(page)
}

// sessionHash хэш токена sid из cookie-банки: в базе лежит только хэш.
func sessionHash(t *testing.T, c *http.Client, base string) string {
	t.Helper()

	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == "sid" && ck.Value != "" {
			sum := sha256.Sum256([]byte(ck.Value))
			return hex.EncodeToString(sum[:])
		}
	}
	t.Fatal("нет cookie sid")
	return ""
}

// TestIdentifyResolvesLoginAndTouchesSession: на обычной странице вошедшего
// виден логин, сессии штампуется мета; гость и запрос без text/html
// проходят без изменений.
func TestIdentifyResolvesLoginAndTouchesSession(t *testing.T) {
	env := testServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /page", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "login=%s", env.srv.Login(r))
	})
	ts := httptest.NewServer(env.srv.Identify(mux))
	t.Cleanup(ts.Close)

	c := client(t)

	if page := pageGet(t, c, ts.URL, "/page"); page != "login=" {
		t.Fatalf("гость получил %q, ожидался пустой логин", page)
	}

	if code, body := register(t, c, env.ts.URL, "ivan", "secret123", "Иван", "ivan@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("регистрация: %d\n%s", code, body)
	}
	if code := loginForm(t, c, env.ts.URL, "ivan", "secret123"); code != http.StatusSeeOther {
		t.Fatalf("вход: %d", code)
	}

	if page := pageGet(t, c, ts.URL, "/page"); page != "login=ivan" {
		t.Fatalf("вошедший получил %q, ожидался login=ivan", page)
	}

	sess, err := env.st.SessionByTokenHash(context.Background(), sessionHash(t, c, ts.URL))
	if err != nil {
		t.Fatalf("SessionByTokenHash: %v", err)
	}
	if sess.IP != "127.0.0.1" || sess.UA != "TestBrowser/1.0" || sess.LastSeen == "" {
		t.Fatalf("мета сессии не проставилась: %+v", sess)
	}

	// Без text/html Identify не трогает запрос: ни контекста, ни меты.
	req, err := http.NewRequest(http.MethodGet, ts.URL+"/page", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(page) != "login=" {
		t.Fatalf("запрос без Accept: %q, логин не должен попадать в контекст", page)
	}
}
