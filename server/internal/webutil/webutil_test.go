package webutil

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestClientIP — реальный адрес клиента за nginx. XFF целиком брать
// нельзя: клиент подставляет себе префикс и получает сколько угодно
// «адресов» для обхода лимита.
func TestClientIP(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		hdr    map[string]string
		want   string
	}{
		{
			name:   "без заголовков — из RemoteAddr",
			remote: "192.0.2.1:1234",
			want:   "192.0.2.1",
		},
		{
			name:   "X-Real-IP от nginx",
			remote: "127.0.0.1:8080",
			hdr:    map[string]string{"X-Real-IP": "198.51.100.7"},
			want:   "198.51.100.7",
		},
		{
			name:   "XFF: подделанный префикс клиента не работает, берётся последний адрес",
			remote: "127.0.0.1:8080",
			hdr: map[string]string{
				"X-Forwarded-For": "6.6.6.6, 198.51.100.7",
			},
			want: "198.51.100.7",
		},
		{
			name:   "X-Real-IP важнее XFF",
			remote: "127.0.0.1:8080",
			hdr: map[string]string{
				"X-Real-IP":       "198.51.100.7",
				"X-Forwarded-For": "6.6.6.6",
			},
			want: "198.51.100.7",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			for k, v := range c.hdr {
				r.Header.Set(k, v)
			}
			if got := ClientIP(r); got != c.want {
				t.Fatalf("ClientIP = %q, ожидалось %q", got, c.want)
			}
		})
	}
}

// TestLimiter — не больше limit действий за окно, протухшее забывается.
func TestLimiter(t *testing.T) {
	l := NewLimiter(3, 50*time.Millisecond)

	for i := 0; i < 3; i++ {
		if l.Throttle("a") {
			t.Fatalf("попытка %d не должна быть в лимите", i+1)
		}
	}
	if !l.Throttle("a") {
		t.Fatal("4-я попытка должна быть в лимите")
	}

	// Другой адрес не страдает.
	if l.Throttle("b") {
		t.Fatal("адрес b не должен зависеть от a")
	}

	// Окно протухло — попытки снова разрешены.
	time.Sleep(60 * time.Millisecond)
	if l.Throttle("a") {
		t.Fatal("после протухания окна лимит должен сброситься")
	}
}
