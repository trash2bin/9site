// Package storetest — тесты хранилища, вынесенные в подкаталог, чтобы
// ls store/ показывал только рабочие файлы. Всё нужное из пакета —
// экспортный API; исключение — проверки внутренних таблиц
// (schema_migrations), они открывают базу напрямую через rawDB.
//
// В Go это отступление от идиомы (обычно *_test.go лежит рядом с кодом):
// вынос стоит того, только пока тесты остаются чёрным ящиком по
// публичному API.
package storetest

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// rawDB открывает базу напрямую, мимо хранилища: внешнему тестовому
// пакету нужны проверки внутренних таблиц (schema_migrations), а второй
// соедин в режиме WAL спокойно соседствует с открытым Store.
func rawDB(t *testing.T, dir string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dir, "site.db"))
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		t.Fatalf("busy_timeout: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// --- миграции ---

// TestMigrateIdempotent: повторный старт не накатывает те же миграции
// заново, а проверки схемы (уникальность почты, строка счётчика)
// переживают перезапуск.
func TestMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := store.Open(dir)
	if err != nil {
		t.Fatalf("повторный Open: %v", err)
	}
	defer func() { _ = s2.Close() }()

	db := rawDB(t, dir)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations: %v", err)
	}
	if count < 2 {
		t.Fatalf("ожидались 2 применённые миграции, получили %d", count)
	}

	// Строка счётчика одна — повторная вставка не задублировалась.
	var visits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM visits`).Scan(&visits); err != nil || visits != 1 {
		t.Fatalf("строка счётчика должна быть одна: %v, %d", err, visits)
	}

	// Уникальность почты пережила перезапуск (индекс на месте).
	ctx := context.Background()
	if _, err := s2.CreateUser(ctx, "первый", "хэш", "", "a@b.ru"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := s2.CreateUser(ctx, "второй", "хэш", "", "a@b.ru"); !errors.Is(err, store.ErrEmailTaken) {
		t.Fatalf("после перезапуска индекс почты не работает: %v", err)
	}
}

func TestMigrateDetectsEditedFile(t *testing.T) {
	dir := t.TempDir()

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Симулируем правку применённой миграции: чужая контрольная сумма.
	if _, err := rawDB(t, dir).Exec(`UPDATE schema_migrations SET sha256 = 'deadbeef' WHERE version = 1`); err != nil {
		t.Fatalf("подмена sha: %v", err)
	}

	if _, err := store.Open(dir); err == nil || !strings.Contains(err.Error(), "изменена после применения") {
		t.Fatalf("ожидалась ошибка о правке миграции, получили: %v", err)
	}
}

func TestMigrateRejectsFutureSchema(t *testing.T) {
	dir := t.TempDir()

	s, err := store.Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Симулируем откат деплоя: в базе версия, о которой бинарник не знает.
	if _, err := rawDB(t, dir).Exec(`INSERT INTO schema_migrations (version, sha256) VALUES (999, 'x')`); err != nil {
		t.Fatalf("вставка будущей версии: %v", err)
	}

	if _, err := store.Open(dir); err == nil || !strings.Contains(err.Error(), "откат невозможен") {
		t.Fatalf("ожидалась ошибка о будущей версии, получили: %v", err)
	}
}

// --- счётчик ---

func TestCounter(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for want := 1; want <= 3; want++ {
		got, err := s.Hit(ctx)
		if err != nil {
			t.Fatalf("Hit: %v", err)
		}
		if got != want {
			t.Fatalf("Hit вернул %d, ожидалось %d", got, want)
		}
	}

	n, err := s.Visits(ctx)
	if err != nil {
		t.Fatalf("Visits: %v", err)
	}
	if n != 3 {
		t.Fatalf("Visits = %d, ожидалось 3", n)
	}
}

// --- пользователи ---

func TestUsers(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "ivan", "hash", "Иван", "i@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	// Повторный логин — store.ErrLoginTaken.
	if _, err := s.CreateUser(ctx, "IVAN", "hash", "", ""); !errors.Is(err, store.ErrLoginTaken) {
		t.Fatalf("ожидался ErrLoginTaken, получили: %v", err)
	}

	byLogin, err := s.UserByLogin(ctx, "ivan")
	if err != nil || byLogin.ID != u.ID {
		t.Fatalf("UserByLogin: %v %+v", err, byLogin)
	}
	if _, err := s.UserByLogin(ctx, "nope"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ожидался ErrNotFound, получили: %v", err)
	}
	if _, err := s.UserByID(ctx, u.ID); err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if _, err := s.UserByEmail(ctx, "I@EXAMPLE.COM"); err != nil {
		t.Fatalf("UserByEmail (регистр): %v", err)
	}

	if err := s.UpdateProfile(ctx, u.ID, "Иван И.", "new@example.com"); err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if err := s.UpdatePassword(ctx, u.ID, "hash2"); err != nil {
		t.Fatalf("UpdatePassword: %v", err)
	}
	if err := s.UpdateAvatar(ctx, u.ID, "u1-abc.png"); err != nil {
		t.Fatalf("UpdateAvatar: %v", err)
	}

	got, err := s.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatalf("UserByID: %v", err)
	}
	if got.FullName != "Иван И." || got.Email != "new@example.com" || got.Avatar != "u1-abc.png" || got.PassHash != "hash2" {
		t.Fatalf("профиль не обновился: %+v", got)
	}
}

// --- сессии и токены сброса ---

func TestSessions(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "ivan", "hash", "", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	expires := time.Now().Add(time.Hour)
	if err := s.CreateSession(ctx, u.ID, "hash1", "csrf1", expires); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sess, err := s.SessionByTokenHash(ctx, "hash1")
	if err != nil {
		t.Fatalf("SessionByTokenHash: %v", err)
	}
	if sess.UserID != u.ID || sess.CSRF != "csrf1" {
		t.Fatalf("сессия не та: %+v", sess)
	}
	if !sess.ExpiresAt.Equal(expires.UTC().Truncate(time.Second)) {
		t.Fatalf("ExpiresAt %v, ожидалось %v", sess.ExpiresAt, expires.UTC())
	}

	if _, err := s.SessionByTokenHash(ctx, "нет"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("ожидался ErrNotFound, получили: %v", err)
	}

	// Протухшие чистятся, свежие — нет.
	if err := s.CreateSession(ctx, u.ID, "старая", "csrf", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("CreateSession (старая): %v", err)
	}
	removed, err := s.PurgeExpiredSessions(ctx, time.Now())
	if err != nil || removed != 1 {
		t.Fatalf("PurgeExpiredSessions: %v, удалилось %d", err, removed)
	}
	if _, err := s.SessionByTokenHash(ctx, "hash1"); err != nil {
		t.Fatalf("свежая сессия не должна удаляться: %v", err)
	}

	// Смена пароля гасит все сессии пользователя.
	if err := s.CreateSession(ctx, u.ID, "другая", "csrf", expires); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := s.DeleteUserSessions(ctx, u.ID); err != nil {
		t.Fatalf("DeleteUserSessions: %v", err)
	}
	if _, err := s.SessionByTokenHash(ctx, "hash1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("после отзыва сессия должна быть не найдена: %v", err)
	}
}

func TestResetTokenSingleUse(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "ivan", "hash", "", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if err := s.CreateResetToken(ctx, u.ID, "rt1", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}

	userID, err := s.ConsumeResetToken(ctx, "rt1")
	if err != nil || userID != u.ID {
		t.Fatalf("ConsumeResetToken: %v, userID=%d", err, userID)
	}

	// Повторный переход по той же ссылке не работает.
	if _, err := s.ConsumeResetToken(ctx, "rt1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("повторное использование должно вернуть ErrNotFound: %v", err)
	}

	// Просроченный токен тоже не работает.
	if err := s.CreateResetToken(ctx, u.ID, "просроченный", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}
	if _, err := s.ConsumeResetToken(ctx, "просроченный"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("просроченный токен должен быть не найден: %v", err)
	}
}

// --- журнал и заявки ---

func TestActionsAndLeads(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if err := s.Record(ctx, store.Action{Login: "ivan", Action: "вход", IP: "127.0.0.1"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	entries, err := s.Recent(ctx, 10)
	if err != nil || len(entries) != 1 || entries[0].Action != "вход" {
		t.Fatalf("Recent: %v %+v", err, entries)
	}

	if err := s.SaveLead(ctx, store.Lead{Name: "Иван", Phone: "+70000000000", Email: "i@example.com", IP: "127.0.0.1"}); err != nil {
		t.Fatalf("SaveLead: %v", err)
	}
	leads, err := s.ListLeads(ctx, 10)
	if err != nil || len(leads) != 1 || leads[0].Name != "Иван" {
		t.Fatalf("ListLeads: %v %+v", err, leads)
	}
}

// --- импорт счётчика из старого visits.dat ---

// TestImportVisits — счётчик переносится один раз, дальше файл .bak и
// повторные старты ничего не делают; растущий счётчик не затирается.
func TestImportVisits(t *testing.T) {
	dir := t.TempDir()
	visits := filepath.Join(dir, "visits.dat")
	if err := os.WriteFile(visits, []byte("77\n"), 0o644); err != nil {
		t.Fatalf("visits.dat: %v", err)
	}

	s := testStore(t)
	ctx := context.Background()

	imported, err := s.ImportVisits(ctx, visits)
	if err != nil || !imported {
		t.Fatalf("ImportVisits: %v, imported=%v", err, imported)
	}
	if n, err := s.Visits(ctx); err != nil || n != 77 {
		t.Fatalf("Visits: %v, ожидалось 77, получили %d", err, n)
	}
	// Файл убран в .bak, но не удалён.
	if _, err := os.Stat(visits); !os.IsNotExist(err) {
		t.Fatal("visits.dat должен быть переименован")
	}
	if _, err := os.Stat(visits + ".bak"); err != nil {
		t.Fatalf(".bak должен существовать: %v", err)
	}

	// Повторный старт: файла уже нет — no-op.
	imported, err = s.ImportVisits(ctx, visits)
	if err != nil || imported {
		t.Fatalf("повторный импорт должен быть no-op: %v, imported=%v", err, imported)
	}

	// Счётчик уже растёт (77 + 1 визит) — старое значение 77 из файла
	// не должно затирать его.
	if _, err := s.Hit(ctx); err != nil {
		t.Fatalf("Hit: %v", err)
	}
	visits2 := filepath.Join(dir, "visits.dat")
	if err := os.WriteFile(visits2, []byte("77"), 0o644); err != nil {
		t.Fatalf("visits.dat: %v", err)
	}
	if imported, err := s.ImportVisits(ctx, visits2); err != nil || imported {
		t.Fatalf("импорт при непустом счётчике: %v, imported=%v", err, imported)
	}
	if n, _ := s.Visits(ctx); n != 78 {
		t.Fatalf("растущий счётчик должен остаться 78, получили %d", n)
	}
}

// TestImportVisitsReportsGarbage — мусорный visits.dat не теряется молча:
// файл уходит в .bak (импорт сходится к нулю различий), но ошибка
// возвращается — оператор видит её в логе один раз на старте.
func TestImportVisitsReportsGarbage(t *testing.T) {
	dir := t.TempDir()
	visits := filepath.Join(dir, "visits.dat")
	if err := os.WriteFile(visits, []byte("не число"), 0o644); err != nil {
		t.Fatalf("visits.dat: %v", err)
	}

	s := testStore(t)
	if _, err := s.ImportVisits(context.Background(), visits); err == nil {
		t.Fatal("мусорный visits.dat должен давать ошибку, а не теряться молча")
	}
	if _, err := os.Stat(visits); !os.IsNotExist(err) {
		t.Fatal("файл должен быть убран в .bak, чтобы импорт не повторялся")
	}
	if _, err := os.Stat(visits + ".bak"); err != nil {
		t.Fatalf(".bak должен существовать (значение восстановимо руками): %v", err)
	}
	// Повторный старт — уже без файла — чисто закрывается.
	if _, err := s.ImportVisits(context.Background(), visits); err != nil {
		t.Fatalf("повторный импорт: %v", err)
	}
}

// TestConsumeResetTokenParallel — два параллельных перехода по одной
// ссылке: ровно один успех, второй — store.ErrNotFound.
func TestConsumeResetTokenParallel(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "ivan", "hash", "", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.CreateResetToken(ctx, u.ID, "rt", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateResetToken: %v", err)
	}

	const attempts = 2
	results := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		go func() {
			_, err := s.ConsumeResetToken(ctx, "rt")
			results <- err
		}()
	}

	var ok, denied int
	for i := 0; i < attempts; i++ {
		switch err := <-results; {
		case err == nil:
			ok++
		case errors.Is(err, store.ErrNotFound):
			denied++
		default:
			t.Fatalf("неожиданная ошибка: %v", err)
		}
	}
	if ok != 1 || denied != 1 {
		t.Fatalf("успехов %d, отказов %d — ждали 1 и 1", ok, denied)
	}
}

// --- почта не должна дублироваться (баг: у email не было UNIQUE) ---

// TestCreateUserRejectsDuplicateEmail: без индекса второй аккаунт с тем же
// адресом заводился молча, и UserByEmail возвращал произвольную строку —
// письмо со сбросом пароля уходило не тому человеку. Регистр не важен.
func TestCreateUserRejectsDuplicateEmail(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "первый", "хэш", "", "a@b.ru"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := s.CreateUser(ctx, "второй", "хэш", "", "A@B.Ru"); !errors.Is(err, store.ErrEmailTaken) {
		t.Fatalf("дубль почты должен давать ErrEmailTaken, получили: %v", err)
	}

	// Пустая почта (легаси-пользователи) дублироваться может — индекс
	// частичный, незанятые адреса не мешают.
	if _, err := s.CreateUser(ctx, "безпочты1", "хэш", "", ""); err != nil {
		t.Fatalf("пустая почта: %v", err)
	}
	if _, err := s.CreateUser(ctx, "безпочты2", "хэш", "", ""); err != nil {
		t.Fatalf("пустая почта (второй): %v", err)
	}
}

// TestUpdateProfileRejectsDuplicateEmail: смена почты в профиле обходитает
// ту же проверку — иначе профиль незаметно крадёт чужой адрес для сброса.
func TestUpdateProfileRejectsDuplicateEmail(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.CreateUser(ctx, "первый", "хэш", "", "a@b.ru"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	u2, err := s.CreateUser(ctx, "второй", "хэш", "", "other@b.ru")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := s.UpdateProfile(ctx, u2.ID, "", "a@b.ru"); !errors.Is(err, store.ErrEmailTaken) {
		t.Fatalf("смена на занятую почту должна давать ErrEmailTaken, получили: %v", err)
	}
	// Своё же по прежнему можно (смена имени без смены почты).
	if err := s.UpdateProfile(ctx, u2.ID, "Имя", "other@b.ru"); err != nil {
		t.Fatalf("обновление без смены почты: %v", err)
	}
}
