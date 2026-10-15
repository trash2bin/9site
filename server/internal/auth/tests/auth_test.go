// Тесты закрытого раздела: регистрация, вход, закрытая страница, выход,
// смена данных, сброс пароля письмом, SQL-инъекции и XSS.
package auth_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/auth"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// testEnv поднятый сервер авторизации с базой и почтовым ящиком.
type testEnv struct {
	ts     *httptest.Server
	st     *store.Store
	outbox string
}

func testServer(t *testing.T) *testEnv {
	t.Helper()
	return startEnv(t, nil)
}

// testServerBudget тот же сервер с лимитом limit на каждый бюджет модуля.
func testServerBudget(t *testing.T, limit int) *testEnv {
	t.Helper()
	return startEnv(t, func(cfg *auth.Config, _ *store.Store) {
		cfg.Throttle = testThrottle(t, limit)
	})
}

// startEnv поднимает сервер авторизации; extra правит конфиг перед New.
func startEnv(t *testing.T, extra func(*auth.Config, *store.Store)) *testEnv {
	t.Helper()

	dir := t.TempDir()
	outbox := filepath.Join(dir, "outbox")
	st := openTestStore(t, dir)

	log, _ := testLogger(t)

	cfg := auth.Config{
		Users:      st,
		Sessions:   st,
		AvatarsDir: filepath.Join(dir, "avatars"),
		OutboxDir:  outbox,
		Throttle:   testThrottle(t, 1000),
		Log:        log,
	}
	if extra != nil {
		extra(&cfg, st)
	}

	// Ссылки в письмах собираются по BaseURL до New, поэтому слушатель
	// поднимаем заранее и адрес берём у него.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://" + ln.Addr().String()
	}

	srv, err := auth.New(cfg)
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	srv.Register(mux)
	ts := httptest.NewUnstartedServer(mux)
	ts.Listener = ln
	ts.Start()
	t.Cleanup(ts.Close)

	return &testEnv{ts: ts, st: st, outbox: outbox}
}

func openTestStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// testThrottle реестр лимитов с порогом limit: тот же *webutil.Limits, что вешает main.
func testThrottle(t *testing.T, limit int) auth.Throttle {
	t.Helper()

	log, _ := testLogger(t)
	limits, err := webutil.NewLimits(map[string]webutil.Budget{
		auth.BudgetLogin:    {Limit: limit, Window: time.Minute},
		auth.BudgetRegister: {Limit: limit, Window: time.Minute},
		auth.BudgetForgot:   {Limit: limit, Window: time.Minute},
	}, log)
	if err != nil {
		t.Fatal(err)
	}
	return limits
}

// testLogger логгер теста: строки пишутся в буфер, почтовые тесты их читают.
func testLogger(t *testing.T) (*logging.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	log, err := logging.New(logging.Config{Output: buf})
	if err != nil {
		t.Fatal(err)
	}
	return log, buf
}

// client браузерный клиент с cookie-банкой; за редиректами не следуем.
func client(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 10 * time.Second,
	}
}

// formCSRF достаёт подпись форм из скрытого поля страницы.
func formCSRF(t *testing.T, c *http.Client, base string) string {
	t.Helper()

	res, err := c.Get(base + "/studio?auth=login")
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
		t.Fatalf("на странице нет CSRF-поля:\n%s", page)
	}
	return string(match[1])
}

