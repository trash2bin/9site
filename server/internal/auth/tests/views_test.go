// Тесты страницы /vault/views: доступ, фильтры, топ, пейджинг, экранирование.
package auth_test

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/auth"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

func TestViewsPageAccess(t *testing.T) {
	env := adminEnv(t)

	res, _ := actionPage(t, env, client(t), "/vault/views")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("гость: %d, ожидался 303", res.StatusCode)
	}

	reader := client(t)
	if code, page := register(t, reader, env.ts.URL, "reader", "reader-pass1", "", "reader@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("регистрация: %d\n%s", code, page)
	}
	res, _ = actionPage(t, env, reader, "/vault/views")
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("обычный юзер: %d, ожидался 403", res.StatusCode)
	}

	admin := adminClient(t, env)
	res, page := actionPage(t, env, admin, "/vault/views")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("админ: %d\n%s", res.StatusCode, page)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control: %q", res.Header.Get("Cache-Control"))
	}
	if !strings.Contains(page, "PAGE VIEWS") {
		t.Fatalf("на странице нет заголовка:\n%s", page)
	}
}

func TestViewsPageFiltersAndTop(t *testing.T) {
	env := adminEnv(t)
	ctx := t.Context()

	for _, v := range []store.View{
		{Login: "reader", Path: "/studio", IP: "192.0.2.1", UA: "Firefox", Referer: "https://example.com/"},
		{Login: "reader", Path: "/studio", IP: "192.0.2.1", UA: "Firefox"},
		{Path: "/", IP: "198.51.100.7", UA: "curl"},
	} {
		if err := env.st.RecordView(ctx, v); err != nil {
			t.Fatalf("RecordView: %v", err)
		}
	}

	admin := adminClient(t, env)

	_, all := actionPage(t, env, admin, "/vault/views")
	for _, want := range []string{"/studio", "reader", "198.51.100.7", "популярные страницы"} {
		if !strings.Contains(all, want) {
			t.Fatalf("во всём трафике нет %q:\n%s", want, all)
		}
	}
	// Топ: /studio встречался дважды.
	if !strings.Contains(all, "<td>2</td>") {
		t.Fatalf("в топе нет счётчика 2:\n%s", all)
	}

	_, users := actionPage(t, env, admin, "/vault/views?audience=users")
	if !strings.Contains(users, "<td>reader</td>") {
		t.Fatalf("срез вошедших без reader:\n%s", users)
	}
	if strings.Contains(users, "<td>/</td>") {
		t.Fatalf("анонимный просмотр / попал в срез вошедших:\n%s", users)
	}

	_, anon := actionPage(t, env, admin, "/vault/views?audience=anonymous")
	if strings.Contains(anon, "<td>reader</td>") {
		t.Fatalf("вошедший попал в срез анонимов:\n%s", anon)
	}
	if !strings.Contains(anon, "<td>-</td>") {
		t.Fatalf("в срезе анонимов нет прочерка:\n%s", anon)
	}

	// Пресеты и мусор: сегодня записи есть, мусорный период и аудитория = всё.
	_, today := actionPage(t, env, admin, "/vault/views?period=today")
	if !strings.Contains(today, "/studio") {
		t.Fatalf("за сегодня ничего нет:\n%s", today)
	}
	_, junk := actionPage(t, env, admin, "/vault/views?period=zzz&audience=zzz&path=stud&login=reader")
	if !strings.Contains(junk, "/studio") {
		t.Fatalf("мусорные параметры должны сбрасываться в «всё»:\n%s", junk)
	}

	_, empty := actionPage(t, env, admin, "/vault/views?login=никого")
	if !strings.Contains(empty, "ничего не найдено") {
		t.Fatalf("пустая выборка не показана:\n%s", empty)
	}
}

func TestViewsPagePagingAndEscaping(t *testing.T) {
	env := adminEnv(t)
	ctx := t.Context()

	for range 51 {
		if err := env.st.RecordView(ctx, store.View{Login: "pager", Path: "/paged"}); err != nil {
			t.Fatalf("RecordView: %v", err)
		}
	}
	if err := env.st.RecordView(ctx, store.View{
		Login: "<b>bad</b>", Path: "/<script>alert(1)</script>", IP: "<i>ip</i>",
	}); err != nil {
		t.Fatalf("RecordView: %v", err)
	}

	admin := adminClient(t, env)

	_, first := actionPage(t, env, admin, "/vault/views?login=pager")
	if !strings.Contains(first, "страница 1 из 2") {
		t.Fatalf("нет первой страницы:\n%s", first)
	}
	_, second := actionPage(t, env, admin, "/vault/views?login=pager&page=2")
	if !strings.Contains(second, "страница 2 из 2") {
		t.Fatalf("нет второй страницы:\n%s", second)
	}
	_, clamped := actionPage(t, env, admin, "/vault/views?login=pager&page=999")
	if !strings.Contains(clamped, "страница 2 из 2") {
		t.Fatalf("номер страницы не обрезан:\n%s", clamped)
	}

	_, escaped := actionPage(t, env, admin, "/vault/views")
	if strings.Contains(escaped, "<script>alert(1)</script>") || strings.Contains(escaped, "<b>bad</b>") {
		t.Fatalf("HTML не экранирован:\n%s", escaped)
	}
	if !strings.Contains(escaped, html.EscapeString("<script>alert(1)</script>")) ||
		!strings.Contains(escaped, html.EscapeString("<b>bad</b>")) {
		t.Fatalf("экранированных значений нет:\n%s", escaped)
	}
}

// TestUsersPageShowsActivity: колонка активности берёт мету последней сессии.
func TestUsersPageShowsActivity(t *testing.T) {
	env := adminEnv(t)
	admin := adminClient(t, env)

	res, page := actionPage(t, env, admin, "/vault/users")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("users: %d\n%s", res.StatusCode, page)
	}
	if !strings.Contains(page, "нет сессий") {
		t.Fatalf("до активности колонка пустой не должна быть:\n%s", page)
	}

	if err := env.st.TouchSession(t.Context(), sessionHash(t, admin, env.ts.URL), "192.0.2.1", "TestBrowser/1.0"); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	_, page = actionPage(t, env, admin, "/vault/users")
	for _, want := range []string{"192.0.2.1", "TestBrowser/1.0", "UTC"} {
		if !strings.Contains(page, want) {
			t.Fatalf("в колонке активности нет %q:\n%s", want, page)
		}
	}
}

// TestViewsRequiresViewsStorage: без хранилища Views сервер не поднимается.
func TestViewsRequiresViewsStorage(t *testing.T) {
	err := newAuth(t, func(cfg *auth.Config) { cfg.Views = nil })
	if err == nil {
		t.Fatal("ожидалась ошибка без Views")
	}
}
