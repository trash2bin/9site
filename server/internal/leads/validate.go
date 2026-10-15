package leads

import (
	"regexp"

	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// phonePattern — телефон в разумных рамках: цифры, пробелы, скобки, дефис,
// точка, ведущий плюс.
var phonePattern = regexp.MustCompile(`^\+?[\d\s()\-.]{5,20}$`)

// validateLead проверяет поля заявки; возвращает текст ошибки для лога.
// Почту проверяет общий webutil.ValidEmail — правило одно на все формы.
func validateLead(l store.Lead) string {
	if n := len([]rune(l.Name)); n < 2 || n > 100 {
		return "имя: 2–100 символов"
	}
	if !phonePattern.MatchString(l.Phone) {
		return "телефон: до 20 знаков — цифры, пробелы, скобки, дефис, ведущий плюс"
	}
	if !webutil.ValidEmail(l.Email) {
		return "некорректная почта"
	}
	return ""
}
