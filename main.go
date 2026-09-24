package main

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// staticRoot — каталог, внутри которого лежит вся статика сайта:
// index.html, style.css и assets/. Локально это public/ рядом с main.go,
// в проде systemd подставляет абсолютный путь через STATIC_ROOT.
var staticRoot = envOr("STATIC_ROOT", "public")

// envOr читает переменную окружения, подставляя значение по умолчанию.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// allowedStatic — единственные файлы в корне staticRoot, которые видит
// браузер. Всё остальное (main.go, go.mod, README.md, package.json, логи,
// бинарник сервера, .git, файл счётчика) лежит выше и наружу не отдаётся.
var allowedStatic = map[string]bool{
	"/style.css":  true,
	"/design.css": true,
}

// pageRoutes — адреса без .html: браузер просит короткий путь, а сервер
// отдаёт страницу из staticRoot. Ключ — то, что видно в строке адреса,
// значение — файл на диске.
var pageRoutes = map[string]string{
	"/maket": "design.html",
}

// assetsPrefix — единственная директория внутри staticRoot, открытая наружу.
const assetsPrefix = "/assets/"

// indexFile — путь к вёрстке внутри каталога статики.
func indexFile() string {
	return filepath.Join(staticRoot, "index.html")
}

// visitMark — место в index.html, куда подставляется счётчик визитов.
const visitMark = "<!--visits-->"

// digitCount — сколько знаков показывать. Меньшие числа добиваются нулями,
// чтобы разрядность в футере не прыгала.
const digitCount = 5

// counterPath — файл, где лежит число визитов. В проде systemd подставляет
// путь из StateDirectory, локально файл лежит рядом с бинарником.
func counterPath() string {
	if p := os.Getenv("VISITS_FILE"); p != "" {
		return p
	}
	return "visits.dat"
}

// counter хранит число в памяти и пишет его на диск. Файл крошечный,
// поэтому записываем на каждый заход: так счёт не теряется при перезапуске.
var counter struct {
	sync.Mutex
	n int
}

// loadCounter читает сохранённое число. Отсутствующий или битый файл —
// не повод не запуститься: начинаем с нуля.
func loadCounter() {
	raw, err := os.ReadFile(counterPath())
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("счётчик: файл не прочитан: %v", err)
		return
	}

	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || n < 0 {
		log.Printf("счётчик: в файле не число (%q), начинаю с нуля", strings.TrimSpace(string(raw)))
		return
	}

	counter.n = n
	log.Printf("счётчик: продолжено с %d", n)
}

// hit увеличивает счётчик на единицу и сразу сохраняет результат.
// Ошибку записи наверх не отдаём: страницу надо показать в любом случае.
func hit() int {
	counter.Lock()
	defer counter.Unlock()

	counter.n++
	if err := saveCounter(counter.n); err != nil {
		log.Printf("счётчик: не сохранён: %v", err)
	}
	return counter.n
}

// saveCounter пишет число через временный файл: переименование в пределах
// одной файловой системы атомарно, поэтому при обрыве не останется
// половинчатого файла.
func saveCounter(n int) error {
	name := counterPath()
	tmp := name + ".tmp"

	if err := os.WriteFile(tmp, []byte(strconv.Itoa(n)+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, name); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// counterHTML раскладывает число на отдельные <span>, как было в вёрстке.
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

// indexHandler отдаёт index.html с подставленным счётчиком визитов.
// Страница собирается в памяти, поэтому файл на диске не меняется и
// остаётся статикой, которую можно открыть и без сервера.
func indexHandler(w http.ResponseWriter, r *http.Request) {
	page, err := os.ReadFile(indexFile())
	if err != nil {
		log.Printf("%s: %v", indexFile(), err)
		http.Error(w, "index.html не читается", http.StatusInternalServerError)
		return
	}

	html := strings.Replace(string(page), visitMark, counterHTML(hit()), 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Без no-store браузер и прокси могут показать вчерашний счёт.
	w.Header().Set("Cache-Control", "no-store")
	if _, err := w.Write([]byte(html)); err != nil {
		log.Printf("ответ: %v", err)
	}
}

// pageHandler отдаёт страницу из pageRoutes как HTML. Файл читается на
// каждый запрос: правки вёрстки видны сразу, без перезапуска сервера.
func pageHandler(file string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Join(staticRoot, file)

		page, err := os.ReadFile(name)
		if err != nil {
			log.Printf("%s: %v", name, err)
			http.Error(w, file+" не читается", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Без no-store браузер покажет вчерашний макет после правки вёрстки.
		w.Header().Set("Cache-Control", "no-store")
		if _, err := w.Write(page); err != nil {
			log.Printf("ответ: %v", err)
		}
	}
}

// staticOnly пропускает только статику сайта и отдаёт 404 на всё прочее.
func staticOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path.Clean схлопывает "/assets/../main.go" в "/main.go",
		// иначе такой запрос обошёл бы проверку префикса.
		cleaned := path.Clean(r.URL.Path)

		if !allowedStatic[cleaned] && !strings.HasPrefix(cleaned, assetsPrefix) {
			http.NotFound(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// checkIndex проверяет на старте, что метка счётчика вообще есть в вёрстке.
// Иначе сайт молча отдавал бы страницу без числа визитов.
func checkIndex() error {
	page, err := os.ReadFile(indexFile())
	if err != nil {
		return fmt.Errorf("%s: %w", indexFile(), err)
	}
	if !strings.Contains(string(page), visitMark) {
		return fmt.Errorf("%s: нет метки %s — счётчику некуда подставляться", indexFile(), visitMark)
	}
	return nil
}

// checkPages проверяет на старте, что все страницы из pageRoutes на месте.
// Иначе адрес из меню отдавал бы 500 вместо вёрстки.
func checkPages() error {
	for route, file := range pageRoutes {
		name := filepath.Join(staticRoot, file)
		if _, err := os.Stat(name); err != nil {
			return fmt.Errorf("%s (%s): %w", name, route, err)
		}
	}
	return nil
}

func main() {
	loadCounter()
	if err := checkIndex(); err != nil {
		log.Fatal(err)
	}
	if err := checkPages(); err != nil {
		log.Fatal(err)
	}

	fileServer := http.FileServer(http.Dir(staticRoot))

	// "/" и "/index.html" обслуживает счётчик, всё остальное — статика.
	// В ServeMux точный шаблон побеждает шаблон-префикс "/", поэтому
	// порядок регистрации значения не имеет.
	http.Handle("/", staticOnly(fileServer))
	http.HandleFunc("/{$}", indexHandler)
	http.HandleFunc("/index.html", indexHandler)

	// Красивые адреса: /maket и /maket/ отдают ту же вёрстку.
	// Вариант с косой чертой нужен, чтобы ссылка вида /maket/ не падала в 404.
	for route, file := range pageRoutes {
		http.HandleFunc(route, pageHandler(file))
		http.HandleFunc(route+"/{$}", pageHandler(file))
	}

	log.Println("Ivan's Little Web: http://localhost:8080")
	log.Println("Press Ctrl+C to stop")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
