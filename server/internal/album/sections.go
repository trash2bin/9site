package album

// Section раздел альбома: слаг в базе и подпись для формы загрузки.
type Section struct {
	Slug  string
	Label string
}

// Sections фиксированный список разделов: техническая часть каталога, как
// допустимые форматы файлов. Произвольные метки живут в тегах. Слаг и подпись
// едут на витрину вместе в /api/photos, подписей во фронте нет.
var Sections = []Section{
	{Slug: "gadgets", Label: "техника"},
	{Slug: "garage", Label: "гараж"},
	{Slug: "mascots", Label: "tux"},
	{Slug: "music", Label: "музыка"},
	{Slug: "misc", Label: "прочее"},
}

// validSection проверяет выбор формы: раздел только из списка.
func validSection(slug string) bool {
	for _, section := range Sections {
		if section.Slug == slug {
			return true
		}
	}
	return false
}
