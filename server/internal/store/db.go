// Package store - единое хранилище сайта: одна SQLite-база site.db,
// схема в schema.sql и весь SQL сервера.
//
// Раньше данные были размазаны по трём местам: файл visits.dat со счётчиком,
// auth.db у пакета auth и actions.db у логгера. Теперь всё живёт здесь:
// счётчик, пользователи, сессии, журнал действий и заявки с формы. Один
// файл - один бэкап, и между таблицами можно делать JOIN.
//
// Драйвер - modernc.org/sqlite, чистый Go без CGO: сборка и деплой
// остаются одним бинарником (CGO_ENABLED=0 в CI).
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	// Драйвер SQLite регистрирует себя в init(); именованный импорт нужен
	// ещё и для разбора кодов ошибок (isUniqueViolation).
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Store - соединение с базой. Все методы безопасны для параллельных
// запросов: за них отвечает database/sql, а база - WAL.
//
// В пакете нет ни одного net/http-типа: store знает про SQL и доменные
// данные, но не про запросы и ответы.
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
// момента cookie действует.
type Session struct {
	TokenHash string
	UserID    int64
	CSRF      string
	ExpiresAt time.Time
}

// ErrLoginTaken - логин уже занят (сработал UNIQUE по полю login).
var ErrLoginTaken = errors.New("login taken")

// ErrEmailTaken - почта уже занята другим аккаунтом (сработал частичный
// UNIQUE-индекс users_email_unique из schema.sql).
var ErrEmailTaken = errors.New("email taken")

// ErrNotFound — записи нет. Сентинел вместо sql.ErrNoRows: потребителям
// (auth, будущие Redis-адаптеры) не нужно импортировать database/sql
// ради ошибки «не найдено».
var ErrNotFound = errors.New("not found")

// noRows превращает sql.ErrNoRows в наш сентинел.
func noRows(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// isUniqueViolation узнаёт нарушение UNIQUE по коду ошибки драйвера, а не
// по тексту сообщения: текст может измениться с обновлением SQLite.
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
			se.Code() == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY ||
			se.Code() == sqlite3.SQLITE_CONSTRAINT
	}
	// Запасной путь, если драйвер завернул ошибку иначе.
	return strings.Contains(err.Error(), "UNIQUE")
}

// Open открывает (при необходимости создавая) базу в каталоге dataDir и
// прогоняет миграции. WAL — параллельное чтение не блокирует запись;
// busy_timeout — запрос ждёт чужую блокировку, а не падает.
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
	// Один поток соединений - рекомендованный режим для SQLite: без него
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

// Close закрывает соединение с базой.
func (s *Store) Close() error { return s.db.Close() }
