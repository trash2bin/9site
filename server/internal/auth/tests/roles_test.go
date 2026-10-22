// Тесты ролей: админ из окружения, RequireAdmin и страница прав.
package auth_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/auth"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

const ownerPassword = "owner-secret-1"

// adminEnv сервер с секретным админом из окружения, как в main.
func adminEnv(t *testing.T) *testEnv {
	t.Helper()
	return startEnv(t, func(cfg *auth.Config, _ *store.Store) {
		cfg.AdminLogin = "owner"
		cfg.AdminPassword = ownerPassword
	})
}

// adminClient клиент, вошедший админом из окружения.
func adminClient(t *testing.T, env *testEnv) *http.Client {
	t.Helper()

	c := client(t)
	if code := loginForm(t, c, env.ts.URL, "owner", ownerPassword); code != http.StatusSeeOther {
		t.Fatalf("вход админа: %d", code)
	}
	return c
}

// newAuth поднимает только auth.New, без HTTP: проверяем отказ на старте.
func newAuth(t *testing.T, cfg func(*auth.Config)) error {
	t.Helper()

	dir := t.TempDir()
	st := openTestStore(t, dir)
	log, _ := testLogger(t)

	c := auth.Config{
		Users:      st,
		Sessions:   st,
		AvatarsDir: filepath.Join(dir, "avatars"),
		OutboxDir:  filepath.Join(dir, "outbox"),
		Throttle:   testThrottle(t, 10),
		Log:        log,
	}
	cfg(&c)

	_, err := auth.New(c)
	return err
}

// TestBootstrapAdmin: логин и пароль из окружения дают рабочего админа.
func TestBootstrapAdmin(t *testing.T) {
	env := adminEnv(t)

	owner, err := env.st.UserByLogin(t.Context(), "owner")
	if err != nil {
		t.Fatalf("админ не создан: %v", err)
	}
	if !owner.IsAdmin() {
		t.Fatalf("роль %q, ожидалась admin", owner.Role)
	}

	c := adminClient(t, env)
	res, err := c.Get(env.ts.URL + "/vault")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("vault админа: %d\n%s", res.StatusCode, page)
	}
	if !strings.Contains(string(page), "роль: admin") {
		t.Fatalf("на странице нет роли:\n%s", page)
	}
}

// TestBootstrapAdminRejectsBadConfig: половина пары, короткий пароль и
// плохой логин роняют старт, а не тихо игнорируются.
func TestBootstrapAdminRejectsBadConfig(t *testing.T) {
	cases := map[string]func(*auth.Config){
		"логин без пароля":  func(c *auth.Config) { c.AdminLogin = "owner" },
		"пароль без логина": func(c *auth.Config) { c.AdminPassword = ownerPassword },
		"короткий пароль":   func(c *auth.Config) { c.AdminLogin, c.AdminPassword = "owner", "123" },
		"логин не по форме": func(c *auth.Config) { c.AdminLogin, c.AdminPassword = "владелец", ownerPassword },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if err := newAuth(t, mutate); err == nil {
				t.Fatal("New вернул nil, ожидалась ошибка")
			}
		})
	}

	// Пустые оба поля: админа из окружения просто нет, старт проходит.
	if err := newAuth(t, func(*auth.Config) {}); err != nil {
		t.Fatalf("без env: %v", err)
	}
}

// TestRequireAdmin: гость уводится на вход, обычный юзер получает 403,
// админ проходит.
func TestRequireAdmin(t *testing.T) {
	env := adminEnv(t)

	mux := http.NewServeMux()
	mux.Handle("GET /admin-only", env.srv.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("admin ok"))
	})))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	guest := client(t)
	if res, err := guest.Get(ts.URL + "/admin-only"); err != nil {
		t.Fatal(err)
	} else {
		_ = res.Body.Close()
		if res.StatusCode != http.StatusSeeOther {
			t.Fatalf("гость: %d, ожидался 303", res.StatusCode)
		}
	}

	reader := client(t)
	if code, page := register(t, reader, env.ts.URL, "reader", "reader-pass1", "", "reader@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("регистрация: %d\n%s", code, page)
	}
	res, err := reader.Get(ts.URL + "/admin-only")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("обычный юзер: %d, ожидался 403", res.StatusCode)
	}

	res, err = adminClient(t, env).Get(ts.URL + "/admin-only")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "admin ok") {
		t.Fatalf("админ: %d\n%s", res.StatusCode, body)
	}
}

