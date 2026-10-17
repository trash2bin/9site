package store

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
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
// schema_migrations: применённый файл править нельзя.
//
// База новее бинарника - это штатный откат деплоя, а не авария: прошлый
// бинарник обязан открыть базу, которую мигратил новый, и работать на ней.
// Иначе авто-роллбэк возвращает бинарник, получает exit 1 на store.Open, и
// цикл Restart=on-failure держит сайт в 502 до ручной правки сервера.
// Чужие версии пропускаем: их SQL в нашей сборке нет.
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

	applied, err := s.appliedVersions()
	if err != nil {
		return err
	}

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

		if appliedVersion(applied, version) {
			// Уже накачена этим же файлом: sha сверяем ниже, повторно не гоним.
			var appliedSum string
			err = s.db.QueryRow(
				`SELECT sha256 FROM schema_migrations WHERE version = $1`, version,
			).Scan(&appliedSum)
			if err != nil {
				return fmt.Errorf("версия миграций: %w", err)
			}
			if appliedSum != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("миграция %s изменена после применения, правьте только новым файлом", name)
			}
			continue
		}

		if futureApplied(applied, version) {
			// База уже новее файла: в нашей сборке его SQL нет, пропускаем,
			// иначе откат бинарника не сможет стартовать.
			continue
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

	return nil
}

// appliedVersions уже накаченные в базу версии миграций.
func (s *Store) appliedVersions() ([]int, error) {
	rows, err := s.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("версия миграций: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]int, 0, 4)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("версия миграций: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// appliedVersion есть ли ровно эта версия среди применённых.
func appliedVersion(applied []int, version int) bool {
	for _, v := range applied {
		if v == version {
			return true
		}
	}
	return false
}

// futureApplied применил ли кто-то версию выше этой: значит база из будущего
// для этого бинарника, файл трогать нельзя.
func futureApplied(applied []int, version int) bool {
	for _, v := range applied {
		if v > version {
			return true
		}
	}
	return false
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
