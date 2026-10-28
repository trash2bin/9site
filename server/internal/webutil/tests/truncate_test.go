// Тесты обрезки строк для полей, которые клиент присылает без ограничений.
package webutil_test

import (
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"короче лимита", "привет", 100, "привет"},
		{"по лимиту", "привет", 6, "привет"},
		{"реже по рунам", "Їжак-в-норе", 4, "Їжак"},
		{"ноль", "привет", 0, ""},
		{"пусто", "", 5, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := webutil.TruncateRunes(c.in, c.n); got != c.want {
				t.Fatalf("TruncateRunes(%q, %d) = %q, ожидалось %q", c.in, c.n, got, c.want)
			}
		})
	}

	long := strings.Repeat("ы", 500)
	if got := webutil.TruncateRunes(long, 300); len([]rune(got)) != 300 {
		t.Fatalf("длинная строка: %d рун", len([]rune(got)))
	}
}
