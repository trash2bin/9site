package leads

import (
	"context"
	"net/http"
	"net/url"
	"strings"

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
	// Адрес идёт в заявку и журнал, тот же ключ считает лимит в Register.
	ip := webutil.ClientIP(r)

	// Запрос с чужого сайта отсекаем, пустой Origin пропускаем: браузер
	// присылает его всегда, curl и старые браузеры не присылают.
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.baseOrigin() {
		s.log.Warn("форма Sign Up: чужой Origin", "ip", ip, "origin", origin)
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
		s.log.Info("форма Sign Up: отсеян бот по honeypot", "ip", ip)
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
		s.log.Warn("форма Sign Up: невалидная заявка", "ip", ip, "причина", errText)
		http.Redirect(w, r, "/maket#contact", http.StatusSeeOther)
		return
	}

	// Потеря заявки не должна ломать ответ: сбой уходит в лог громкой
	// ошибкой. Контекст без отмены, заявка уже принята.
	if err := s.store.SaveLead(context.WithoutCancel(r.Context()), lead); err != nil {
		s.log.Error("заявка не сохранена", "err", err)
	}
	s.log.Action("-", "заявка с формы Sign Up", ip)

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
