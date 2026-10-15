package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Mailer доставка писем; интерфейс объявлен здесь, у модуля, который их шлёт.
type Mailer interface {
	// Send отправляет готовое RFC822-сообщение; to и subject нужны для
	// журнала и имени файла в outbox.
	Send(ctx context.Context, from, to, subject string, raw []byte) error
}

func message(from, to, subject, body string) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	// Тема не-ASCII: кодируем по RFC 2047, иначе кракозябры.
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(body)

	return []byte(b.String())
}

// smtpPresets предустановки SMTP: хост и порт по имени, логин/пароль свои.
var smtpPresets = map[string]struct {
	host string
	port int
	// hint где взять пароль приложения, показывается в логе при старте.
	hint string
}{
	"gmail": {
		"smtp.gmail.com", 587,
		"myaccount.google.com → Безопасность → Пароли приложений (нужна 2FA)",
	},
	"yandex": {
		"smtp.yandex.ru", 465,
		"Яндекс ID → Безопасность → Пароли приложений",
	},
	"mailru": {
		"smtp.mail.ru", 465,
		"настройки почты → Пароли приложений",
	},
}

// resolve подставляет настройки провайдера и проверяет конфиг на старте;
// нерабочий SMTP письма не роняет, они уходят в outbox.
func (c *SMTPConfig) resolve() (hint string, err error) {
	if c.Provider != "" {
		if c.Host != "" {
			return "", fmt.Errorf("SMTP: заданы и SMTP_PROVIDER=%q, и SMTP_HOST: оставь что-то одно", c.Provider)
		}
		preset, ok := smtpPresets[strings.ToLower(c.Provider)]
		if !ok {
			return "", fmt.Errorf("SMTP: неизвестный провайдер %q (есть: gmail, yandex, mailru), или задай SMTP_HOST сам", c.Provider)
		}
		c.Host = preset.host
		c.Port = preset.port
		hint = preset.hint

		if c.Username == "" || c.Password == "" {
			return "", fmt.Errorf("SMTP: SMTP_PROVIDER=%s задан, но не заполнены SMTP_USER и SMTP_PASS (пароль приложения)", c.Provider)
		}
	}

	if (c.Username == "") != (c.Password == "") {
		return "", errors.New("SMTP: логин и пароль задаются вместе")
	}
	if (c.Username != "") && !c.Configured() {
		return "", errors.New("SMTP: заданы логин/пароль, но не настроен сервер: укажи SMTP_PROVIDER или SMTP_HOST")
	}
	return hint, nil
}

// buildMailer собирает тракт: outboxMailer декоратор кладёт копию письма
// в outbox независимо от того, ушло ли оно по SMTP.
func (s *Server) buildMailer() Mailer {
	out := fileMailer{dir: s.outboxDir}
	if !s.cfg.SMTP.Configured() {
		return out
	}
	return outboxMailer{inner: smtpMailer{cfg: s.cfg.SMTP}, outbox: out}
}

func (s *Server) sendMail(ctx context.Context, to, subject, body string) error {
	from := s.cfg.SMTP.From
	if from == "" {
		from = s.cfg.SMTP.Username
	}
	if from == "" {
		from = "no-reply@localhost"
	}

	msg := message(from, to, subject, body)
	if err := s.mailer.Send(ctx, from, to, subject, msg); err != nil {
		return err
	}

	if s.cfg.SMTP.Configured() {
		s.log.Info("письмо отправлено", "to", to, "smtp", s.cfg.SMTP.Host, "port", s.cfg.SMTP.Port)
	} else {
		s.log.Info("SMTP не настроен: письмо сохранено только в outbox", "to", to)
	}
	return nil
}

// --- реализации Mailer ---

type fileMailer struct {
	dir string
}

func (m fileMailer) Send(_ context.Context, _, to, subject string, raw []byte) error {
	name := filepath.Join(m.dir,
		time.Now().Format("20060102-150405.000")+"-"+safeFileName(to)+".eml")
	if err := os.WriteFile(name, raw, 0o644); err != nil {
		return fmt.Errorf("исходящее письмо: %w", err)
	}
	return nil
}

// outboxMailer декоратор: письмо сначала копируется в outbox, потом уходит через внутренний Mailer.
type outboxMailer struct {
	inner  Mailer
	outbox fileMailer
}

func (m outboxMailer) Send(ctx context.Context, from, to, subject string, raw []byte) error {
	if err := m.outbox.Send(ctx, from, to, subject, raw); err != nil {
		return err
	}
	return m.inner.Send(ctx, from, to, subject, raw)
}

type smtpMailer struct {
	cfg SMTPConfig
}

func (m smtpMailer) Send(ctx context.Context, from, to string, _ string, raw []byte) error {
	if err := sendSMTP(ctx, m.cfg, from, to, raw); err != nil {
		return fmt.Errorf("SMTP: %w", err)
	}
	return nil
}

func (c SMTPConfig) Configured() bool {
	return c.Host != "" && c.Port > 0
}

// sendSMTP отправляет собранное письмо: 465 сразу TLS (SMTPS), остальные
// обычный SMTP с STARTTLS. Дедлайн на соединение, чтобы висящий сервер не держал запрос.
func sendSMTP(ctx context.Context, cfg SMTPConfig, from, to string, msg []byte) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))

	dialer := &net.Dialer{Timeout: 15 * time.Second}

	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}

	var conn net.Conn
	var err error
	implicitTLS := cfg.Port == 465
	if implicitTLS {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    &tls.Config{ServerName: cfg.Host},
		}
		conn, err = tlsDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	// net/smtp сам не поднимает STARTTLS: без него submission-серверы
	// (Gmail, 587) отвергнут Auth как незашифрованное соединение.
	if !implicitTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
				return err
			}
		}
	}

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	// Envelope-отправитель тот же, что в заголовке From: разные источники
	// может отвергнуть сервер.
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// safeFileName оставляет в адресе только символы, безопасные для имени файла.
func safeFileName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '@' || r == '.' || r == '-' || r == '_':
			return r
		default:
			return '_'
		}
	}, s)
}
