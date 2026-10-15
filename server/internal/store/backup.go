package store

import (
	"database/sql"
	"fmt"
	"os"
)

// Snapshot снимает согласованную копию базы в dst через VACUUM INTO, живой сервис
// не останавливается; dst не должен существовать, чужой файл не перезаписывается.
func Snapshot(src, dst string) error {
	// Без файла SQLite аккуратно создаст пустой, и бэкап сойдётся к пустой копии.
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
