package album_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// countPhotos сколько снимков сейчас в базе.
func countPhotos(t *testing.T, env *testEnv) int {
	t.Helper()
	photos, err := env.st.ListPhotos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return len(photos)
}

// pngFile однотонный PNG заданного размера: дешёвый вход для ресайза.
func pngFile(t *testing.T, w, h int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// animatedGIF две кадра: проверяем, что анимация на диск ложится без перекодирования.
func animatedGIF(t *testing.T) []byte {
	t.Helper()

	frames := []image.Image{
		image.NewPaletted(image.Rect(0, 0, 20, 20), color.Palette{color.Black, color.White}),
		image.NewPaletted(image.Rect(0, 0, 20, 20), color.Palette{color.White, color.Black}),
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: []*image.Paletted{
		frames[0].(*image.Paletted), frames[1].(*image.Paletted),
	}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// upload отправляет форму загрузки и отдаёт ответ с телом.
func upload(t *testing.T, env *testEnv, fields map[string]string, fileName string, data []byte) (*http.Response, []byte) {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for name, value := range fields {
		if err := w.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	if data != nil {
		part, err := w.CreateFormFile("photo", fileName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest("POST", env.ts.URL+"/vault/album", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return res, page
}

// TestAdminRequiresSession: гостя закрытая страница уводит на вход.
func TestAdminRequiresSession(t *testing.T) {
	env := testServerWith(t, fakeAccess{guest: true, csrf: true})

	for _, req := range []struct{ method, path string }{
		{"GET", "/vault/album"},
		{"POST", "/vault/album"},
		{"POST", "/vault/album/delete"},
	} {
		request, err := http.NewRequest(req.method, env.ts.URL+req.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := noRedirect.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()

		if res.StatusCode != http.StatusSeeOther {
			t.Fatalf("%s %s: статус %d, ожидался 303", req.method, req.path, res.StatusCode)
		}
		if loc := res.Header.Get("Location"); loc != "/studio?auth=login" {
			t.Fatalf("%s %s: редирект на %q", req.method, req.path, loc)
		}
	}
}

// TestAdminPageListsPhotos: страница показывает засев и поле подписи формы.
func TestAdminPageListsPhotos(t *testing.T) {
	env := testServer(t)

	res, body := get(t, env.ts.URL+"/vault/album")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("статус %d", res.StatusCode)
	}
	page := string(body)
	for _, want := range []string{"Nintendo 3DS", "в альбоме: 37", `name="csrf" value="test-csrf"`, "ALBUM UPLOAD"} {
		if !strings.Contains(page, want) {
			t.Fatalf("на странице нет %q", want)
		}
	}
}

// TestUploadRejectsBadCSRF: без подписи формы запись не появляется.
func TestUploadRejectsBadCSRF(t *testing.T) {
	env := testServerWith(t, fakeAccess{csrf: false, login: "tester"})
	before := countPhotos(t, env)

	_, page := upload(t, env, map[string]string{
		"csrf": "чужой", "name": "Кадр", "album": "misc",
	}, "pic.png", pngFile(t, 20, 20))

	if !strings.Contains(string(page), "форма устарела") {
		t.Fatalf("нет сообщения о подписи:\n%s", page)
	}
	if got := countPhotos(t, env); got != before {
		t.Fatalf("снимков стало %d, было %d", got, before)
	}
}

// TestUploadRejectsNotImage: тип определяется по содержимому, а не по имени файла.
func TestUploadRejectsNotImage(t *testing.T) {
	env := testServer(t)
	before := countPhotos(t, env)

	_, page := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "не картинка", "album": "misc",
	}, "pic.png", []byte("<html><script>alert(1)</script></html>"))

	if !strings.Contains(string(page), "png, jpeg или gif") {
		t.Fatalf("нет сообщения о формате:\n%s", page)
	}
	if got := countPhotos(t, env); got != before {
		t.Fatalf("снимков стало %d, было %d", got, before)
	}
}

// TestUploadRejectsOversize: больше 5 МБ не принимается.
func TestUploadRejectsOversize(t *testing.T) {
	env := testServer(t)
	before := countPhotos(t, env)

	huge := bytes.Repeat([]byte{0}, 6<<20)
	_, page := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "гигант", "album": "misc",
	}, "huge.png", huge)

	if !strings.Contains(string(page), "МБ") {
		t.Fatalf("нет сообщения про размер:\n%s", page[:min(len(page), 400)])
	}
	if got := countPhotos(t, env); got != before {
		t.Fatalf("снимков стало %d, было %d", got, before)
	}
}

// TestUploadResizesToWebP: большой PNG ужимается до 1600 px и ложится на диск
// как WebP, размеры в базе совпадают с файлом.
func TestUploadResizesToWebP(t *testing.T) {
	env := testServer(t)

	res, page := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "Широкий кадр", "album": "travel",
	}, "wide.png", pngFile(t, 2400, 1200))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("статус %d, ожидался 303:\n%s", res.StatusCode, page)
	}

	photos, err := env.st.ListPhotos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	photo := photos[len(photos)-1]

	if photo.Name != "Широкий кадр" || photo.Album != "travel" || photo.UploadedBy != "tester" {
		t.Fatalf("запись %+v", photo)
	}
	if photo.Width != 1600 || photo.Height != 800 {
		t.Fatalf("размер %dx%d, ожидался 1600x800", photo.Width, photo.Height)
	}
	if !strings.HasPrefix(photo.URL, "/photos/p") || !strings.HasSuffix(photo.URL, ".webp") {
		t.Fatalf("url %q", photo.URL)
	}

	name := filepath.Base(photo.URL)
	data, err := os.ReadFile(filepath.Join(env.photosDir, name))
	if err != nil {
		t.Fatalf("файл загрузки: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("RIFF")) || !bytes.Contains(data[:16], []byte("WEBP")) {
		t.Fatalf("файл не WebP: % x", data[:16])
	}
	if int64(len(data)) != photo.Bytes {
		t.Fatalf("в базе %d байт, на диске %d", photo.Bytes, len(data))
	}
	if entries, err := os.ReadDir(env.photosDir); err != nil || len(entries) != 1 {
		t.Fatalf("в каталоге мусор: %v, %v", entries, err)
	}

	// Файл отдаётся витрине по тому же адресу.
	if res, body := get(t, env.ts.URL+photo.URL); res.StatusCode != http.StatusOK || !bytes.Equal(body, data) {
		t.Fatalf("файл по %s: статус %d", photo.URL, res.StatusCode)
	}
}

// TestUploadKeepsGIF: анимированный GIF перекодировать нельзя, он сохраняется
// байт в байт.
func TestUploadKeepsGIF(t *testing.T) {
	env := testServer(t)
	source := animatedGIF(t)

	if res, page := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "Тикс машет", "album": "mascots",
	}, "tux.gif", source); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("статус %d:\n%s", res.StatusCode, page)
	}

	photos, err := env.st.ListPhotos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	photo := photos[len(photos)-1]
	if !strings.HasSuffix(photo.URL, ".gif") || photo.Width != 20 || photo.Height != 20 {
		t.Fatalf("запись %+v", photo)
	}

	data, err := os.ReadFile(filepath.Join(env.photosDir, filepath.Base(photo.URL)))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, source) {
		t.Fatal("gif перекодирован, анимация могла потеряться")
	}
}

