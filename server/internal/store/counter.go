package store

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Hit увеличивает счётчик визитов на единицу и возвращает новое значение.
// Один атомарный UPDATE по единственной строке: без мьютексов и без
// файлов — блокировку строки SQLite берёт на себя.
func (s *Store) Hit(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`UPDATE visits SET n = n + 1 WHERE id = 1 RETURNING n`,
	).Scan(&n); err != nil {
		return 0, noRows(err)
	}
	return n, nil
}

// Visits возвращает текущее число визитов — пригодится админке.
func (s *Store) Visits(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT n FROM visits WHERE id = 1`).Scan(&n); err != nil {
		return 0, noRows(err)
	}
	return n, nil
}

// setVisits задаёт счётчик — нужно только при импорте старого visits.dat.
func (s *Store) setVisits(ctx context.Context, n int) error {
	_, err := s.db.ExecContext(ctx, `UPDATE visits SET n = $1 WHERE id = 1`, n)
	return err
}

// ImportVisits переносит счётчик из старого файла visits.dat в базу —
// единственное, что реально существует на проде из прежних хранилищ.
//
// Вызывается на каждом старте и безвреден без файла: старые данные
// переименовываются в *.bak сразу после вливки, поэтому повторные запуски
// ничего не находят. Файл убирается в .bak в любом случае — импорт должен
// сойтись к нулю различий; непрочитанный файл при этом не теряется молча:
// ошибка возвращается, а значение остаётся в .bak и восстановимо руками.
// Возвращает true, если значение реально переносилось.
func (s *Store) ImportVisits(ctx context.Context, visitsFile string) (bool, error) {
	if visitsFile == "" || !fileExists(visitsFile) {
		return false, nil
	}

	imported := false
	n, readErr := readVisitsFile(visitsFile)
	if readErr == nil {
		var cur int
		if err := s.db.QueryRowContext(ctx, `SELECT n FROM visits WHERE id = 1`).Scan(&cur); err != nil {
			return false, fmt.Errorf("импорт счётчика: %w", err)
		}
		if cur == 0 {
			if err := s.setVisits(ctx, n); err != nil {
				return false, fmt.Errorf("импорт счётчика: %w", err)
			}
			imported = true
		}
	}
	if err := os.Rename(visitsFile, visitsFile+".bak"); err != nil {
		return false, fmt.Errorf("импорт счётчика: %s не переименован в .bak: %w", visitsFile, err)
	}
	if readErr != nil {
		return false, fmt.Errorf("импорт счётчика %s не перенесён: %w", visitsFile, readErr)
	}
	return imported, nil
}

// fileExists сообщает, лежит ли файл на диске.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// readVisitsFile читает старый файл со счётчиком.
func readVisitsFile(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s: не число", path)
	}
	return n, nil
}
