// Тесты шва для других срезов: Require, Login и проверка подписи формы.
package auth_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// secured поднимает отдельный сервер с двумя закрытыми маршрутами: их
// оборачивает Require, как это делает срез альбома.
func secured(t *testing.T, env *testEnv) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle("GET /guarded", env.srv.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "login=%s csrf=%s", env.srv.Login(r), env.srv.CSRFToken(w, r))
	})))
	mux.Handle("POST /guarded", env.srv.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !env.srv.CSRFOK(r) {
			http.Error(w, "форма устарела", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// TestRequireRedirectsGuest: без сессии закрытая страница уводит на вход,
// а не отдаёт 403 с пустым телом.
func TestRequireRedirectsGuest(t *testing.T) {
	env := testServer(t)
	ts := secured(t, env)
	guest := client(t)

	res, err := guest.Get(ts.URL + "/guarded")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("гость получил %d, ожидался 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/studio?auth=login" {
		t.Fatalf("редирект на %q", loc)
	}
}

// TestRequirePassesLoginAndCSRF: авторизованного пускает, логин виден
// обработчику, подпись формы из сессии принимается, чужая отвергается.
func TestRequirePassesLoginAndCSRF(t *testing.T) {
	env := testServer(t)
	ts := secured(t, env)
	c := client(t)

	if code, page := register(t, c, env.ts.URL, "ivan", "secret123", "Иван", "ivan@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("регистрация: %d\n%s", code, page)
	}
	if code := loginForm(t, c, env.ts.URL, "ivan", "secret123"); code != http.StatusSeeOther {
		t.Fatalf("вход: %d", code)
	}

	res, err := c.Get(ts.URL + "/guarded")
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("авторизованный получил %d\n%s", res.StatusCode, page)
	}

	match := regexp.MustCompile(`login=(\S*) csrf=(\S*)`).FindSubmatch(page)
	if match == nil {
		t.Fatalf("ответ без логина и подписи:\n%s", page)
	}
	login, csrf := string(match[1]), string(match[2])
	if login != "ivan" {
		t.Fatalf("логин %q, ожидался ivan", login)
	}

	post := func(token string) int {
		res, err := c.PostForm(ts.URL+"/guarded", map[string][]string{"csrf": {token}})
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	if code := post("чужая-подпись"); code != http.StatusForbidden {
		t.Fatalf("чужая подпись: %d, ожидался 403", code)
	}
	if code := post(csrf); code != http.StatusNoContent {
		t.Fatalf("своя подпись: %d, ожидался 204", code)
	}
}
