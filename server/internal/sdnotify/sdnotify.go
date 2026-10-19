// Package sdnotify - несколько строк протокола systemd: бинарник сам говорит
// менеджеру, когда слушатель открыт. Без него менеджер считает Type=simple
// запущенным процессом и проверяет порт в тот момент, когда сервер ещё не
// слушает, поэтому Type=notify стал возможен только отсюда.
package sdnotify

import (
	"net"
	"os"
)

// Ready сообщает менеджеру, что сервис поднялся и слушатель открыт.
// Без NOTIFY_SOCKET (запуск вручную, тесты) вызов ничего не делает.
func Ready() error {
	return send("READY=1")
}

func send(state string) error {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return nil
	}
	conn, err := net.Dial("unixgram", addr)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte(state))
	return err
}
