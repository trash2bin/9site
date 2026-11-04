package auth_test

import (
	"html"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/auth"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

func actionPage(t *testing.T, env *testEnv, client *http.Client, path string) (*http.Response, string) {
	t.Helper()

	res, err := client.Get(env.ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return res, string(body)
}

func TestActionsPageAccess(t *testing.T) {
	env := adminEnv(t)

	res, _ := actionPage(t, env, client(t), "/vault/actions")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("гость: %d, ожидался 303", res.StatusCode)
	}

	reader := client(t)
	if code, page := register(t, reader, env.ts.URL, "reader", "reader-pass1", "", "reader@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("регистрация: %d\n%s", code, page)
	}
	res, _ = actionPage(t, env, reader, "/vault/actions")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("обычный юзер: %d, ожидался 403", res.StatusCode)
	}

	admin := adminClient(t, env)
	entries, err := env.st.ListActions(t.Context(), store.ActionFilter{}, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundLogin := false
	for _, entry := range entries {
		if entry.Login == "owner" && entry.Event == auth.EventLoginSucceeded.Name() && entry.Result == logging.ResultSuccess {
			foundLogin = true
		}
	}
	if !foundLogin {
		t.Fatalf("вход админа не попал в журнал: %+v", entries)
	}

	res, page := actionPage(t, env, admin, "/vault/actions")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("админ: %d\n%s", res.StatusCode, page)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control: %q", res.Header.Get("Cache-Control"))
	}
	for _, want := range []string{"ACTION JOURNAL", "owner", auth.EventLoginSucceeded.Name()} {
		if !strings.Contains(page, want) {
			t.Fatalf("на странице нет %q:\n%s", want, page)
		}
	}
}

func TestActionsPageFiltersPagingAndEscaping(t *testing.T) {
	env := adminEnv(t)
	ctx := t.Context()

	for i := range 51 {
		if err := env.st.Record(ctx, store.Action{
			Login: "pager", Event: "test.paged", Target: string(rune('a' + i%26)),
			Result: logging.ResultSuccess, IP: "192.0.2.1",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.st.Record(ctx, store.Action{
		Login: "<b>bad</b>", Event: "test.escaped", Target: "<script>alert(1)</script>",
		Result: logging.ResultSuccess, IP: "<i>ip</i>",
	}); err != nil {
		t.Fatal(err)
	}

	admin := adminClient(t, env)
	_, first := actionPage(t, env, admin, "/vault/actions?event=test.paged")
	if !strings.Contains(first, "page 1 of 2") {
		t.Fatalf("нет первой страницы:\n%s", first)
	}
	_, second := actionPage(t, env, admin, "/vault/actions?event=test.paged&page=2")
	if !strings.Contains(second, "page 2 of 2") {
		t.Fatalf("нет второй страницы:\n%s", second)
	}

	_, last := actionPage(t, env, admin, "/vault/actions?event=test.paged&page=999")
	if !strings.Contains(last, "page 2 of 2") {
		t.Fatalf("номер страницы не обрезан:\n%s", last)
	}

	_, escaped := actionPage(t, env, admin, "/vault/actions?event=test.escaped")
	if strings.Contains(escaped, "<script>alert(1)</script>") || strings.Contains(escaped, "<b>bad</b>") {
		t.Fatalf("HTML не экранирован:\n%s", escaped)
	}
	if !strings.Contains(escaped, html.EscapeString("<script>alert(1)</script>")) ||
		!strings.Contains(escaped, html.EscapeString("<b>bad</b>")) {
		t.Fatalf("экранированных значений нет:\n%s", escaped)
	}
}
