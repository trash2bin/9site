package auth

// Тесты закрытого раздела: прогоняют весь путь лабораторной N4 —
// регистрация, вход, закрытая страница, выход, смена данных, сброс пароля
// письмом — и проверяют защиту от SQL-инъекций и XSS.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
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

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// testServer поднимает сервер авторизации во временном каталоге: базу
// (реальную SQLite — тесты интеграционные) и каталоги файлов.
func testServer(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()

	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv, err := New(Config{
		Users:      st,
		Sessions:   st,
		AvatarsDir: filepath.Join(dir, "avatars"),
		OutboxDir:  filepath.Join(dir, "outbox"),
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	srv.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	// Ссылки в письмах должны вести на тестовый сервер, а не на localhost.
	srv.cfg.BaseURL = ts.URL

	return ts, srv
}

// client — браузерный клиент с cookie-банкой: cookie переживают запросы,
// как в настоящем браузере. За редиректами не следуем: проверяем сами.
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

// formCSRF достаёт подпись форм прямо из страницы — так же, как это делает
// браузер: значение лежит в скрытом поле каждой формы.
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

// register собирает multipart-запрос регистрации, как его шлёт форма.
// Возвращает статус и тело ответа.
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

// loginForm шлёт обычную форму входа и возвращает статус.
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

// logout выходит из авторизации.
func logout(t *testing.T, c *http.Client, base string) {
	t.Helper()

	res, err := c.PostForm(base+"/auth/logout", url.Values{"csrf": {formCSRF(t, c, base)}})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
}

// TestFullFlow — сценарий лабораторной целиком: закрытый раздел не пускает
// гостя, регистрация открывает доступ, выход его закрывает.
func TestFullFlow(t *testing.T) {
	ts, _ := testServer(t)
	c := client(t)

	// Гость ходит по студии свободно — авторизация не забор.
	res, err := c.Get(ts.URL + "/studio")
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

	// А вот vault по прямой ссылке гостю закрыт.
	res, err = c.Get(ts.URL + "/vault")
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

	// Регистрация.
	if code, _ := register(t, c, ts.URL, "tester", "secret-pass-1", "Иван Тестов", "tester@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("регистрация: статус %d, ждали 303", code)
	}

	// Закрытый раздел открывается.
	res, err = c.Get(ts.URL + "/vault")
	if err != nil {
		t.Fatal(err)
	}
	page, _ = io.ReadAll(res.Body)
	_ = res.Body.Close()
	if !bytes.Contains(page, []byte("секретный раздел")) {
		t.Fatalf("авторизованный не увидел закрытый раздел:\n%s", page)
	}

	// Выход — и закрытый раздел снова закрыт.
	logout(t, c, ts.URL)
	res, err = c.Get(ts.URL + "/vault")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("после выхода /vault отдал %d, ждали 303", res.StatusCode)
	}

	// Повторный вход тем же паролем.
	if code := loginForm(t, c, ts.URL, "tester", "secret-pass-1"); code != http.StatusSeeOther {
		t.Fatalf("повторный вход: статус %d, ждали 303", code)
	}
}

// TestSessionSurvivesRestart проверяет требование «без выхода пользователь
// авторизован и в следующем сеансе»: cookie живёт долго и работает из
// нового браузера.
func TestSessionSurvivesRestart(t *testing.T) {
	ts, _ := testServer(t)
	c := client(t)

	register(t, c, ts.URL, "durable", "secret-pass-1", "", "durable@example.com", nil)

	// Новый клиент = новый сеанс браузера; cookie подставляются из банки.
	fresh := &http.Client{Jar: c.Jar, CheckRedirect: c.CheckRedirect, Timeout: 10 * time.Second}
	res, err := fresh.Get(ts.URL + "/vault")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("во втором сеансе /vault отдал %d, ждали 200", res.StatusCode)
	}
}

