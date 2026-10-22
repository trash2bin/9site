package store_test

import (
	"context"
	"slices"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// TestTags: тег заводится один раз, вешается на кадр, читается пачкой,
// а вместе с кадром уходят и связи.
func TestTags(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	first, err := s.EnsureTag(ctx, "retro")
	if err != nil {
		t.Fatalf("EnsureTag: %v", err)
	}
	again, err := s.EnsureTag(ctx, "retro")
	if err != nil {
		t.Fatalf("повторный EnsureTag: %v", err)
	}
	if first != again {
		t.Fatalf("тег задвоился: %d и %d", first, again)
	}

	other, err := s.EnsureTag(ctx, "tux")
	if err != nil {
		t.Fatal(err)
	}
	if first == other {
		t.Fatal("разные теги получили один номер")
	}

	photo, err := s.CreatePhoto(ctx, store.Photo{
		Name: "Кадр с тегами", URL: "/photos/p0000000000000002.webp", Album: "misc",
		Width: 10, Height: 10, UploadedBy: "reader",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AttachTags(ctx, photo.ID, []string{"retro", "tux", "нет-такого"}); err != nil {
		t.Fatalf("AttachTags: %v", err)
	}
	// Повторная навеска не плодит дубли.
	if err := s.AttachTags(ctx, photo.ID, []string{"retro"}); err != nil {
		t.Fatal(err)
	}

	tags, err := s.Tags(ctx)
	if err != nil {
		t.Fatalf("Tags: %v", err)
	}
	if want := []string{"retro", "tux"}; !slices.Equal(tags, want) {
		t.Fatalf("теги %v, ожидались %v", tags, want)
	}

	byPhoto, err := s.TagsByPhoto(ctx)
	if err != nil {
		t.Fatalf("TagsByPhoto: %v", err)
	}
	if got := byPhoto[photo.ID]; !slices.Equal(got, []string{"retro", "tux"}) {
		t.Fatalf("теги кадра %v", got)
	}

	// Каскад: связи уходят вместе с кадром, сами теги остаются.
	if _, err := s.DeletePhoto(ctx, photo.ID); err != nil {
		t.Fatal(err)
	}
	byPhoto, err = s.TagsByPhoto(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := byPhoto[photo.ID]; len(got) != 0 {
		t.Fatalf("связи кадра остались: %v", got)
	}
	// Без единого кадра тег из выдачи пропадает: мусор не живёт в фильтре.
	if tags, err := s.Tags(ctx); err != nil || len(tags) != 0 {
		t.Fatalf("теги после удаления кадра: %v, %v", tags, err)
	}
}
