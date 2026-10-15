// Правила полей заявки гоняются через POST /signup: validateLead не
// экспортируется, наружу торчит только форма.
package leads_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestValidateLead(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		ok   bool
	}{
		{"телефон с пробелами и дефисами", url.Values{
			"name": {"Иван"}, "phone": {"+7 900 123-45-67"}, "email": {"i@example.com"},
		}, true},
		{"телефон со скобками", url.Values{
			"name": {"Иван"}, "phone": {"8(900)123-45-67"}, "email": {"i@example.com"},
		}, true},
		{"имя ровно в 100 символов", url.Values{
			"name": {strings.Repeat("а", 100)}, "phone": {"+79001234567"}, "email": {"i@x.com"},
		}, true},
		{"имя короче двух символов", url.Values{
			"name": {"И"}, "phone": {"+79001234567"}, "email": {"i@example.com"},
		}, false},
		{"пустой телефон", url.Values{
			"name": {"Иван"}, "phone": {""}, "email": {"i@example.com"},
		}, false},
		{"мусорная почта", url.Values{
			"name": {"Иван"}, "phone": {"+79001234567"}, "email": {"почта"},
		}, false},
		{"имя длиннее 100 символов", url.Values{
			"name": {strings.Repeat("а", 101)}, "phone": {"+79001234567"}, "email": {"i@x.com"},
		}, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts, st := testServer(t)

			res := postSignup(t, ts, c.form, http.Header{"Origin": {testOrigin}})

			want := "/maket#contact"
			wantLeads := 0
			if c.ok {
				want = "/maket?sent=1#contact"
				wantLeads = 1
			}
			if got := res.Header.Get("Location"); got != want {
				t.Fatalf("редирект %q, ожидался %q", got, want)
			}
			if n := leadCount(t, st); n != wantLeads {
				t.Fatalf("заявок %d, ожидалось %d", n, wantLeads)
			}
		})
	}
}
