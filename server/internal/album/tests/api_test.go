package album_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/store"

	"github.com/ivan223/web1-personal-site/server/internal/api"
)

// apiResponse форма ответа витрины.
type apiResponse struct {
	Photos []struct {
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Album  string   `json:"album"`
		Width  int      `json:"width"`
		Height int      `json:"height"`
		Author string   `json:"author"`
		Tags   []string `json:"tags"`
	} `json:"photos"`
	Albums []struct {
		Slug  string `json:"slug"`
		Label string `json:"label"`
	} `json:"albums"`
	Tags []string `json:"tags"`
}

// TestPhotosAPI: витрина получает засев из базы и не кэшируется браузером.
func TestPhotosAPI(t *testing.T) {
	env := testServer(t)

	res, body := get(t, env.ts.URL+api.PhotosPath)
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
	if len(first.Tags) != 0 || len(data.Tags) != 0 {
		t.Fatalf("у засева не должно быть тегов: кадр %v, список %v", first.Tags, data.Tags)
	}
	if first.Author != "system" {
		t.Fatalf("автор засева %q, ожидался system", first.Author)
	}
	if len(data.Albums) != 5 || data.Albums[0].Slug != "gadgets" || data.Albums[0].Label != "техника" {
		t.Fatalf("разделы %v", data.Albums)
	}
}

// TestPhotosAPIAfterUpload: новая запись сразу видна витрине, включая
// раздел, которого раньше не было.
func TestPhotosAPIAfterUpload(t *testing.T) {
	env := testServer(t)

	if _, err := env.st.CreatePhoto(context.Background(), store.Photo{
		Name: "Свежий кадр", URL: api.FilesPath + "p0123456789abcdef.webp",
		Album: "music", Width: 1200, Height: 1600, UploadedBy: "tester",
	}); err != nil {
		t.Fatalf("CreatePhoto: %v", err)
	}

	_, body := get(t, env.ts.URL+api.PhotosPath)
	var data apiResponse
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}

	last := data.Photos[len(data.Photos)-1]
	if last.Name != "Свежий кадр" || last.URL != "/photos/p0123456789abcdef.webp" || last.Album != "music" {
		t.Fatalf("последний снимок %+v", last)
	}
	if last.Author != "tester" {
		t.Fatalf("автор загрузки %q, ожидался tester", last.Author)
	}

	// Разделы фиксированы: новая загрузка список не расширяет.
	slugs := make([]string, 0, len(data.Albums))
	for _, section := range data.Albums {
		slugs = append(slugs, section.Slug)
	}
	if want := []string{"gadgets", "garage", "mascots", "music", "misc"}; !slices.Equal(slugs, want) {
		t.Fatalf("разделы %v, ожидались %v", slugs, want)
	}
}