// pageCSRF подпись формы со страницы прав.
func pageCSRF(t *testing.T, c *http.Client, url string) string {
	t.Helper()

	res, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`name="csrf" value="([^"]*)"`).FindSubmatch(page)
	if match == nil {
		t.Fatalf("на странице нет подписи:\n%s", page)
	}
	return string(match[1])
}

// setRole отправляет форму смены роли и отдаёт статус с телом.
func setRole(t *testing.T, c *http.Client, base, csrf, login, role string) (int, string) {
	t.Helper()

	res, err := c.PostForm(base+"/vault/users/role", map[string][]string{
		"csrf": {csrf}, "login": {login}, "role": {role},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body)
}

// TestUsersPageRoles: админ видит список и меняет роли, последнего админа
// снять нельзя, обычный юзер на страницу не попадает.
func TestUsersPageRoles(t *testing.T) {
	env := adminEnv(t)

	reader := client(t)
	if code, page := register(t, reader, env.ts.URL, "reader", "reader-pass1", "Читатель", "reader@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("регистрация: %d\n%s", code, page)
	}

	if res, err := reader.Get(env.ts.URL + "/vault/users"); err != nil {
		t.Fatal(err)
	} else {
		_ = res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("обычный юзер на странице прав: %d, ожидался 403", res.StatusCode)
		}
	}

	admin := adminClient(t, env)
	res, err := admin.Get(env.ts.URL + "/vault/users")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	text := string(page)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("страница прав: %d\n%s", res.StatusCode, text)
	}
	for _, want := range []string{"owner", "reader", "reader@example.com", "сделать админом", "снять админа"} {
		if !strings.Contains(text, want) {
			t.Fatalf("на странице нет %q:\n%s", want, text)
		}
	}

	csrf := pageCSRF(t, admin, env.ts.URL+"/vault/users")

	// Единственного админа снять нельзя.
	if code, body := setRole(t, admin, env.ts.URL, csrf, "owner", store.RoleUser); code != http.StatusOK ||
		!strings.Contains(body, "последнего админа снять нельзя") {
		t.Fatalf("снятие последнего админа: %d\n%s", code, body)
	}

	// Обычного юзера повышаем: теперь админов двое.
	if code, body := setRole(t, admin, env.ts.URL, csrf, "reader", store.RoleAdmin); code != http.StatusSeeOther {
		t.Fatalf("повышение: %d\n%s", code, body)
	}
	if user, err := env.st.UserByLogin(t.Context(), "reader"); err != nil || !user.IsAdmin() {
		t.Fatalf("роль reader после повышения: %+v, %v", user, err)
	}

	// Неизвестный логин, непонятная роль и битая подпись: страница с ошибкой.
	for _, tc := range []struct{ login, role, csrf, want string }{
		{"nobody", store.RoleAdmin, csrf, "такого логина нет"},
		{"reader", "root", csrf, "непонятная роль"},
		{"reader", store.RoleUser, "чужая-подпись", "форма устарела"},
	} {
		code, body := setRole(t, admin, env.ts.URL, tc.csrf, tc.login, tc.role)
		if code != http.StatusOK || !strings.Contains(body, tc.want) {
			t.Fatalf("%s/%s: %d\n%s", tc.login, tc.role, code, body)
		}
	}

	// Снимаем админа с себя: роль читается из базы на каждом запросе,
	// поэтому дальше тот же клиент уже не админ.
	if code, body := setRole(t, admin, env.ts.URL, csrf, "owner", store.RoleUser); code != http.StatusSeeOther {
		t.Fatalf("снятие с себя: %d\n%s", code, body)
	}
	if res, err := admin.Get(env.ts.URL + "/vault/users"); err != nil {
		t.Fatal(err)
	} else {
		_ = res.Body.Close()
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("бывший админ на странице прав: %d, ожидался 403", res.StatusCode)
		}
	}

	// Reader остался единственным админом: снять роль с него нельзя.
	readerCSRF := pageCSRF(t, reader, env.ts.URL+"/vault/users")
	if code, body := setRole(t, reader, env.ts.URL, readerCSRF, "reader", store.RoleUser); code != http.StatusOK ||
		!strings.Contains(body, "последнего админа снять нельзя") {
		t.Fatalf("снятие единственного админа: %d\n%s", code, body)
	}
}

