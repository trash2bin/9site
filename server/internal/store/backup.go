package store

import (
	"database/sql"
	"fmt"
	"os"
)

// Snapshot снимает согласованную копию базы в указанный файл — для
// резервной копии перед деплоем. Работает через VACUUM INTO: SQLite сама
// дожидается чужих транзакций и переносит данные вместе с содержимым WAL,
// поэтому снимок снимается и на работающем сервисе — без остановки и без
// ручного копирования спутников *.db-wal/*.db-shm (их копировать отдельно
// нельзя: получится рассинхронизированная база).
//
// dst не должен существовать: VACUUM INTO чужой файл не перезаписывает —
// случайно затереть вчерашнюю копию невозможно.
func Snapshot(src, dst string) error {
	// Отдельная проверка нужна, чтобы не «снять копию» пустой базы: если
	// файла нет, SQLite аккуратно создаст новый пустой, и бэкап сойдётся.
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("база %s: %w", src, err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", src)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("%s: %w", src, err)
	}
	defer func() { _ = db.Close() }()

	if _, err := db.Exec(`VACUUM INTO ?`, dst); err != nil {
		return fmt.Errorf("снимок %s: %w", dst, err)
	}
	return nil
}
