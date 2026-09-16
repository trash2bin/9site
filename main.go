package main

import (
	"log"
	"net/http"
	"path"
	"strings"
)

// allowedFiles — единственные файлы в корне, которые видит браузер.
// Всё остальное (main.go, go.mod, README.md, package.json, логи, бинарник
// сервера, .git) наружу не отдаётся.
var allowedFiles = map[string]bool{
	"/":           true,
	"/index.html": true,
	"/style.css":  true,
}

// assetsPrefix — единственная директория, открытая наружу.
const assetsPrefix = "/assets/"

// staticOnly пропускает только статику сайта и отдаёт 404 на всё прочее.
func staticOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// path.Clean схлопывает "/assets/../main.go" в "/main.go",
		// иначе такой запрос обошёл бы проверку префикса.
		cleaned := path.Clean(r.URL.Path)

		if !allowedFiles[cleaned] && !strings.HasPrefix(cleaned, assetsPrefix) {
			http.NotFound(w, r)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	fileServer := http.FileServer(http.Dir("."))

	http.Handle("/", staticOnly(fileServer))

	log.Println("Ivan's Little Web: http://localhost:8080")
	log.Println("Press Ctrl+C to stop")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