// TestVaultLinksByRole: ссылка на альбом есть у всех вошедших, на права
// только у админа.
func TestVaultLinksByRole(t *testing.T) {
	env := adminEnv(t)

	reader := client(t)
	if code, page := register(t, reader, env.ts.URL, "reader", "reader-pass1", "", "reader@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("регистрация: %d\n%s", code, page)
	}

	links := func(c *http.Client) string {
		res, err := c.Get(env.ts.URL + "/vault")
		if err != nil {
			t.Fatal(err)
		}
		page, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("vault: %d\n%s", res.StatusCode, page)
		}
		return string(page)
	}

	if page := links(reader); !strings.Contains(page, "/vault/album") || strings.Contains(page, "/vault/users") {
		t.Fatalf("vault обычного юзера:\n%s", page)
	}
	if page := links(adminClient(t, env)); !strings.Contains(page, "/vault/users") {
		t.Fatalf("vault админа без ссылки на права:\n%s", page)
	}
}

// pageBody запрашивает страницу прав и отдаёт тело.
func pageBody(t *testing.T, c *http.Client, url string) string {
	t.Helper()

	res, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%s: статус %d\n%s", url, res.StatusCode, page)
	}
	return string(page)
}

// TestUsersPaging: список идёт страницами по 50, счётчик честный, за
// пределами списка показывается последняя страница, а не пустота.
func TestUsersPaging(t *testing.T) {
	env := adminEnv(t)
	ctx := t.Context()

	for i := range 51 {
		login := fmt.Sprintf("user%02d", i)
		if _, err := env.st.CreateUser(ctx, login, "hash", "", login+"@example.com"); err != nil {
			t.Fatalf("CreateUser %s: %v", login, err)
		}
	}
	admin := adminClient(t, env)

	first := pageBody(t, admin, env.ts.URL+"/vault/users")
	for _, want := range []string{"страница 1 из 2", "всего аккаунтов: 52", `href="/vault/users?page=2"`, ">user50<"} {
		if !strings.Contains(first, want) {
			t.Fatalf("первая страница без %q", want)
		}
	}
	if strings.Contains(first, ">user00<") {
		t.Fatal("самый старый аккаунт попал на первую страницу")
	}

	second := pageBody(t, admin, env.ts.URL+"/vault/users?page=2")
	for _, want := range []string{"страница 2 из 2", `href="/vault/users?page=1"`, ">user00<", ">owner<"} {
		if !strings.Contains(second, want) {
			t.Fatalf("вторая страница без %q", want)
		}
	}

	// За пределами и мусор в параметре: последняя и первая страницы.
	if page := pageBody(t, admin, env.ts.URL+"/vault/users?page=99"); !strings.Contains(page, "страница 2 из 2") {
		t.Fatal("page=99 не подрезался до последней страницы")
	}
	if page := pageBody(t, admin, env.ts.URL+"/vault/users?page=abc"); !strings.Contains(page, "страница 1 из 2") {
		t.Fatal("мусор в page не свёлся к первой странице")
	}

	// Смена роли со второй страницы возвращает туда же.
	csrf := pageCSRF(t, admin, env.ts.URL+"/vault/users?page=2")
	res, err := admin.PostForm(env.ts.URL+"/vault/users/role", map[string][]string{
		"csrf": {csrf}, "login": {"user00"}, "role": {store.RoleAdmin}, "page": {"2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/vault/users?ok=role&page=2" {
		t.Fatalf("редирект после смены роли: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
}
