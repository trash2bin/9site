package auth

// Тесты почтовых пресетов: провайдер подставляет хост/порт, кривой конфиг
// ловится на старте, а не при первом сбросе пароля.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// openTestStore открывает реальную SQLite-базу во временном каталоге.
func openTestStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// serverWithMail поднимает модуль с заданным SMTP-конфигом.
func serverWithMail(t *testing.T, smtp SMTPConfig) (*Server, error) {
	t.Helper()

	dir := t.TempDir()
	st := openTestStore(t, dir)

	return New(Config{
		Users:      st,
		Sessions:   st,
		AvatarsDir: filepath.Join(dir, "avatars"),
		OutboxDir:  filepath.Join(dir, "outbox"),
		Throttle:   testThrottle(t, 1000),
		SMTP:       smtp,
	})
}

func TestSMTPPreset(t *testing.T) {
	s, err := serverWithMail(t, SMTPConfig{
		Provider: "gmail",
		Username: "me@gmail.com",
		Password: "app-password",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if s.cfg.SMTP.Host != "smtp.gmail.com" || s.cfg.SMTP.Port != 587 {
		t.Fatalf("gmail-пресет дал %s:%d", s.cfg.SMTP.Host, s.cfg.SMTP.Port)
	}
	// При настроенном SMTP почтовый тракт — outbox-копия + настоящая отправка.
	if _, ok := s.mailer.(outboxMailer); !ok {
		t.Fatalf("ожидался outboxMailer, получили %T", s.mailer)
	}

	// Регистр имени провайдера не важен.
	if _, err := serverWithMail(t, SMTPConfig{
		Provider: "YANDEX",
		Username: "me@yandex.ru",
		Password: "app-password",
	}); err != nil {
		t.Fatalf("New (YANDEX): %v", err)
	}
}

func TestSMTPConfigErrors(t *testing.T) {
	cases := []struct {
		name string
		cfg  SMTPConfig
		want string
	}{
		{
			name: "провайдер вместе с хостом",
			cfg:  SMTPConfig{Provider: "gmail", Host: "smtp.example.com"},
			want: "оставь что-то одно",
		},
		{
			name: "неизвестный провайдер",
			cfg:  SMTPConfig{Provider: "rambler", Username: "a", Password: "b"},
			want: "неизвестный провайдер",
		},
		{
			name: "провайдер без логина-пароля",
			cfg:  SMTPConfig{Provider: "gmail"},
			want: "не заполнены SMTP_USER и SMTP_PASS",
		},
		{
			name: "пароль без логина",
			cfg:  SMTPConfig{Host: "smtp.example.com", Port: 587, Password: "b"},
			want: "задаются вместе",
		},
		{
			name: "логин без сервера",
			cfg:  SMTPConfig{Username: "a", Password: "b"},
			want: "не настроен сервер",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := serverWithMail(t, c.cfg)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ожидалась ошибка с %q, получили: %v", c.want, err)
			}
		})
	}
}

func TestSMTPUnconfiguredOK(t *testing.T) {
	// Без почты модуль поднимается: письма просто идут в outbox.
	s, err := serverWithMail(t, SMTPConfig{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := s.mailer.(fileMailer); !ok {
		t.Fatalf("ожидался fileMailer, получили %T", s.mailer)
	}
}
