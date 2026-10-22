package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// TestUserRole: новый пользователь обычный, роль меняется по логину,
// неизвестный логин это ErrNotFound.
func TestUserRole(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	user, err := s.CreateUser(ctx, "reader", "hash", "Читатель", "reader@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.Role != store.RoleUser || user.IsAdmin() {
		t.Fatalf("новая роль %q, ожидалась %q", user.Role, store.RoleUser)
	}

	if err := s.SetUserRole(ctx, "reader", store.RoleAdmin); err != nil {
		t.Fatalf("SetUserRole: %v", err)
	}
	if err := s.SetUserRole(ctx, "nobody", store.RoleAdmin); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("неизвестный логин: %v, ожидался ErrNotFound", err)
	}

	// Чтение по логину и по id отдаёт ту же роль.
	for _, get := range []func() (*store.User, error){
		func() (*store.User, error) { return s.UserByLogin(ctx, "reader") },
		func() (*store.User, error) { return s.UserByID(ctx, user.ID) },
	} {
		got, err := get()
		if err != nil {
			t.Fatal(err)
		}
		if !got.IsAdmin() {
			t.Fatalf("роль %q после повышения", got.Role)
		}
	}

	if n, err := s.AdminCount(ctx); err != nil || n != 1 {
		t.Fatalf("админов %d (%v), ожидался 1", n, err)
	}

	total, err := s.CountUsers(ctx)
	if err != nil || total != 1 {
		t.Fatalf("всего аккаунтов %d (%v), ожидался 1", total, err)
	}

	users, err := s.ListUsers(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || users[0].Login != "reader" {
		t.Fatalf("список %+v", users)
	}
}

// TestEnsureAdmin: первый запуск заводит админа, второй обновляет роль и
// пароль, а обычный пользователь с тем же логином становится админом.
func TestEnsureAdmin(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	created, err := s.EnsureAdmin(ctx, "owner", "first-hash")
	if err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if !created {
		t.Fatal("первый вызов должен создавать аккаунт")
	}

	owner, err := s.UserByLogin(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if !owner.IsAdmin() || owner.PassHash != "first-hash" {
		t.Fatalf("админ из окружения: %+v", owner)
	}

	created, err = s.EnsureAdmin(ctx, "owner", "second-hash")
	if err != nil {
		t.Fatalf("повторный EnsureAdmin: %v", err)
	}
	if created {
		t.Fatal("второй вызов не должен создавать дубль")
	}
	owner, err = s.UserByLogin(ctx, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if owner.PassHash != "second-hash" {
		t.Fatalf("пароль не обновился: %q", owner.PassHash)
	}

	// Существующий обычный пользователь: повышается, новые строки не плодятся.
	if _, err := s.CreateUser(ctx, "guest1", "hash", "", "guest@example.com"); err != nil {
		t.Fatal(err)
	}
	if created, err := s.EnsureAdmin(ctx, "guest1", "hash2"); err != nil || created {
		t.Fatalf("повышение обычного юзера: created=%v err=%v", created, err)
	}
	guest, err := s.UserByLogin(ctx, "guest1")
	if err != nil {
		t.Fatal(err)
	}
	if !guest.IsAdmin() || guest.Email != "guest@example.com" {
		t.Fatalf("после повышения: %+v", guest)
	}
}
