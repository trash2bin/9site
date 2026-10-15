package auth_test

// Тесты почтовых пресетов: провайдер подставляет хост/порт, кривой конфиг
// ловится на старте, а не при первом сбросе пароля.

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivan223/web1-personal-site/server/internal/auth"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

func mailConfig(t *testing.T, log *logging.Logger, smtp auth.SMTPConfig) auth.Config {
	t.Helper()

	dir := t.TempDir()
	st := openTestStore(t, dir)
	return auth.Config{
		Users:      st,
		Sessions:   st,
		AvatarsDir: filepath.Join(dir, "avatars"),
		OutboxDir:  filepath.Join(dir, "outbox"),
		Throttle:   testThrottle(t, 1000),
		Log:        log,
		SMTP:       smtp,
	}
}

func TestSMTPPreset(t *testing.T) {
	log, buf := testLogger(t)
	if _, err := auth.New(mailConfig(t, log, auth.SMTPConfig{
		Provider: "gmail",
		Username: "me@gmail.com",
		Password: "app-password",
	})); err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "почта: SMTP настроен") ||
		!strings.Contains(got, "smtp.gmail.com:587") {
		t.Fatalf("в логе нет пресета gmail:\n%s", got)
	}

	// Регистр имени провайдера не важен.
	log, buf = testLogger(t)
	if _, err := auth.New(mailConfig(t, log, auth.SMTPConfig{
		Provider: "YANDEX",
		Username: "me@yandex.ru",
		Password: "app-password",
	})); err != nil {
		t.Fatalf("New (YANDEX): %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "smtp.yandex.ru:465") {
		t.Fatalf("в логе нет пресета yandex:\n%s", got)
	}
}

func TestSMTPConfigErrors(t *testing.T) {
	cases := []struct {
		name string
		cfg  auth.SMTPConfig
		want string
	}{
		{
			name: "провайдер вместе с хостом",
			cfg:  auth.SMTPConfig{Provider: "gmail", Host: "smtp.example.com"},
			want: "оставь что-то одно",
		},
		{
			name: "неизвестный провайдер",
			cfg:  auth.SMTPConfig{Provider: "rambler", Username: "a", Password: "b"},
			want: "неизвестный провайдер",
		},
		{
			name: "провайдер без логина-пароля",
			cfg:  auth.SMTPConfig{Provider: "gmail"},
			want: "не заполнены SMTP_USER и SMTP_PASS",
		},
		{
			name: "пароль без логина",
			cfg:  auth.SMTPConfig{Host: "smtp.example.com", Port: 587, Password: "b"},
			want: "задаются вместе",
		},
		{
			name: "логин без сервера",
			cfg:  auth.SMTPConfig{Username: "a", Password: "b"},
			want: "не настроен сервер",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			log, _ := testLogger(t)
			_, err := auth.New(mailConfig(t, log, c.cfg))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ожидалась ошибка с %q, получили: %v", c.want, err)
			}
		})
	}
}

func TestSMTPUnconfiguredOK(t *testing.T) {
	// Без почты модуль поднимается, лог честно говорит про outbox.
	log, buf := testLogger(t)
	if _, err := auth.New(mailConfig(t, log, auth.SMTPConfig{})); err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "почта: SMTP не настроен") {
		t.Fatalf("нет строки о ненастроенной почте:\n%s", got)
	}
}

// TestUnreachableSMTPKeepsOutbox недоступный SMTP не теряет письмо:
// копия всегда лежит в outbox.
func TestUnreachableSMTPKeepsOutbox(t *testing.T) {
	// Захватываем порт и отпускаем: соединение отклоняется сразу.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	env := startEnv(t, func(cfg *auth.Config, _ *store.Store) {
		cfg.SMTP = auth.SMTPConfig{
			Host:     "127.0.0.1",
			Port:     port,
			Username: "smtp-user",
			Password: "smtp-pass",
		}
	})

	c := client(t)
	register(t, c, env.ts.URL, "deadsmtp", "secret-pass-1", "", "dead@example.com", nil)

	res, err := c.PostForm(env.ts.URL+"/auth/forgot", url.Values{
		"email": {"dead@example.com"},
		"csrf":  {formCSRF(t, c, env.ts.URL)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	matches, _ := filepath.Glob(filepath.Join(env.outbox, "*.eml"))
	if len(matches) != 1 {
		t.Fatalf("копия письма не попала в outbox: файлов %d", len(matches))
	}
}
