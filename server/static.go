package main

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// staticOnly пропускает только статику сайта и отдаёт 404 на всё прочее.
func staticOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path.Clean схлопывает "/assets/../main.go" в "/main.go",
		// иначе такой запрос обошёл бы проверку префикса.
		cleaned := path.Clean(r.URL.Path)

		if !allowedStatic[cleaned] && !strings.HasPrefix(cleaned, assetsPrefix) &&
			!strings.HasPrefix(cleaned, cssPrefix) && !strings.HasPrefix(cleaned, jsPrefix) {
			http.NotFound(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// alwaysRevalidate говорит браузеру: не используй копию из кэша без
// проверки у сервера. Без этого header'а Chrome по эвристике (10% от
// возраста Last-Modified) держит старый print.css «свежим» часами:
// правка печатного вида не видна до очистки кэша — картинки в печатной
// версии, например, оставались скрытыми, хотя файл на сервере уже
// исправлен. Файлы без версионирования в пути должны перепроверяться;
// 304 отдаётся бесплатно, а кэш всё равно работает.
func alwaysRevalidate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// checkIndex проверяет на старте, что метка счётчика вообще есть в вёрстке.
// Иначе сайт молча отдавал бы страницу без числа визитов.
func (a *app) checkIndex() error {
	page, err := os.ReadFile(indexFile())
	if err != nil {
		return fmt.Errorf("%s: %w", indexFile(), err)
	}
	if !strings.Contains(string(page), visitMark) {
		return fmt.Errorf("%s: нет метки %s — счётчику некуда подставляться", indexFile(), visitMark)
	}
	return nil
}

// checkPages проверяет на старте, что все страницы из pageRoutes на месте
// и содержат свои метки (пока — «спасибо» после заявки на /maket).
// Иначе адрес из меню отдавал бы 500 вместо вёрстки, а сломанная разметка
// молча не показала бы «спасибо».
func (a *app) checkPages() error {
	for route, file := range pageRoutes {
		name := filepath.Join(staticRoot, file)
		body, err := os.ReadFile(name)
		if err != nil {
			return fmt.Errorf("%s (%s): %w", name, route, err)
		}

		mark, ok := pageMarkers[file]
		if ok && !strings.Contains(string(body), mark) {
			return fmt.Errorf("%s (%s): нет метки %s", name, route, mark)
		}
	}
	return nil
}

// styleEntries — точки входа стилей, которые грузит браузер.
var styleEntries = []string{"style.css", "design.css", "catalog.css"}

// extraStatic — файлы, которые подключены напрямую из HTML, а не через @import.
// Печатная тема и JS-бандл нужны фотоальбому; без них страница работает некорректно.
var extraStatic = []string{"print.css", filepath.Join("js", "main.js")}

// checkExtraStatic проверяет, что печатная тема и собранный скрипт на месте.
func (a *app) checkExtraStatic() error {
	for _, rel := range extraStatic {
		name := filepath.Join(staticRoot, rel)
		if _, err := os.Stat(name); err != nil {
			return fmt.Errorf("%s: %w (не забыл npm run build:ts?)", name, err)
		}
	}
	return nil
}

// checkStyles проверяет, что все файлы, подключённые через @import в точках
// входа, лежат на месте. Пропавший файл слоя иначе дал бы только 404 на
// стиль: страница открылась бы без вёрстки и без внятной ошибки в логе.
//
// Разбор нарочно простой: в проекте ровно одна форма записи —
// @import url("путь"); — и отдельный CSS-парсер здесь ни к чему.
func (a *app) checkStyles() error {
	var imports int

	for _, entry := range styleEntries {
		name := filepath.Join(staticRoot, entry)
		text, err := os.ReadFile(name)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}

		for _, line := range strings.Split(string(text), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "@import") {
				continue
			}

			rel := importPath(line)
			if rel == "" {
				return fmt.Errorf("%s: не разобран импорт %q", name, line)
			}

			// Путь в @import записан от корня статики — тот же адрес, по
			// которому файл просит браузер. Локально он же лежит в staticRoot.
			file := filepath.Join(staticRoot, filepath.FromSlash(rel))
			if _, err := os.Stat(file); err != nil {
				return fmt.Errorf("%s (%s): %w", file, rel, err)
			}
			imports++
		}
	}

	a.log.Info("стили подключены", "слоёв", imports, "входы", strings.Join(styleEntries, ", "))
	return nil
}

// importPath вытаскивает путь из строки @import url("путь").
func importPath(line string) string {
	const open, close = `url("`, `")`

	start := strings.Index(line, open)
	if start < 0 {
		return ""
	}
	rest := line[start+len(open):]

	end := strings.Index(rest, close)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
