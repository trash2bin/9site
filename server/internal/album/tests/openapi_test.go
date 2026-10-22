package album_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/api"
)

// TestSpecBehindLogin: спека закрыта сессией, гость уходит на вход.
func TestSpecBehindLogin(t *testing.T) {
	guest := testServerWith(t, fakeAccess{guest: true})
	res, _ := get(t, guest.ts.URL+api.SpecPath)
	if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/studio?auth=login" {
		t.Fatalf("гостю статус %d на %q", res.StatusCode, res.Header.Get("Location"))
	}

	env := testServerWith(t, fakeAccess{csrf: true, login: "reader"})
	res, body := get(t, env.ts.URL+api.SpecPath)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("статус %d", res.StatusCode)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q", got)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type %q", res.Header.Get("Content-Type"))
	}
	if !bytes.Contains(body, []byte(`"`+api.PhotosPath+`"`)) {
		t.Fatal("в спеке нет /api/photos")
	}
}
