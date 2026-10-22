package auth

import (
	// embed вшивает HTML-шаблоны в бинарник: деплой остаётся одним файлом,
	// а вёрстка правится обычными файлами в templates/.
	_ "embed"
	"html/template"
)

// studioData данные страницы закрытого раздела; html/template экранирует все значения (XSS).
type studioData struct {
	User      *User
	Avatar    string
	Message   string
	Error     string
	Open      string // какая вкладка модального окна открыта: login|register|forgot
	ResetLink string // ссылка сброса показывается только когда SMTP не настроен
	CSRF      string
}

type resetData struct {
	Token string
	Error string
	CSRF  string
}

//go:embed templates/studio.html
var studioHTML string

//go:embed templates/reset.html
var resetHTML string

//go:embed templates/vault.html
var vaultHTML string

//go:embed templates/users.html
var usersHTML string

// studioTemplate и гостевая «стена» закрытого раздела, и страница
// авторизованного: различаются блоком {{if .User}}.
var studioTemplate = template.Must(template.New("studio").Parse(studioHTML))

var resetTemplate = template.Must(template.New("reset").Parse(resetHTML))

// vaultTemplate закрытый раздел; доступ проверяет обработчик, не шаблон.
var vaultTemplate = template.Must(template.New("vault").Parse(vaultHTML))

// usersTemplate управление правами; открывает только RequireAdmin.
var usersTemplate = template.Must(template.New("users").Parse(usersHTML))
