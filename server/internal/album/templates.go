package album

import (
	// embed вшивает вёрстку в бинарник: деплой остаётся одним файлом.
	_ "embed"
	"html/template"
)

//go:embed templates/admin.html
var adminHTML string

// adminTemplate страница загрузки: html/template экранирует подписи и логины.
var adminTemplate = template.Must(template.New("admin").Parse(adminHTML))
