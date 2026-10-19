// Package sdnotify_test - протокол systemd против настоящего unixgram-сокета.
package sdnotify_test

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/sdnotify"
)

func TestReadySendsTheStateToTheSocket(t *testing.T) {
	addr := filepath.Join(t.TempDir(), "notify")
	ln, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		t.Fatalf("сокет: %v", err)
	}
	defer func() { _ = ln.Close() }()

	t.Setenv("NOTIFY_SOCKET", addr)
	if err := sdnotify.Ready(); err != nil {
		t.Fatalf("Ready: %v", err)
	}

	buf := make([]byte, 64)
	if err := ln.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("срок: %v", err)
	}
	n, _, err := ln.ReadFrom(buf)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if got := string(buf[:n]); got != "READY=1" {
		t.Fatalf("состояние %q, ждали READY=1", got)
	}
}

func TestReadyWithoutTheSocketDoesNothing(t *testing.T) {
	t.Setenv("NOTIFY_SOCKET", "")
	if err := sdnotify.Ready(); err != nil {
		t.Fatalf("Ready без сокета обязан молчать, получили %v", err)
	}
}
