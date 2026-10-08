package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// indexHandler отдаёт index.html с подставленным счётчиком визитов.
// Страница собирается в памяти, поэтому файл на диске не меняется и
// остаётся статикой, которую можно открыть и без сервера.
func (a *app) indexHandler(w http.ResponseWriter, r *http.Request) {
	page, err := os.ReadFile(indexFile())
	if err != nil {
		a.log.Error("index.html не читается", "err", err)
		http.Error(w, "index.html не читается", http.StatusInternalServerError)
		return
	}

	html := strings.Replace(string(page), visitMark, counterHTML(a.hit()), 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Без no-store браузер и прокси могут показать вчерашний счёт.
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write([]byte(html)); err != nil {
		a.log.Error("ответ не ушёл", "err", err)
	}
}

// pageHandler отдаёт страницу из pageRoutes как HTML. Файл читается на
// каждый запрос: правки вёрстки видны сразу, без перезапуска сервера.
func (a *app) pageHandler(file string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Join(staticRoot, file)

		page, err := os.ReadFile(name)
		if err != nil {
			a.log.Error("страница не читается", "file", name, "err", err)
			http.Error(w, file+" не читается", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Без no-store браузер покажет вчерашний макет после правки вёрстки.
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(page); err != nil {
			a.log.Error("ответ не ушёл", "err", err)
		}
	}
}
