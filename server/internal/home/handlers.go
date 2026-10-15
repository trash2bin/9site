package home

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// visitMark место в index.html, куда подставляется счётчик визитов.
const visitMark = "<!--visits-->"

// thanksMark место в design.html, куда подставляется блок «спасибо»
// после заявки с формы Sign Up.
const thanksMark = "<!--sent-->"

// digitCount знаков в счётчике: меньше добивается нулями, чтобы разрядность не прыгала.
const digitCount = 5

// thanksHTML блок «спасибо», подставляется вместо маркера по ?sent=1.
const thanksHTML = `<p class="thanks">Спасибо! Мы свяжемся с вами.</p>`

// indexHandler отдаёт index.html с подставленным счётчиком.
func (s *Server) indexHandler(w http.ResponseWriter, r *http.Request) {
	page, err := os.ReadFile(s.indexFile())
	if err != nil {
		s.log.Error("index.html не читается", "err", err)
		http.Error(w, "index.html не читается", http.StatusInternalServerError)
		return
	}

	html := strings.Replace(string(page), visitMark, counterHTML(s.hit(r.Context())), 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Без no-store браузер и прокси могут показать вчерашний счёт.
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write([]byte(html)); err != nil {
		s.log.Error("ответ не ушёл", "err", err)
	}
}

// hit увеличивает счётчик и возвращает новое число. Ошибка записи наверх
// не идёт: сбой базы повод для warn, а не 500 на главной.
func (s *Server) hit(ctx context.Context) int {
	// Контекст без отмены: визит уже случился, обрыв соединения его не отменяет.
	n, err := s.store.Hit(context.WithoutCancel(ctx))
	if err != nil {
		s.log.Warn("счётчик: не сохранён", "err", err)
		return int(s.lastVisits.Load())
	}
	s.lastVisits.Store(int64(n))
	return n
}

func counterHTML(n int) string {
	digits := strconv.Itoa(n)
	if len(digits) < digitCount {
		digits = strings.Repeat("0", digitCount-len(digits)) + digits
	}

	var b strings.Builder
	for _, d := range digits {
		b.WriteString("<span>")
		b.WriteRune(d)
		b.WriteString("</span>")
	}
	return b.String()
}

// pageHandler отдаёт страницу из pageRoutes; файл читается на каждый запрос,
// правки вёрстки видны без перезапуска.
func (s *Server) pageHandler(file string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Join(s.staticRoot, file)

		page, err := os.ReadFile(name)
		if err != nil {
			s.log.Error("страница не читается", "file", name, "err", err)
			http.Error(w, file+" не читается", http.StatusInternalServerError)
			return
		}

		body := string(page)
		if strings.Contains(body, thanksMark) && r.URL.Query().Get("sent") == "1" {
			body = strings.Replace(body, thanksMark, thanksHTML, 1)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Без no-store браузер покажет вчерашний макет после правки вёрстки.
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write([]byte(body)); err != nil {
			s.log.Error("ответ не ушёл", "err", err)
		}
	}
}
