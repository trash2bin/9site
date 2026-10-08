package auth

import (
	// embed вшивает HTML-шаблоны из templates/ внутрь бинарника при сборке:
	// деплой остаётся одним файлом, а верстка правится обычными HTML-файлами,
	// а не гигантской строкой в Go-коде.
	_ "embed"
	"html/template"
)

// studioData — данные страницы закрытого раздела. Шаблоны — html/template:
// все значения автоматически экранируются, поэтому ФИО вида
// <script>...</script> отображается текстом, а не исполняется (XSS, контрольный
// вопрос про защиту).
type studioData struct {
	User      *User
	Avatar    string
	Message   string
	Error     string
	Open      string // какая вкладка модального окна открыта: login|register|forgot
	ResetLink string // ссылка сброса показывается только когда SMTP не настроен
	CSRF      string
}

// resetData — данные формы установки нового пароля по ссылке из письма.
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

// studioTemplate — и гостевая «стена» закрытого раздела, и страница
// авторизованного пользователя: различаются блоком {{if .User}}.
var studioTemplate = template.Must(template.New("studio").Parse(studioHTML))

// resetTemplate — страница нового пароля по ссылке из письма.
var resetTemplate = template.Must(template.New("reset").Parse(resetHTML))

// vaultTemplate — закрытый раздел. Отдаётся только авторизованным: гостя
// обработчик уводит на окно входа, этот шаблон он не видит.
var vaultTemplate = template.Must(template.New("vault").Parse(vaultHTML))
