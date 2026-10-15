-- 0001: схема сайта. Пользователи, сессии и токены сброса перенесены без
-- изменений из старой auth.db (деплой её не знал — локальные данные),
-- журнал действий — из logging/logging.go, счётчик — из visits.dat.

CREATE TABLE IF NOT EXISTS users (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  login      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
  pass_hash  TEXT    NOT NULL,
  full_name  TEXT    NOT NULL DEFAULT '',
  email      TEXT    NOT NULL DEFAULT '',
  avatar     TEXT    NOT NULL DEFAULT '',
  created_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Почта не должна дублироваться: второй аккаунт с тем же адресом означал
-- бы, что письмо со сбросом пароля уходит не тому человеку (UserByEmail
-- вернул бы произвольную строку). Регистр не важен — COLLATE NOCASE.
-- Частичный индекс: пустые адреса (пользователи без почты) дублироваться
-- могут.
CREATE UNIQUE INDEX IF NOT EXISTS users_email_unique
  ON users (email COLLATE NOCASE) WHERE email != '';

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

CREATE TABLE IF NOT EXISTS actions (
  id     INTEGER PRIMARY KEY AUTOINCREMENT,
  at     TEXT NOT NULL DEFAULT (datetime('now')),
  login  TEXT NOT NULL,
  action TEXT NOT NULL,
  ip     TEXT    NOT NULL
);

-- Счётчик визитов: одна строка с фиксированным id, растёт атомарным
-- UPDATE ... RETURNING. Файл visits.dat больше не участвует.
CREATE TABLE IF NOT EXISTS visits (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  n  INTEGER NOT NULL DEFAULT 0
);

INSERT INTO visits (id, n) VALUES (1, 0);
