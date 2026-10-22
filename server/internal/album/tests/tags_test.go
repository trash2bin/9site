// Тесты тегов: админ заводит новый, остальным доступен только список из базы.
package album_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/store"

	"github.com/ivan223/web1-personal-site/server/internal/api"
)

// uploadTags отправляет форму с полем «теги» и отдаёт ответ с телом.
func uploadTags(t *testing.T, env *testEnv, tags string) (*http.Response, []byte) {
	t.Helper()
	return upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "Кадр с тегами", "album": "misc", "tags": tags,
	}, "tagged.png", pngFile(t, 120, 90))
}

// TestTagsAdminCreatesNew: админ пишет теги через запятую, они нормализуются,
// дубли убираются, тег появляется в базе и в ответе витрины.
func TestTagsAdminCreatesNew(t *testing.T) {
	env := testServer(t)

	res, page := uploadTags(t, env, " retro , TUX,retro ")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("статус %d, ожидался 303:\n%s", res.StatusCode, page)
	}

	tags, err := env.st.Tags(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"retro", "tux"}; !slices.Equal(tags, want) {
		t.Fatalf("теги в базе %v, ожидались %v", tags, want)
	}

	_, body := get(t, env.ts.URL+api.PhotosPath)
	var data apiResponse
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("ответ не JSON: %v", err)
	}
	last := data.Photos[len(data.Photos)-1]
	if !slices.Equal(last.Tags, []string{"retro", "tux"}) {
		t.Fatalf("теги кадра %v", last.Tags)
	}
	if !slices.Equal(data.Tags, []string{"retro", "tux"}) {
		t.Fatalf("список тегов витрины %v", data.Tags)
	}
}

// TestTagsUserOnlyExisting: юзеру новый тег недоступен, а существующий можно.
func TestTagsUserOnlyExisting(t *testing.T) {
	env := testServerWith(t, fakeAccess{csrf: true, login: "reader"})
	ctx := context.Background()

	// Тег известен, пока висит хоть на одном кадре.
	photo, err := env.st.CreatePhoto(ctx, store.Photo{
		Name: "старый кадр", URL: api.FilesPath + "p0000000000000001.webp",
		Album: "misc", Width: 10, Height: 10, UploadedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.st.EnsureTag(ctx, "retro"); err != nil {
		t.Fatal(err)
	}
	if err := env.st.AttachTags(ctx, photo.ID, []string{"retro"}); err != nil {
		t.Fatal(err)
	}

	res, page := uploadTags(t, env, "retro")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("известный тег: статус %d\n%s", res.StatusCode, page)
	}

	res, page = uploadTags(t, env, "retro, fresh")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("новый тег: статус %d, ожидалась страница с ошибкой:\n%s", res.StatusCode, page)
	}
	if !strings.Contains(string(page), "новый тег заводит админ") {
		t.Fatalf("нет объяснения про новый тег:\n%s", page)
	}
	if tags, err := env.st.Tags(ctx); err != nil || !slices.Equal(tags, []string{"retro"}) {
		t.Fatalf("теги после отказа %v (%v)", tags, err)
	}
}

// TestTagsRejectBadAndMany: форма тега и потолок в пять штук.
func TestTagsRejectBadAndMany(t *testing.T) {
	env := testServer(t)

	res, page := uploadTags(t, env, "Bad_Tag")
	if res.StatusCode != http.StatusOK || !strings.Contains(string(page), "тег: строчная латиница") {
		t.Fatalf("плохой тег: %d\n%s", res.StatusCode, page)
	}

	res, page = uploadTags(t, env, "t1,t2,t3,t4,t5,t6")
	if res.StatusCode != http.StatusOK || !strings.Contains(string(page), "тегов не больше 5") {
		t.Fatalf("много тегов: %d\n%s", res.StatusCode, page)
	}

	if got := countPhotos(t, env); got != 37 {
		t.Fatalf("снимков %d, ожидалось 37", got)
	}
}

