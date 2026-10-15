package store

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrate прогоняет migrations/*.sql, каждую в своей транзакции, и пишет sha256 файла в
// schema_migrations: применённый файл править нельзя, база новее бинарника гасит старт.
func (s *Store) migrate() error {
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_migrations (
  version    INTEGER PRIMARY KEY,
  sha256     TEXT    NOT NULL,
  applied_at TEXT    NOT NULL DEFAULT (datetime('now'))
);`); err != nil {
		return fmt.Errorf("таблица миграций: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("каталог миграций: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		version, err := migrationVersion(name)
		if err != nil {
			return err
		}

		body, err := fs.ReadFile(migrationsFS, path.Join("migrations", name))
		if err != nil {
			return fmt.Errorf("миграция %s: %w", name, err)
		}
		sum := sha256.Sum256(body)

		var appliedSum string
		err = s.db.QueryRow(
			`SELECT sha256 FROM schema_migrations WHERE version = $1`, version,
		).Scan(&appliedSum)
		switch {
		case err == nil:
			if appliedSum != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("миграция %s изменена после применения, правьте только новым файлом", name)
			}
			continue
		case errors.Is(err, sql.ErrNoRows):
			// Ещё не применена, накатываем ниже.
		default:
			return fmt.Errorf("версия миграций: %w", err)
		}

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("миграция %s: %w", name, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("миграция %s: %w", name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (version, sha256) VALUES ($1, $2)`,
			version, hex.EncodeToString(sum[:]),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("миграция %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("миграция %s: %w", name, err)
		}
	}

	// База из будущего: продолжать - молча ломать данные, старт падает явно.
	var maxApplied, maxKnown int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&maxApplied); err != nil {
		return fmt.Errorf("версия миграций: %w", err)
	}
	if len(names) > 0 {
		if maxKnown, err = migrationVersion(names[len(names)-1]); err != nil {
			return err
		}
	}
	if maxApplied > maxKnown {
		return fmt.Errorf("база знает миграции до %d, бинарник только до %d: откат невозможен, нужен бэкап и ручной разбор", maxApplied, maxKnown)
	}
	return nil
}

// migrationVersion достаёт номер из имени файла: "0002_leads.sql" → 2.
// Четыре цифры обязательны: без ведущих нулей 10_x.sql идёт раньше 2_y.sql.
func migrationVersion(name string) (int, error) {
	num, rest, ok := strings.Cut(name, "_")
	if !ok || rest == "" || !strings.HasSuffix(name, ".sql") {
		return 0, fmt.Errorf("имя миграции %q: ожидается NNNN_описание.sql", name)
	}
	if len(num) != 4 {
		return 0, fmt.Errorf("имя миграции %q: ожидается версия из четырёх цифр с ведущими нулями", name)
	}
	version, err := strconv.Atoi(num)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("имя миграции %q: версия должна быть положительным числом", name)
	}
	return version, nil
}
