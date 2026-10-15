package main

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// Ограничения полей формы Sign Up. Телефон — разумные рамки: цифры,
// пробелы, скобки, дефис, ведущий плюс.
var (
	phonePattern = regexp.MustCompile(`^\+?[\d\s()\-.]{5,20}$`)
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// Ограничение частоты заявок с одного адреса: бот не выкачает таблицу leads,
// а человек с лимитом не столкнётся.
const (
	signupLimit  = 5
	signupWindow = 10 * time.Minute
)

// signupHandler принимает форму Sign Up со страницы /maket и сохраняет
// заявку в таблицу leads. Схема PRG: после сохранения — редирект обратно
// с ?sent=1, чтобы обновление страницы не повторяло отправку.
//
// Защита от спама: honeypot (скрытое поле company — человек его не
// заполняет), лимитер по адресу и проверка Origin. CSRF здесь не нужен:
// форма анонимная и публичная, мутирует только таблица заявок.
func (a *app) signupHandler(w http.ResponseWriter, r *http.Request) {
	ip := webutil.ClientIP(r)

	if a.signupLimit.Throttle(ip) {
		a.log.Warn("форма Sign Up: лимит заявок", "ip", ip)
		http.Error(w, "слишком много заявок — попробуйте позже", http.StatusTooManyRequests)
		return
	}

	// Запрос с чужого сайта (или подделанный Origin) отсекаем: браузер
	// Origin присылает всегда, curl и старые браузеры — не присылают.
	if origin := r.Header.Get("Origin"); origin != "" && origin != a.baseOrigin() {
		a.log.Warn("форма Sign Up: чужой Origin", "ip", ip, "origin", origin)
		http.Error(w, "запрос с чужого сайта", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/maket#contact", http.StatusSeeOther)
		return
	}

	// Honeypot: заполненное скрытое поле означает бота. Отвечаем как
	// успешной — бот не должен понять, что он раскрыт.
	if strings.TrimSpace(r.PostFormValue("company")) != "" {
		a.log.Info("форма Sign Up: отсеян бот по honeypot", "ip", ip)
		http.Redirect(w, r, "/maket#contact", http.StatusSeeOther)
		return
	}

	lead := store.Lead{
		Name:      strings.TrimSpace(r.PostFormValue("name")),
		Phone:     strings.TrimSpace(r.PostFormValue("phone")),
		Email:     strings.TrimSpace(r.PostFormValue("email")),
		IP:        ip,
		UserAgent: r.UserAgent(),
	}
	if errText := validateLead(lead); errText != "" {
		a.log.Warn("форма Sign Up: невалидная заявка", "ip", ip, "причина", errText)
		http.Redirect(w, r, "/maket#contact", http.StatusSeeOther)
		return
	}

	// Заявка — побочный эффект: её потеря не должна ломать ответ, но в
	// лог идёт громкой ошибкой.
	if err := a.store.SaveLead(context.WithoutCancel(r.Context()), lead); err != nil {
		a.log.Error("заявка не сохранена", "err", err)
	}
	a.log.Action("-", "заявка с формы Sign Up", ip)

	http.Redirect(w, r, "/maket?sent=1#contact", http.StatusSeeOther)
}

// baseOrigin — разрешённый источник для Origin-заголовка: тот же сайт.
func (a *app) baseOrigin() string {
	u, err := url.Parse(a.baseURL)
	if err != nil || u.Scheme == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// validateLead проверяет поля заявки; возвращает текст ошибки для лога.
func validateLead(l store.Lead) string {
	if n := len([]rune(l.Name)); n < 2 || n > 100 {
		return "имя: 2–100 символов"
	}
	if !phonePattern.MatchString(l.Phone) {
		return "телефон: до 20 знаков — цифры, пробелы, скобки, дефис, ведущий плюс"
	}
	if !emailPattern.MatchString(l.Email) || len(l.Email) > 254 {
		return "некорректная почта"
	}
	return ""
}
