package home

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// allowedStatic — единственные файлы в корне staticRoot, которые видит
// браузер. Всё остальное (main.go, go.mod, README.md, package.json, логи,
// бинарник сервера, .git, файл счётчика) лежит выше и наружу не отдаётся.
var allowedStatic = map[string]bool{
	"/style.css":   true,
	"/design.css":  true,
	"/catalog.css": true,
	"/print.css":   true,
}

// Каталоги внутри staticRoot, открытые наружу целиком, и каталог с
// production-бандлом страницы фотоальбома. Внутри только css/js/картинки:
// ничего исполняемого туда не попадает.
const (
	assetsPrefix = "/assets/"
	cssPrefix    = "/css/"
	jsPrefix     = "/js/"
)

// indexFile — путь к вёрстке главной внутри каталога статики.
func (s *Server) indexFile() string {
	return filepath.Join(s.staticRoot, "index.html")
}

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

		// Каталоги наружу не отдаются: http.FileServer показал бы оглавление —
		// список файлов с именами, размерами и датами. Проверяем исходный
		// путь, а не cleaned: Clean срезает концевую косую черту и
		// "/css/auth" проходило бы как обычный файл.
		if !allowedStatic[cleaned] && strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// alwaysRevalidate говорит браузеру: не используй копию из кэша без
// проверки у сервера. Без этого header'а Chrome по эвристике (10% от
// возраста Last-Modified) держит старый print.css «свежим» часами.
func alwaysRevalidate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// checkIndex проверяет на старте, что метка счётчика есть в вёрстке.
// Иначе сайт молча отдавал бы страницу без числа визитов.
func (s *Server) checkIndex() error {
	page, err := os.ReadFile(s.indexFile())
	if err != nil {
		return fmt.Errorf("%s: %w", s.indexFile(), err)
	}
	if !strings.Contains(string(page), visitMark) {
		return fmt.Errorf("%s: нет метки %s — счётчику некуда подставляться", s.indexFile(), visitMark)
	}
	return nil
}

// checkPages проверяет на старте, что все страницы из pageRoutes на месте
// и содержат свои метки (пока — «спасибо» после заявки на /maket).
func (s *Server) checkPages() error {
	for route, file := range pageRoutes {
		name := filepath.Join(s.staticRoot, file)
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

// extraStatic — файлы, подключённые напрямую из HTML, а не через @import.
// Печатная тема и JS-бандл нужны фотоальбому.
var extraStatic = []string{"print.css", filepath.Join("js", "main.js")}

// checkExtraStatic проверяет, что печатная тема и собранный скрипт на месте.
func (s *Server) checkExtraStatic() error {
	for _, rel := range extraStatic {
		name := filepath.Join(s.staticRoot, rel)
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
func (s *Server) checkStyles() error {
	var imports int

	for _, entry := range styleEntries {
		name := filepath.Join(s.staticRoot, entry)
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
			// которому файл просит браузер.
			file := filepath.Join(s.staticRoot, filepath.FromSlash(rel))
			if _, err := os.Stat(file); err != nil {
				return fmt.Errorf("%s (%s): %w", file, rel, err)
			}
			imports++
		}
	}

	s.log.Info("стили подключены", "слоёв", imports, "входы", strings.Join(styleEntries, ", "))
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
