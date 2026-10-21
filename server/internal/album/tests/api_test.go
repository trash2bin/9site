package album_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// apiResponse форма ответа витрины.
type apiResponse struct {
	Photos []struct {
		Name   string `json:"name"`
		URL    string `json:"url"`
		Album  string `json:"album"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	} `json:"photos"`
	Albums []string `json:"albums"`
}

// TestPhotosAPI: витрина получает засев из базы и не кэшируется браузером.
func TestPhotosAPI(t *testing.T) {
	env := testServer(t)

	res, body := get(t, env.ts.URL+"/api/photos")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("статус %d\n%s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type %q", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control %q, ожидался no-store", got)
	}

	var data apiResponse
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("ответ не JSON: %v\n%s", err, body)
	}
	if len(data.Photos) != 37 {
		t.Fatalf("снимков %d, ожидалось 37", len(data.Photos))
	}

	first := data.Photos[0]
	if first.Name != "Nintendo 3DS" || first.URL != "/assets/img/console-3ds.jpg" ||
		first.Album != "gadgets" || first.Width != 1600 || first.Height != 1200 {
		t.Fatalf("первый снимок %+v", first)
	}
	if len(data.Albums) != 5 || data.Albums[0] != "gadgets" {
		t.Fatalf("разделы %v", data.Albums)
	}
}

// TestPhotosAPIAfterUpload: новая запись сразу видна витрине, включая
// раздел, которого раньше не было.
func TestPhotosAPIAfterUpload(t *testing.T) {
	env := testServer(t)

	if _, err := env.st.CreatePhoto(context.Background(), store.Photo{
		Name: "Свежий кадр", URL: "/photos/p0123456789abcdef.webp",
		Album: "travel", Width: 1200, Height: 1600, UploadedBy: "tester",
	}); err != nil {
		t.Fatalf("CreatePhoto: %v", err)
	}

	_, body := get(t, env.ts.URL+"/api/photos")
	var data apiResponse
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}

	last := data.Photos[len(data.Photos)-1]
	if last.Name != "Свежий кадр" || last.URL != "/photos/p0123456789abcdef.webp" || last.Album != "travel" {
		t.Fatalf("последний снимок %+v", last)
	}

	var found bool
	for _, slug := range data.Albums {
		if slug == "travel" {
			found = true
		}
	}
	if !found {
		t.Fatalf("раздел travel не попал в список: %v", data.Albums)
	}
}
