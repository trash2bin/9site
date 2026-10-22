package album_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/api"
)

// writePhoto кладёт файл в каталог загрузок, как это делает обработчик.
func writePhoto(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestPhotoFile: файл загрузки отдаётся с типом по расширению и долгим кэшем,
// имя уникально, поэтому кэш можно не проверять.
func TestPhotoFile(t *testing.T) {
	env := testServer(t)

	const webp = "p0123456789abcdef.webp"
	writePhoto(t, env.photosDir, webp, []byte("RIFFxxxxWEBPVP8 "))

	res, body := get(t, env.ts.URL+api.FilesPath+webp)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("статус %d", res.StatusCode)
	}
	if got := res.Header.Get("Content-Type"); got != "image/webp" {
		t.Fatalf("Content-Type %q", got)
	}
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control %q", got)
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options %q", got)
	}
	if string(body) != "RIFFxxxxWEBPVP8 " {
		t.Fatalf("тело %q", body)
	}

	const gif = "p0000000000000001.gif"
	writePhoto(t, env.photosDir, gif, []byte("GIF89a"))
	if res, _ := get(t, env.ts.URL+api.FilesPath+gif); res.StatusCode != http.StatusOK {
		t.Fatalf("gif: статус %d", res.StatusCode)
	} else if got := res.Header.Get("Content-Type"); got != "image/gif" {
		t.Fatalf("gif: Content-Type %q", got)
	}
}

// TestPhotoFileRejectsForeignNames: наружу выходят только файлы с именем,
// которое генерирует сервер.
func TestPhotoFileRejectsForeignNames(t *testing.T) {
	env := testServer(t)

	writePhoto(t, env.photosDir, "p0123456789abcdef.webp", []byte("x"))
	// Файл рядом с каталогом загрузок: выйти за него нельзя.
	if err := os.WriteFile(filepath.Join(env.photosDir, "..", "site.db"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		api.FilesPath,
		api.FilesPath + "site.db",
		api.FilesPath + "p0123456789abcdef.png",
		api.FilesPath + "p0123456789abcde.webp",
		api.FilesPath + "..%2Fsite.db",
		api.FilesPath + "p0123456789abcdef.webp/",
	} {
		if res, _ := get(t, env.ts.URL+path); res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: статус %d, ожидался 404", path, res.StatusCode)
		}
	}
}
