package main

import (
	"os"
	"path/filepath"
	"strconv"
)

// staticRoot — каталог, внутри которого лежит вся статика сайта:
// index.html, design.html, style.css с css/ и assets/. Локально это public/
// рядом с main.go, в проде systemd подставляет абсолютный путь через
// STATIC_ROOT.
var staticRoot = envOr("STATIC_ROOT", "public")

// envOr читает переменную окружения, подставляя значение по умолчанию.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// envInt читает числовую переменную окружения. Мусор вместо числа — не
// повод не запускаться: берём запасное значение и предупреждаем в лог.
func (a *app) envInt(name string, fallback int) int {
	v := os.Getenv(name)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		a.log.Warn("переменная окружения не число", "name", name, "value", v, "fallback", fallback)
		return fallback
	}
	return n
}

// allowedStatic — единственные файлы в корне staticRoot, которые видит
// браузер. Всё остальное (main.go, go.mod, README.md, package.json, логи,
// бинарник сервера, .git, файл счётчика) лежит выше и наружу не отдаётся.
var allowedStatic = map[string]bool{
	"/style.css":   true,
	"/design.css":  true,
	"/catalog.css": true,
	"/print.css":   true,
}

// cssPrefix — каталог с разобранными на слои стилями, единственный, кроме
// assets, который отдаётся наружу целиком. Внутри только css: файлы
// генерируются руками, ничего исполняемого туда не попадает.
const cssPrefix = "/css/"

// jsPrefix — каталог с production-бандлом страницы фотоальбома.
const jsPrefix = "/js/"

// pageRoutes — адреса без .html: браузер просит короткий путь, а сервер
// отдаёт страницу из staticRoot. Ключ — то, что видно в строке адреса,
// значение — файл на диске.
var pageRoutes = map[string]string{
	"/maket":   "design.html",
	"/catalog": "catalog.html",
}

// assetsPrefix — единственная директория внутри staticRoot, открытая наружу.
const assetsPrefix = "/assets/"

// indexFile — путь к вёрстке внутри каталога статики.
func indexFile() string {
	return filepath.Join(staticRoot, "index.html")
}

// counterPath — файл, где лежит число визитов. В проде systemd подставляет
// путь из StateDirectory, локально файл лежит рядом с бинарником.
func counterPath() string {
	if p := os.Getenv("VISITS_FILE"); p != "" {
		return p
	}
	return "visits.dat"
}
