// Тесты журнала просмотров и меты сессии.
package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// TestPageViews: запись и выборка просмотров, фильтры по логину и пути,
// анонимы (пустой login) ищутся отдельно от вошедших.
func TestPageViews(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	views := []store.View{
		{Login: "ivan", Path: "/studio", IP: "192.0.2.1", UA: "Firefox", Referer: "https://example.com/"},
		{Login: "ivan", Path: "/vault", IP: "192.0.2.1", UA: "Firefox"},
		{Path: "/", IP: "198.51.100.7", UA: "curl"},
	}
	for _, v := range views {
		if err := s.RecordView(ctx, v); err != nil {
			t.Fatalf("RecordView: %v", err)
		}
	}

	all, err := s.ListViews(ctx, store.ViewFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("ListViews: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("всего просмотров: %d, ожидалось 3", len(all))
	}
	if all[0].Path != "/" || all[0].Login != "" {
		t.Fatalf("сверху должен быть последний записанный, получено %+v", all[0])
	}
	if all[0].At == "" {
		t.Fatalf("у строки нет времени: %+v", all[0])
	}

	ivan, err := s.ListViews(ctx, store.ViewFilter{Login: "ivan"}, 10, 0)
	if err != nil {
		t.Fatalf("ListViews по логину: %v", err)
	}
	if len(ivan) != 2 {
		t.Fatalf("просмотров ivan: %d, ожидалось 2", len(ivan))
	}

	studio, err := s.ListViews(ctx, store.ViewFilter{Path: "stud"}, 10, 0)
	if err != nil {
		t.Fatalf("ListViews по пути: %v", err)
	}
	if len(studio) != 1 || studio[0].Path != "/studio" {
		t.Fatalf("фильтр по пути: %+v", studio)
	}

	if n, err := s.CountViews(ctx, store.ViewFilter{Login: "ivan"}); err != nil || n != 2 {
		t.Fatalf("CountViews ivan: %d, %v", n, err)
	}
	if n, err := s.CountViews(ctx, store.ViewFilter{}); err != nil || n != 3 {
		t.Fatalf("CountViews всего: %d, %v", n, err)
	}
	if n, err := s.CountViews(ctx, store.ViewFilter{Login: "никого"}); err != nil || n != 0 {
		t.Fatalf("CountViews никого: %d, %v", n, err)
	}

	if page, err := s.ListViews(ctx, store.ViewFilter{}, 0, 0); err != nil || len(page) != 0 {
		t.Fatalf("лимит 0: %d, %v", len(page), err)
	}
	if page, err := s.ListViews(ctx, store.ViewFilter{}, 2, 2); err != nil || len(page) != 1 {
		t.Fatalf("offset: %d, %v", len(page), err)
	}
}

// TestSessionTouch: мета активности заполняется один раз, повтор внутри
// минуты не перетирает адрес и браузер.
func TestSessionTouch(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "ivan", "хеш", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateSession(ctx, u.ID, "hash1", "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sess, err := s.SessionByTokenHash(ctx, "hash1")
	if err != nil {
		t.Fatalf("SessionByTokenHash: %v", err)
	}
	if sess.IP != "" || sess.UA != "" || sess.LastSeen != "" {
		t.Fatalf("у свежей сессии мета должна быть пустой: %+v", sess)
	}

	if err := s.TouchSession(ctx, "hash1", "192.0.2.1", "Firefox"); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}
	sess, err = s.SessionByTokenHash(ctx, "hash1")
	if err != nil {
		t.Fatalf("SessionByTokenHash: %v", err)
	}
	if sess.IP != "192.0.2.1" || sess.UA != "Firefox" || sess.LastSeen == "" {
		t.Fatalf("мета не проставилась: %+v", sess)
	}

	if err := s.TouchSession(ctx, "hash1", "198.51.100.7", "curl"); err != nil {
		t.Fatalf("TouchSession (повтор): %v", err)
	}
	sess, err = s.SessionByTokenHash(ctx, "hash1")
	if err != nil {
		t.Fatalf("SessionByTokenHash: %v", err)
	}
	if sess.IP != "192.0.2.1" || sess.UA != "Firefox" {
		t.Fatalf("повтор внутри минуты перётёр мету: %+v", sess)
	}

	if err := s.TouchSession(ctx, "нет-сессии", "192.0.2.1", "Firefox"); err != nil {
		t.Fatalf("TouchSession без сессии: %v", err)
	}
}

// TestLastSessionMeta: мета последней активности по пользователю;
// у аккаунта без сессий ErrNotFound, не пустая строка.
func TestLastSessionMeta(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "ivan", "хеш", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := s.LastSessionMeta(ctx, u.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("без сессий: %v, ожидался ErrNotFound", err)
	}

	if err := s.CreateSession(ctx, u.ID, "hash1", "csrf", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.TouchSession(ctx, "hash1", "192.0.2.1", "Firefox"); err != nil {
		t.Fatalf("TouchSession: %v", err)
	}

	meta, err := s.LastSessionMeta(ctx, u.ID)
	if err != nil {
		t.Fatalf("LastSessionMeta: %v", err)
	}
	if meta.IP != "192.0.2.1" || meta.UA != "Firefox" || meta.LastSeen == "" {
		t.Fatalf("мета: %+v", meta)
	}
}

// TestViewsAudienceAndSince: аудитория делит анонимов и вошедших,
// Since режет по времени, мусорные значения схлопываются в «всё».
func TestViewsAudienceAndSince(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, v := range []store.View{
		{Login: "ivan", Path: "/studio"},
		{Login: "ivan", Path: "/vault"},
		{Path: "/"},
	} {
		if err := s.RecordView(ctx, v); err != nil {
			t.Fatalf("RecordView: %v", err)
		}
	}

	audiences := []struct {
		audience string
		want     int
	}{
		{store.AudienceAll, 3},
		{store.AudienceUsers, 2},
		{store.AudienceAnonymous, 1},
		// мусор не совпадает ни с одной аудиторией: нормализация на обработчике
		{"мусор", 0},
	}
	for _, c := range audiences {
		filter := store.ViewFilter{Audience: c.audience}
		if n, err := s.CountViews(ctx, filter); err != nil || n != c.want {
			t.Fatalf("аудитория %q: %d, %v; ожидалось %d", c.audience, n, err, c.want)
		}
	}

	if n, err := s.CountViews(ctx, store.ViewFilter{Since: "2000-01-01 00:00:00"}); err != nil || n != 3 {
		t.Fatalf("since в прошлом: %d, %v", n, err)
	}
	if n, err := s.CountViews(ctx, store.ViewFilter{Since: "2999-01-01 00:00:00"}); err != nil || n != 0 {
		t.Fatalf("since в будущем: %d, %v", n, err)
	}
}

// TestTopPaths: страницы отсортированы по числу просмотров, лимит и фильтр работают.
func TestTopPaths(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, v := range []store.View{
		{Login: "ivan", Path: "/a"},
		{Login: "ivan", Path: "/a"},
		{Login: "ivan", Path: "/a"},
		{Path: "/a"},
		{Login: "ivan", Path: "/b"},
		{Path: "/c"},
		{Path: "/c"},
	} {
		if err := s.RecordView(ctx, v); err != nil {
			t.Fatalf("RecordView: %v", err)
		}
	}

	top, err := s.TopPaths(ctx, store.ViewFilter{}, 10)
	if err != nil {
		t.Fatalf("TopPaths: %v", err)
	}
	want := []store.PathCount{{Path: "/a", N: 4}, {Path: "/c", N: 2}, {Path: "/b", N: 1}}
	if len(top) != len(want) {
		t.Fatalf("топ: %+v", top)
	}
	for i, p := range want {
		if top[i] != p {
			t.Fatalf("топ[%d]: %+v, ожидалось %+v", i, top[i], p)
		}
	}

	limited, err := s.TopPaths(ctx, store.ViewFilter{}, 1)
	if err != nil || len(limited) != 1 || limited[0].Path != "/a" {
		t.Fatalf("лимит топа: %+v, %v", limited, err)
	}

	users, err := s.TopPaths(ctx, store.ViewFilter{Audience: store.AudienceUsers}, 10)
	if err != nil || len(users) != 2 || users[0].Path != "/a" || users[0].N != 3 {
		t.Fatalf("топ по вошедшим: %+v, %v", users, err)
	}
}

// TestPurgeViews: чистка удаляет только старые записи.
func TestPurgeViews(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for range 3 {
		if err := s.RecordView(ctx, store.View{Path: "/"}); err != nil {
			t.Fatalf("RecordView: %v", err)
		}
	}

	if n, err := s.PurgeViews(ctx, time.Now().Add(-time.Hour)); err != nil || n != 0 {
		t.Fatalf("чистка в прошлом: %d, %v", n, err)
	}
	if n, err := s.PurgeViews(ctx, time.Now().Add(time.Hour)); err != nil || n != 3 {
		t.Fatalf("чистка в будущем: %d, %v", n, err)
	}
	if n, err := s.CountViews(ctx, store.ViewFilter{}); err != nil || n != 0 {
		t.Fatalf("после чистки осталось: %d, %v", n, err)
	}
}