func register(t *testing.T, c *http.Client, base, login, password, fullName, email string, avatar io.Reader) (int, []byte) {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	fields := map[string]string{
		"login":     login,
		"password":  password,
		"full_name": fullName,
		"email":     email,
		"csrf":      formCSRF(t, c, base),
	}
	for name, value := range fields {
		if err := w.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if avatar != nil {
		part, err := w.CreateFormFile("avatar", "pic.gif")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(part, avatar); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", base+"/auth/register", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	page, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, page
}

func loginForm(t *testing.T, c *http.Client, base, login, password string) int {
	t.Helper()

	res, err := c.PostForm(base+"/auth/login", url.Values{
		"login":    {login},
		"password": {password},
		"csrf":     {formCSRF(t, c, base)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return res.StatusCode
}

func logout(t *testing.T, c *http.Client, base string) {
	t.Helper()

	res, err := c.PostForm(base+"/auth/logout", url.Values{"csrf": {formCSRF(t, c, base)}})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
}

// TestFullFlow закрытый раздел: гость не попадает, после регистрации попадает,
// после выхода снова не попадает.
func TestFullFlow(t *testing.T) {
	env := testServer(t)
	c := client(t)

	res, err := c.Get(env.ts.URL + "/studio")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /studio без авторизации: статус %d, ждали 200", res.StatusCode)
	}
	if bytes.Contains(page, []byte("черновики альбома")) {
		t.Fatal("гость увидел содержимое vault на открытой витрине")
	}

	res, err = c.Get(env.ts.URL + "/vault")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("GET /vault без авторизации: статус %d, ждали 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); loc != "/studio?auth=login" {
		t.Fatalf("гостя отправили на %q, ждали /studio?auth=login", loc)
	}

	if code, _ := register(t, c, env.ts.URL, "tester", "secret-pass-1", "Иван Тестов", "tester@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("регистрация: статус %d, ждали 303", code)
	}

	res, err = c.Get(env.ts.URL + "/vault")
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !bytes.Contains(page, []byte("секретный раздел")) {
		t.Fatalf("авторизованный не увидел закрытый раздел:\n%s", page)
	}

	logout(t, c, env.ts.URL)
	res, err = c.Get(env.ts.URL + "/vault")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("после выхода /vault отдал %d, ждали 303", res.StatusCode)
	}

	if code := loginForm(t, c, env.ts.URL, "tester", "secret-pass-1"); code != http.StatusSeeOther {
		t.Fatalf("повторный вход: статус %d, ждали 303", code)
	}
}

// TestSessionSurvivesRestart те же cookie в новом клиенте: сессия живёт на сервере,
// а не в памяти процесса.
func TestSessionSurvivesRestart(t *testing.T) {
	env := testServer(t)
	c := client(t)

	register(t, c, env.ts.URL, "durable", "secret-pass-1", "", "durable@example.com", nil)

	fresh := &http.Client{Jar: c.Jar, CheckRedirect: c.CheckRedirect, Timeout: 10 * time.Second}
	res, err := fresh.Get(env.ts.URL + "/vault")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("во втором сеансе /vault отдал %d, ждали 200", res.StatusCode)
	}
}

// TestSQLInjection инъекция через форму входа не обходит авторизацию
// и не роняет сервер.
func TestSQLInjection(t *testing.T) {
	env := testServer(t)
	c := client(t)

	register(t, c, env.ts.URL, "victim", "secret-pass-1", "", "victim@example.com", nil)

	for _, payload := range []string{
		"victim' OR '1'='1",
		"victim' --",
		"' OR 1=1 --",
		`victim"; DROP TABLE users; --`,
	} {
		if code := loginForm(t, client(t), env.ts.URL, payload, "anything"); code == http.StatusSeeOther {
			t.Fatalf("инъекция %q прошла авторизацию", payload)
		}
	}

	if code := loginForm(t, client(t), env.ts.URL, "victim", "secret-pass-1"); code != http.StatusSeeOther {
		t.Fatal("настоящий пользователь после инъекций не может войти")
	}
}

func TestXSSEscape(t *testing.T) {
	env := testServer(t)
	c := client(t)

	malicious := `<script>alert("xss")</script>`
	register(t, c, env.ts.URL, "xssfan", "secret-pass-1", malicious, "xss@example.com", nil)

	res, err := c.Get(env.ts.URL + "/studio")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if bytes.Contains(page, []byte(malicious)) {
		t.Fatal("вредоносная разметка попала в ответ без экранирования")
	}
	if !bytes.Contains(page, []byte("&lt;script&gt;")) {
		t.Fatalf("ожидали экранированное &lt;script&gt; в ответе:\n%s", page)
	}
}

// TestPasswordReset письмо со ссылкой лежит в outbox; ссылка меняет
// пароль и работает только один раз.
func TestPasswordReset(t *testing.T) {
	env := testServer(t)
	c := client(t)

	register(t, c, env.ts.URL, "forgetful", "old-pass-123", "", "forgetful@example.com", nil)
	logout(t, c, env.ts.URL)

	res, err := c.PostForm(env.ts.URL+"/auth/forgot", url.Values{
		"email": {"forgetful@example.com"},
		"csrf":  {formCSRF(t, c, env.ts.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	matches, _ := filepath.Glob(filepath.Join(env.outbox, "*.eml"))
	if len(matches) != 1 {
		t.Fatalf("в почтовом ящике %d писем, ждали 1", len(matches))
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	link := regexp.MustCompile(regexp.QuoteMeta(env.ts.URL) + `/reset\?token=[A-Za-z0-9_-]+`).Find(raw)
	if link == nil {
		t.Fatalf("в письме нет ссылки на сброс:\n%s", raw)
	}

	tokenValue := strings.TrimPrefix(string(link), env.ts.URL+"/reset?token=")
	res, err = c.PostForm(env.ts.URL+"/reset", url.Values{
		"token":    {tokenValue},
		"password": {"brand-new-pass-9"},
		"csrf":     {formCSRF(t, c, env.ts.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("сброс пароля: статус %d, ждали 303", res.StatusCode)
	}

	if code := loginForm(t, client(t), env.ts.URL, "forgetful", "old-pass-123"); code == http.StatusSeeOther {
		t.Fatal("старый пароль остался рабочим")
	}
	if code := loginForm(t, client(t), env.ts.URL, "forgetful", "brand-new-pass-9"); code != http.StatusSeeOther {
		t.Fatal("новый пароль не работает")
	}

	res, err = c.PostForm(env.ts.URL+"/reset", url.Values{
		"token":    {tokenValue},
		"password": {"another-pass-9"},
		"csrf":     {formCSRF(t, c, env.ts.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !bytes.Contains(page, []byte("недействительна")) {
		t.Fatalf("повторная ссылка сработала:\n%s", page)
	}
}

func TestProfileUpdate(t *testing.T) {
	env := testServer(t)
	c := client(t)

	register(t, c, env.ts.URL, "editor", "secret-pass-1", "Старое Имя", "old@example.com", nil)

	form := url.Values{
		"csrf":         {formCSRF(t, c, env.ts.URL)},
		"login":        {"hacker"}, // смена логина игнорируется
		"full_name":    {"Новое Имя"},
		"email":        {"new@example.com"},
		"cur_password": {"secret-pass-1"},
		"new_password": {"second-pass-123"},
	}
	res, err := c.PostForm(env.ts.URL+"/auth/profile", form)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if !bytes.Contains(page, []byte("Новое Имя")) {
		t.Fatalf("ФИО не обновилось:\n%s", page)
	}
	if bytes.Contains(page, []byte(`value="hacker"`)) {
		t.Fatal("логин удалось изменить")
	}

	logout(t, c, env.ts.URL)
	if code := loginForm(t, client(t), env.ts.URL, "editor", "secret-pass-1"); code == http.StatusSeeOther {
		t.Fatal("старый пароль остался рабочим")
	}
	if code := loginForm(t, client(t), env.ts.URL, "editor", "second-pass-123"); code != http.StatusSeeOther {
		t.Fatal("новый пароль не работает")
	}
}

// testGIF валидная картинка: тип аватара определяется по содержимому,
// а не по имени файла.
var testGIF = []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04" +
	"\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")

// TestAvatar картинка сохраняется и отдаётся; не-картинки отвергаются,
// иначе под видом аватара заливается stored XSS.
func TestAvatar(t *testing.T) {
	env := testServer(t)
	c := client(t)

	register(t, c, env.ts.URL, "shooter", "secret-pass-1", "", "shooter@example.com", bytes.NewReader(testGIF))

	user, err := env.st.UserByLogin(context.Background(), "shooter")
	if err != nil {
		t.Fatal(err)
	}
	if user.Avatar == "" {
		t.Fatal("аватар не сохранился")
	}

	res, err := c.Get(env.ts.URL + "/avatars/" + user.Avatar)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("аватар отдал %d, ждали 200", res.StatusCode)
	}

	_, page := register(t, c, env.ts.URL, "evil", "secret-pass-1", "", "evil@example.com",
		strings.NewReader("<html><script>alert(1)</script></html>"))
	if !bytes.Contains(page, []byte("изображением")) {
		t.Fatalf("не-картинка не отвергнута:\n%s", page)
	}
}

// TestRegisterBadAvatarDoesNotCreateUser ошибочный аватар не оставляет
// полузарегистрованный аккаунт, повтор с исправленным файлом проходит.
func TestRegisterBadAvatarDoesNotCreateUser(t *testing.T) {
	env := testServer(t)
	c := client(t)

	code, page := register(t, c, env.ts.URL, "halfdone", "secret-pass-1", "Пол Данных", "half@example.com",
		strings.NewReader("<html><script>alert(1)</script></html>"))
	if code == http.StatusSeeOther {
		t.Fatal("файл-не-картинка не должен проходить регистрацию")
	}
	if !bytes.Contains(page, []byte("изображением")) {
		t.Fatalf("форма должна объяснить проблему с аватаром:\n%s", page)
	}
	if _, err := env.st.UserByLogin(context.Background(), "halfdone"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("после неудачного аватара аккаунт не должен существовать, получили: %v", err)
	}

	big := make([]byte, 0, 520<<10)
	for len(big) <= 512<<10 {
		big = append(big, testGIF...)
	}
	code, page = register(t, c, env.ts.URL, "bigfella", "secret-pass-1", "", "big@example.com", bytes.NewReader(big))
	if code == http.StatusSeeOther {
		t.Fatal("аватар больше 512 КБ не должен проходить регистрацию")
	}
	if !bytes.Contains(page, []byte("512 КБ")) {
		t.Fatalf("форма должна объяснить про размер:\n%s", page)
	}
	if _, err := env.st.UserByLogin(context.Background(), "bigfella"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("аккаунт создался при невалидном аватаре: %v", err)
	}

	if code, page := register(t, c, env.ts.URL, "halfdone", "secret-pass-1", "Пол Данных", "half@example.com",
		bytes.NewReader(testGIF)); code != http.StatusSeeOther {
		t.Fatalf("повтор с исправленным аватаром должен пройти:\n%s", page)
	}
	user, err := env.st.UserByLogin(context.Background(), "halfdone")
	if err != nil {
		t.Fatal(err)
	}
	if user.Avatar == "" {
		t.Fatal("аватар со второй попытки не сохранился")
	}
}

// TestForgotWithoutSMTPHidesLink без работающей почты ссылка на экран постороннему
// не попадает: любой мог бы запросить сброс чужого аккаунта. Сообщение отличает
// «почта не настроена» от настоящего сбоя отправки.
func TestForgotWithoutSMTPHidesLink(t *testing.T) {
	env := testServer(t)
	c := client(t)

	register(t, c, env.ts.URL, "mailless", "secret-pass-1", "", "mailless@example.com", nil)
	logout(t, c, env.ts.URL)

	form := url.Values{"email": {"mailless@example.com"}, "csrf": {formCSRF(t, c, env.ts.URL)}}
	req, err := http.NewRequest("POST", env.ts.URL+"/auth/forgot", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Запрос пришёл не с самого сервера, как у любого посетителя сайта.
	req.Header.Set("X-Real-IP", "203.0.113.5")

	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if bytes.Contains(page, []byte("/reset?token=")) {
		t.Fatal("рабочая ссылка сброса пароля показана постороннему")
	}
	if !bytes.Contains(page, []byte("не настроено")) {
		t.Fatalf("форма должна честно сказать, что почта не настроена:\n%s", page)
	}

	// Письмо не потеряно: администратор достанет его из outbox.
	matches, _ := filepath.Glob(filepath.Join(env.outbox, "*.eml"))
	if len(matches) != 1 {
		t.Fatalf("в почтовом ящике %d писем, ждали 1", len(matches))
	}
}

// TestLoginThrottled лимит попыток входа: после бюджета форма возвращается
// со страницей студии и Retry-After, а не дежурным 429.
func TestLoginThrottled(t *testing.T) {
	env := testServerBudget(t, 2)
	c := client(t)
	csrf := formCSRF(t, c, env.ts.URL)

	// Неудачные попытки тоже идут в счёт.
	for i := 0; i < 2; i++ {
		res, err := c.PostForm(env.ts.URL+"/auth/login", url.Values{
			"login": {"kto-to"}, "password": {"neverno"}, "csrf": {csrf},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.Header.Get("Retry-After") != "" {
			t.Fatalf("попытка %d: Retry-After не должен стоять, пока бюджет цел", i+1)
		}
	}

	res, err := c.PostForm(env.ts.URL+"/auth/login", url.Values{
		"login": {"kto-to"}, "password": {"neverno"}, "csrf": {csrf},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	if !bytes.Contains(page, []byte("слишком много попыток входа")) {
		t.Fatalf("страница лимита не та:\n%s", page)
	}
	if res.Header.Get("Retry-After") == "" {
		t.Fatal("без Retry-After клиент не знает, когда вернуться")
	}
	// Лимит отдаёт страницу студии с CSRF, а не пустой 429 из middleware.
	if !bytes.Contains(page, []byte("name=\"csrf\"")) {
		t.Fatalf("лимит должен отдавать страницу с формой и CSRF:\n%s", page)
	}
}

// brokenUsers хранилище с «отвалившейся» таблицей пользователей.
type brokenUsers struct {
	*store.Store
}

func (brokenUsers) UserByLogin(context.Context, string) (*store.User, error) {
	return nil, errors.New("база недоступна")
}

// TestLoginStorageError сбой хранилища при входе не должен маскироваться
// под «неверный логин или пароль».
func TestLoginStorageError(t *testing.T) {
	env := startEnv(t, func(cfg *auth.Config, st *store.Store) {
		cfg.Users = brokenUsers{Store: st}
	})

	c := client(t)
	res, err := c.PostForm(env.ts.URL+"/auth/login", url.Values{
		"login":    {"кто-то"},
		"password": {"пароль"},
		"csrf":     {formCSRF(t, c, env.ts.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	body := string(page)
	if !strings.Contains(body, "не удалось войти: попробуйте позже") {
		t.Fatalf("сбой хранилища должен показать «попробуйте позже»:\n%s", body)
	}
	if strings.Contains(body, "неверный логин или пароль") {
		t.Fatal("сбой хранилища не должен выглядеть как неверный пароль")
	}
}

// TestRegisterDuplicateEmail вторая регистрация с чужой почтой возвращается
// с понятной ошибкой формы.
func TestRegisterDuplicateEmail(t *testing.T) {
	env := testServer(t)
	c := client(t)

	if code, _ := register(t, c, env.ts.URL, "first1", "secret-pass-1", "Иван", "same@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("первая регистрация: статус %d, ждали 303", code)
	}

	code, page := register(t, c, env.ts.URL, "second1", "secret-pass-1", "Пётр", "Same@Example.com", nil)
	if code == http.StatusSeeOther {
		t.Fatal("дубль почты не должен заводить второй аккаунт")
	}
	body := string(page)
	if !strings.Contains(body, "почт") || !strings.Contains(body, "уже") {
		t.Fatalf("форма должна объяснить про занятую почту:\n%s", body)
	}
	if strings.Contains(body, "500") || strings.Contains(body, "internal") {
		t.Fatalf("дубль почты: ошибка формы, а не сервера:\n%s", body)
	}
}
