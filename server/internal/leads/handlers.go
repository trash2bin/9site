package leads

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// tooMany ответ при исчерпанном бюджете; Retry-After уже поставил middleware.
func (s *Server) tooMany(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "слишком много заявок: попробуйте позже", http.StatusTooManyRequests)
}

// handleSignup принимает форму Sign Up, сохраняет заявку, далее PRG-редирект
// с ?sent=1. Защита: honeypot, лимитер и Origin; CSRF не нужен, форма анонимна.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	// Адрес идёт в заявку и журнал, тот же ключ считает лимит в Register.
	ip := webutil.ClientIP(r)

	// Запрос с чужого сайта отсекаем, пустой Origin пропускаем: браузер
	// присылает его всегда, curl и старые браузеры не присылают.
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.baseOrigin() {
		s.log.Warn("sign-up form: foreign Origin", "ip", ip, "origin", origin)
		http.Error(w, "запрос с чужого сайта", http.StatusForbidden)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/maket#contact", http.StatusSeeOther)
		return
	}

	// Honeypot: заполненное скрытое поле означает бота. Отвечаем как
	// успешной, бот не должен понять, что раскрыт.
	if strings.TrimSpace(r.PostFormValue("company")) != "" {
		s.log.Info("sign-up form: bot rejected by honeypot", "ip", ip)
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
		s.log.Warn("sign-up form: invalid lead", "ip", ip, "reason", errText)
		http.Redirect(w, r, "/maket#contact", http.StatusSeeOther)
		return
	}

	// Потеря заявки не должна ломать ответ: сбой уходит в лог громкой
	// ошибкой. Контекст без отмены, заявка уже принята.
	if err := s.store.SaveLead(context.WithoutCancel(r.Context()), lead); err != nil {
		s.log.Error("lead not saved", "err", err)
		s.log.Action(logging.Action{
			Login: "anonymous", Event: EventLeadSubmitFailed,
			Result: logging.ResultFailure, IP: ip,
		})
	} else {
		s.log.Action(logging.Action{
			Login: "anonymous", Event: EventLeadSubmitted,
			Result: logging.ResultSuccess, IP: ip,
		})
	}

	http.Redirect(w, r, "/maket?sent=1#contact", http.StatusSeeOther)
}

// baseOrigin разрешённый источник для Origin: тот же сайт.
func (s *Server) baseOrigin() string {
	u, err := url.Parse(s.baseURL)
	if err != nil || u.Scheme == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
