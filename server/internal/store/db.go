// Package store - единое хранилище сайта: одна SQLite-база site.db, схема в
// migrations/ и весь SQL сервера: счётчик, пользователи, сессии, журнал, заявки.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	// Драйвер регистрирует себя в init(), lib даёт коды ошибок SQLite для isUniqueViolation.
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Store - соединение с базой; методы безопасны для параллельных запросов
// (database/sql, база в WAL).
type Store struct {
	db *sql.DB
}

// User - запись таблицы users.
type User struct {
	ID       int64
	Login    string
	PassHash string
	FullName string
	Email    string
	Avatar   string
}

// Session - активная сессия: кто вошёл, чем подписаны формы и до какого
// срока cookie действует.
type Session struct {
	TokenHash string
	UserID    int64
	CSRF      string
	ExpiresAt time.Time
}

// ErrLoginTaken - логин уже занят (UNIQUE по login).
var ErrLoginTaken = errors.New("login taken")

// ErrEmailTaken - почта занята другим аккаунтом (частичный UNIQUE
// users_email_unique).
var ErrEmailTaken = errors.New("email taken")

// ErrNotFound - записи нет; сентинел вместо sql.ErrNoRows, чтобы не тянуть
// database/sql ради ошибки «не найдено».
var ErrNotFound = errors.New("not found")

func noRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// isUniqueViolation - нарушение UNIQUE по кодам драйвера, а не по тексту:
// текст ошибок SQLite меняется между версиями, CHECK-нарушение не считаем.
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
			se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}

// Open открывает (создавая при необходимости) базу в dataDir и прогоняет миграции:
// WAL не блокирует чтение, busy_timeout даёт ждать чужую блокировку, а не падать.
func Open(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("%s: %w", dataDir, err)
	}

	path := filepath.Join(dataDir, "site.db")
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"+
			"&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)",
		path,
	)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// Один поток соединений, рекомендованный режим SQLite: без него
	// конкурирующие записи получают SQLITE_BUSY несмотря на busy_timeout.
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
