package auth

import (
	"database/sql"
	"fmt"

	// Чистый Go-драйвер SQLite: работает без CGO, поэтому сборка и деплой
	// остаются одним бинарником (CGO_ENABLED=0 в CI).
	_ "modernc.org/sqlite"
)

// openDB открывает базу и включает нужные PRAGMA.
func openDB(path string) (*sql.DB, error) {
	// busy_timeout — пауза при блокировке базы другим запросом;
	// foreign_keys — каскадное удаление сессий и токенов вместе с пользователем.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return db, nil
}

// migrate создаёт таблицы. Выполняется при каждом старте: IF NOT EXISTS
// делает запуск идемпотентным.
func (s *Server) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS users (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  login      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
  pass_hash  TEXT    NOT NULL,
  full_name  TEXT    NOT NULL DEFAULT '',
  email      TEXT    NOT NULL DEFAULT '',
  avatar     TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf       TEXT    NOT NULL,
  created_at TEXT    NOT NULL DEFAULT (datetime('now')),
  expires_at TEXT    NOT NULL
);

CREATE TABLE IF NOT EXISTS reset_tokens (
  token_hash TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT    NOT NULL,
  used       INTEGER NOT NULL DEFAULT 0
);
`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("схема auth-базы: %w", err)
	}
	return nil
}