// uploadBoxes отправляет форму с отмеченными галочками тегов.
func uploadBoxes(t *testing.T, env *testEnv, boxes ...string) (*http.Response, []byte) {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, box := range boxes {
		if err := w.WriteField("tag", box); err != nil {
			t.Fatal(err)
		}
	}
	for name, value := range map[string]string{
		"csrf": "test-csrf", "name": "Кадр с галочками", "album": "misc",
	} {
		if err := w.WriteField(name, value); err != nil {
			t.Fatal(err)
		}
	}
	part, err := w.CreateFormFile("photo", "boxes.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(pngFile(t, 100, 80)); err != nil {
		t.Fatal(err)
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

// TestTagsFromCheckboxes: юзер выбирает теги галочками из базы, свободного
// поля «новые теги» у него нет, а админ его видит.
func TestTagsFromCheckboxes(t *testing.T) {
	env := testServerWith(t, fakeAccess{csrf: true, login: "reader"})
	ctx := context.Background()

	seed, err := env.st.CreatePhoto(ctx, store.Photo{
		Name: "кадр с тегами", URL: api.FilesPath + "p0000000000000002.webp",
		Album: "misc", Width: 10, Height: 10, UploadedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"retro", "tux"} {
		if _, err := env.st.EnsureTag(ctx, slug); err != nil {
			t.Fatal(err)
		}
	}
	if err := env.st.AttachTags(ctx, seed.ID, []string{"retro", "tux"}); err != nil {
		t.Fatal(err)
	}

	_, page := get(t, env.ts.URL+"/vault/album")
	for _, want := range []string{`name="tag" value="retro"`, `name="tag" value="tux"`} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("нет галочки %q", want)
		}
	}
	if strings.Contains(string(page), "новые теги") {
		t.Fatal("юзеру показали поле для новых тегов")
	}

	res, body := uploadBoxes(t, env, "retro", "tux")
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("статус %d, ожидался 303:\n%s", res.StatusCode, body)
	}

	byPhoto, err := env.st.TagsByPhoto(ctx)
	if err != nil {
		t.Fatal(err)
	}
	photos, err := env.st.ListPhotos(ctx)
	if err != nil {
		t.Fatal(err)
	}
	last := photos[len(photos)-1]
	if got := byPhoto[last.ID]; !slices.Equal(got, []string{"retro", "tux"}) {
		t.Fatalf("теги кадра %v", got)
	}

	// Админ видит поле для новых тегов.
	adminEnv := testServer(t)
	if _, err := adminEnv.st.EnsureTag(context.Background(), "retro"); err != nil {
		t.Fatal(err)
	}
	adminSeed, err := adminEnv.st.CreatePhoto(context.Background(), store.Photo{
		Name: "кадр админа", URL: api.FilesPath + "p0000000000000003.webp",
		Album: "misc", Width: 10, Height: 10, UploadedBy: "owner",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := adminEnv.st.AttachTags(context.Background(), adminSeed.ID, []string{"retro"}); err != nil {
		t.Fatal(err)
	}
	_, adminPage := get(t, adminEnv.ts.URL+"/vault/album")
	if !strings.Contains(string(adminPage), "новые теги") {
		t.Fatal("админу не показали поле для новых тегов")
	}
}

// TestRejectedUploadLeavesNoTag: отказ по файлу не заводит тег, иначе в
// фильтре витрины навсегда висела бы метка без единого кадра.
func TestRejectedUploadLeavesNoTag(t *testing.T) {
	env := testServer(t)
	ctx := context.Background()

	res, page := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "Битый кадр", "album": "misc", "tags": "retr0",
	}, "broken.png", []byte("это не картинка"))
	if res.StatusCode != http.StatusOK || !strings.Contains(string(page), "нужен файл png") {
		t.Fatalf("ожидался отказ по файлу, статус %d:\n%s", res.StatusCode, page)
	}
	if tags, err := env.st.Tags(ctx); err != nil || len(tags) != 0 {
		t.Fatalf("после отказа в базе теги %v (%v)", tags, err)
	}

	// Успешная загрузка с тем же тегом заводит его: строка не «съедена» отказом.
	if res, page := uploadTags(t, env, "retr0"); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("повторная загрузка: статус %d\n%s", res.StatusCode, page)
	}
	if tags, err := env.st.Tags(ctx); err != nil || !slices.Equal(tags, []string{"retr0"}) {
		t.Fatalf("теги после успешной загрузки %v (%v)", tags, err)
	}
}