// TestUploadRejectsBadAlbum: раздел проверяется по слагу.
func TestUploadRejectsBadAlbum(t *testing.T) {
	env := testServer(t)
	before := countPhotos(t, env)

	_, page := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "Кадр", "album": "Мой Раздел!",
	}, "pic.png", pngFile(t, 20, 20))

	if !strings.Contains(string(page), "раздел") {
		t.Fatalf("нет сообщения про раздел:\n%s", page)
	}
	if got := countPhotos(t, env); got != before {
		t.Fatalf("снимков стало %d, было %d", got, before)
	}
}

// TestUploadEscapesCaption: подпись с разметкой хранится как текст и
// экранируется на странице.
func TestUploadEscapesCaption(t *testing.T) {
	env := testServer(t)

	const caption = `<script>alert("xss")</script>`
	if res, page := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": caption, "album": "misc",
	}, "pic.png", pngFile(t, 20, 20)); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("статус %d:\n%s", res.StatusCode, page)
	}

	_, body := get(t, env.ts.URL+"/vault/album")
	page := string(body)
	if strings.Contains(page, caption) {
		t.Fatal("подпись попала на страницу без экранирования")
	}
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Fatalf("экранированная подпись не найдена:\n%s", page)
	}
}

// TestDeleteRemovesRowAndFile: удаление убирает запись и файл, засев из
// статики удаляется только из базы.
func TestDeleteRemovesRowAndFile(t *testing.T) {
	env := testServer(t)

	if res, page := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "Временный", "album": "misc",
	}, "pic.png", pngFile(t, 30, 30)); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("загрузка: статус %d:\n%s", res.StatusCode, page)
	}
	photos, err := env.st.ListPhotos(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	photo := photos[len(photos)-1]
	path := filepath.Join(env.photosDir, filepath.Base(photo.URL))

	remove := func(id int64) *http.Response {
		res, err := noRedirect.PostForm(env.ts.URL+"/vault/album/delete", map[string][]string{
			"csrf": {"test-csrf"}, "id": {itoa(id)},
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		return res
	}

	if res := remove(photo.ID); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("удаление: статус %d", res.StatusCode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("файл остался на диске: %v", err)
	}
	if got := countPhotos(t, env); got != 37 {
		t.Fatalf("снимков %d, ожидалось 37", got)
	}

	// Засев: файл в public/ не наш, трогать его нельзя.
	seed := photos[0]
	seedFile := filepath.Join("..", "..", "..", "..", "public", "assets", "img", filepath.Base(seed.URL))
	if _, err := os.Stat(seedFile); err != nil {
		t.Skipf("статики нет в дереве: %v", err)
	}
	if res := remove(seed.ID); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("удаление засева: статус %d", res.StatusCode)
	}
	if got := countPhotos(t, env); got != 36 {
		t.Fatalf("снимков %d, ожидалось 36", got)
	}
	if _, err := os.Stat(seedFile); err != nil {
		t.Fatalf("файл засева удалён: %v", err)
	}
}

// itoa номер без strconv, чтобы не тянуть пакет ради одной строки.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