// TestSQLInjection — попытка SQL-инъекции через форму входа не должна ни
// обойти авторизацию, ни уронить сервер (контрольный вопрос N6).
func TestSQLInjection(t *testing.T) {
	ts, _ := testServer(t)
	c := client(t)

	register(t, c, ts.URL, "victim", "secret-pass-1", "", "victim@example.com", nil)

	for _, payload := range []string{
		"victim' OR '1'='1",
		"victim' --",
		"' OR 1=1 --",
		`victim"; DROP TABLE users; --`,
	} {
		if code := loginForm(t, client(t), ts.URL, payload, "anything"); code == http.StatusSeeOther {
			t.Fatalf("инъекция %q прошла авторизацию", payload)
		}
	}

	// Таблица на месте, настоящий пользователь жив.
	if code := loginForm(t, client(t), ts.URL, "victim", "secret-pass-1"); code != http.StatusSeeOther {
		t.Fatal("настоящий пользователь после инъекций не может войти")
	}
}

// TestXSSEscape — вредоносное ФИО отображается текстом, а не исполняется.
func TestXSSEscape(t *testing.T) {
	ts, _ := testServer(t)
	c := client(t)

	malicious := `<script>alert("xss")</script>`
	register(t, c, ts.URL, "xssfan", "secret-pass-1", malicious, "xss@example.com", nil)

	res, err := c.Get(ts.URL + "/studio")
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

// TestPasswordReset — письмо со ссылкой уходит в почтовый ящик, ссылка
// меняет пароль и работает только один раз.
func TestPasswordReset(t *testing.T) {
	ts, srv := testServer(t)
	c := client(t)

	register(t, c, ts.URL, "forgetful", "old-pass-123", "", "forgetful@example.com", nil)
	logout(t, c, ts.URL)

	// Запрос сброса.
	res, err := c.PostForm(ts.URL+"/auth/forgot", url.Values{
		"email": {"forgetful@example.com"},
		"csrf":  {formCSRF(t, c, ts.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	// Письмо лежит в файле — достаём ссылку.
	matches, _ := filepath.Glob(filepath.Join(srv.outboxDir, "*.eml"))
	if len(matches) != 1 {
		t.Fatalf("в почтовом ящике %d писем, ждали 1", len(matches))
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	link := regexp.MustCompile(regexp.QuoteMeta(ts.URL) + `/reset\?token=[A-Za-z0-9_-]+`).Find(raw)
	if link == nil {
		t.Fatalf("в письме нет ссылки на сброс:\n%s", raw)
	}

	// Переходим по ссылке и ставим новый пароль.
	tokenValue := strings.TrimPrefix(string(link), ts.URL+"/reset?token=")
	res, err = c.PostForm(ts.URL+"/reset", url.Values{
		"token":    {tokenValue},
		"password": {"brand-new-pass-9"},
		"csrf":     {formCSRF(t, c, ts.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("сброс пароля: статус %d, ждали 303", res.StatusCode)
	}

	// Старый пароль больше не работает, новый — работает.
	if code := loginForm(t, client(t), ts.URL, "forgetful", "old-pass-123"); code == http.StatusSeeOther {
		t.Fatal("старый пароль остался рабочим")
	}
	if code := loginForm(t, client(t), ts.URL, "forgetful", "brand-new-pass-9"); code != http.StatusSeeOther {
		t.Fatal("новый пароль не работает")
	}

	// Ссылка одноразовая: повторная отправка пароля уже не принимается.
	res, err = c.PostForm(ts.URL+"/reset", url.Values{
		"token":    {tokenValue},
		"password": {"another-pass-9"},
		"csrf":     {formCSRF(t, c, ts.URL)},
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

// TestProfileUpdate — данные меняются, логин остаётся прежним, пароль
// подтверждается текущим.
func TestProfileUpdate(t *testing.T) {
	ts, _ := testServer(t)
	c := client(t)

	register(t, c, ts.URL, "editor", "secret-pass-1", "Старое Имя", "old@example.com", nil)

	form := url.Values{
		"csrf":         {formCSRF(t, c, ts.URL)},
		"login":        {"hacker"}, // попытка сменить логин — должна быть проигнорирована
		"full_name":    {"Новое Имя"},
		"email":        {"new@example.com"},
		"cur_password": {"secret-pass-1"},
		"new_password": {"second-pass-123"},
	}
	res, err := c.PostForm(ts.URL+"/auth/profile", form)
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

	// Пароль сменился — старый вход не работает.
	logout(t, c, ts.URL)
	if code := loginForm(t, client(t), ts.URL, "editor", "secret-pass-1"); code == http.StatusSeeOther {
		t.Fatal("старый пароль остался рабочим")
	}
	if code := loginForm(t, client(t), ts.URL, "editor", "second-pass-123"); code != http.StatusSeeOther {
		t.Fatal("новый пароль не работает")
	}
}

// TestAvatar — картинка сохраняется и отдаётся, файлы-не-картинки
// отвергаются (иначе под видом аватара заливается stored XSS).
func TestAvatar(t *testing.T) {
	ts, srv := testServer(t)
	c := client(t)

	gif := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\x00\x00\x00\xff\xff\xff!\xf9\x04" +
		"\x01\x00\x00\x00\x00,\x00\x00\x00\x00\x01\x00\x01\x00\x00\x02\x02D\x01\x00;")
	register(t, c, ts.URL, "shooter", "secret-pass-1", "", "shooter@example.com", bytes.NewReader(gif))

	user, err := srv.userByLogin(context.Background(), "shooter")
	if err != nil {
		t.Fatal(err)
	}
	if user.Avatar == "" {
		t.Fatal("аватар не сохранился")
	}

	res, err := c.Get(ts.URL + avatarURL(user))
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("аватар отдал %d, ждали 200", res.StatusCode)
	}

	// Файл-не-картинка отвергается.
	_, page := register(t, c, ts.URL, "evil", "secret-pass-1", "", "evil@example.com",
		strings.NewReader("<html><script>alert(1)</script></html>"))
	if !bytes.Contains(page, []byte("изображением")) {
		t.Fatalf("не-картинка не отвергнута:\n%s", page)
	}
}

// --- страховки после ревью ---

// brokenUsers — хранилище, у которого «отвалилась» таблица пользователей.
// Нужно, чтобы отличить в обработчике сбой базы от неверного пароля.
type brokenUsers struct {
	*store.Store
}

func (brokenUsers) UserByLogin(context.Context, string) (*store.User, error) {
	return nil, errors.New("база недоступна")
}

// TestLoginStorageError — сбой хранилища при входе не должен маскироваться
// под «неверный логин или пароль»: пользователю показывается честное
// «попробуйте позже».
func TestLoginStorageError(t *testing.T) {
	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv, err := New(Config{
		Users:      brokenUsers{Store: st},
		Sessions:   st,
		AvatarsDir: filepath.Join(dir, "avatars"),
		OutboxDir:  filepath.Join(dir, "outbox"),
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	srv.Register(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	srv.cfg.BaseURL = ts.URL

	c := client(t)
	csrf := formCSRF(t, c, ts.URL)

	res, err := c.PostForm(ts.URL+"/auth/login", url.Values{
		"login":    {"кто-то"},
		"password": {"пароль"},
		"csrf":     {csrf},
	})
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()

	body := string(page)
	if !strings.Contains(body, "не удалось войти — попробуйте позже") {
		t.Fatalf("сбой хранилища должен показать «попробуйте позже»:\n%s", body)
	}
	if strings.Contains(body, "неверный логин или пароль") {
		t.Fatalf("сбой хранилища не должен выглядеть как неверный пароль")
	}
}

// TestRegisterDuplicateEmail: раньше второй аккаунт с чужой почтой
// заводился молча — письмо со сбросом пароля могло уйти не тому человеку.
// Теперь форма обязана вернуться с понятной ошибкой.
func TestRegisterDuplicateEmail(t *testing.T) {
	ts, _ := testServer(t)
	c := client(t)

	if code, _ := register(t, c, ts.URL, "first1", "secret-pass-1", "Иван", "same@example.com", nil); code != http.StatusSeeOther {
		t.Fatalf("первая регистрация: статус %d, ждали 303", code)
	}

	code, page := register(t, c, ts.URL, "second1", "secret-pass-1", "Пётр", "Same@Example.com", nil)
	if code == http.StatusSeeOther {
		t.Fatal("дубль почты не должен заводить второй аккаунт")
	}
	body := string(page)
	if !strings.Contains(body, "почт") || !strings.Contains(body, "уже") {
		t.Fatalf("форма должна объяснить про занятую почту:\n%s", body)
	}
	if strings.Contains(body, "500") || strings.Contains(body, "internal") {
		t.Fatalf("дубль почты — не ошибка сервера, а ошибка формы:\n%s", body)
	}
}
