package store_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// TestPhotoSeed засев из миграции на месте: витрина до первой загрузки
// показывает ровно те файлы, что лежали в статике.
func TestPhotoSeed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	photos, err := s.ListPhotos(ctx)
	if err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}
	if len(photos) != 37 {
		t.Fatalf("снимков в засеве %d, ожидалось 37", len(photos))
	}

	first := photos[0]
	if first.Name != "Nintendo 3DS" || first.URL != "/assets/img/console-3ds.jpg" ||
		first.Album != "gadgets" || first.Width != 1600 || first.Height != 1200 {
		t.Fatalf("первая запись засева: %+v", first)
	}
	if first.UploadedBy != "" {
		t.Fatalf("у засева не должно быть автора: %q", first.UploadedBy)
	}

	albums, err := s.Albums(ctx)
	if err != nil {
		t.Fatalf("Albums: %v", err)
	}
	if want := []string{"gadgets", "garage", "mascots", "misc", "music"}; !slices.Equal(albums, want) {
		t.Fatalf("разделы %v, ожидались %v", albums, want)
	}
}

func TestCreatePhoto(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreatePhoto(ctx, store.Photo{
		Name: "Снимок с телефона", URL: "/photos/p0123456789abcdef.webp",
		Album: "misc", Width: 1600, Height: 1200, Bytes: 1234, UploadedBy: "ivan",
	})
	if err != nil {
		t.Fatalf("CreatePhoto: %v", err)
	}
	if created.ID == 0 || created.CreatedAt == "" {
		t.Fatalf("id и дата должны заполниться базой: %+v", created)
	}

	photos, err := s.ListPhotos(ctx)
	if err != nil {
		t.Fatalf("ListPhotos: %v", err)
	}
	last := photos[len(photos)-1]
	if last != *created {
		t.Fatalf("в списке %+v, создана %+v", last, created)
	}

	// Тот же файл дважды: витрина не должна показывать дубль.
	if _, err := s.CreatePhoto(ctx, store.Photo{
		Name: "копия", URL: created.URL, Album: "misc", Width: 1, Height: 1,
	}); err == nil {
		t.Fatal("повторный url должен нарушать UNIQUE")
	}
}

func TestDeletePhoto(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.CreatePhoto(ctx, store.Photo{
		Name: "под снос", URL: "/photos/p00000000000000001.webp",
		Album: "garage", Width: 10, Height: 10,
	})
	if err != nil {
		t.Fatalf("CreatePhoto: %v", err)
	}

	deleted, err := s.DeletePhoto(ctx, created.ID)
	if err != nil {
		t.Fatalf("DeletePhoto: %v", err)
	}
	if deleted.URL != created.URL {
		t.Fatalf("удалён %+v, ожидалась запись %+v", deleted, created)
	}

	if _, err := s.DeletePhoto(ctx, created.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("повторное удаление: %v, ожидался ErrNotFound", err)
	}
}
