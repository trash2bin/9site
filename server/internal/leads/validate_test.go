package leads

import (
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// TestValidateLead — правила полей заявки. Тест внутренний: validateLead
// не экспортируется, наружу торчит только форма.
func TestValidateLead(t *testing.T) {
	cases := []struct {
		lead store.Lead
		ok   bool
	}{
		{store.Lead{Name: "Иван", Phone: "+7 900 123-45-67", Email: "i@example.com"}, true},
		{store.Lead{Name: "Иван", Phone: "8(900)123-45-67", Email: "i@example.com"}, true},
		{store.Lead{Name: "И", Phone: "+79001234567", Email: "i@example.com"}, false},                // имя короче 2
		{store.Lead{Name: "Иван", Phone: "", Email: "i@example.com"}, false},                         // без телефона
		{store.Lead{Name: "Иван", Phone: "+79001234567", Email: "почта"}, false},                     // мусор
		{store.Lead{Name: strings.Repeat("а", 101), Phone: "+79001234567", Email: "i@x.com"}, false}, // имя длиннее 100
	}

	for _, c := range cases {
		errText := validateLead(c.lead)
		if c.ok && errText != "" {
			t.Fatalf("%+v: неожиданная ошибка %q", c.lead, errText)
		}
		if !c.ok && errText == "" {
			t.Fatalf("%+v: ожидалась ошибка валидации", c.lead)
		}
	}
}
