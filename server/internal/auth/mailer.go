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

// Mailer — доставка писем. Интерфейс объявлен здесь, в модуле, который
// шлёт письма: SMTP, файл-«почтовый ящик» и заглушка в тестах
// подменяются без ведома остального кода.
type Mailer interface {
	// Send отправляет готовое RFC822-сообщение от имени from. to и
	// subject — данные для журнала и имени файла в outbox.
	Send(ctx context.Context, from, to, subject string, raw []byte) error
}

// message собирает письмо в формате RFC 822: оно и по SMTP уходит, и в
// файл-«почтовый ящик» сохраняется в одинаковом виде.
func message(from, to, subject, body string) []byte {
	var b strings.Builder

	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", to)
	// Тема — не-ASCII, поэтому кодируем по RFC 2047; иначе письмо
	// отобразится кракозябрами.
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(body)

	return []byte(b.String())
}

// smtpPresets — предустановки SMTP популярных почтовых провайдеров: хост
// и порт подставляются по имени, логин/пароль — свои. У всех трёх нужен
// именно пароль приложения: обычный пароль аккаунта SMTP не пускает.
var smtpPresets = map[string]struct {
	host string
	port int
	// hint — где взять пароль приложения; показывается в логе при старте.
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

// resolve подставляет настройки выбранного провайдера и проверяет, что
// конфиг осмысленный. Стоит здесь, а не в main: нерабочий SMTP письма
// не роняет — они молча копируются в outbox, поэтому опечатку в конфиге
// надо ловить на старте, а не через месяц, когда кто-то забудет пароль.
// Возвращает подсказку для лога (пустую, если её нет).
func (c *SMTPConfig) resolve() (hint string, err error) {
	if c.Provider != "" {
		if c.Host != "" {
			return "", fmt.Errorf("SMTP: заданы и SMTP_PROVIDER=%q, и SMTP_HOST — оставь что-то одно", c.Provider)
		}
		preset, ok := smtpPresets[strings.ToLower(c.Provider)]
		if !ok {
			return "", fmt.Errorf("SMTP: неизвестный провайдер %q (есть: gmail, yandex, mailru) — или задай SMTP_HOST сам", c.Provider)
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
		return "", errors.New("SMTP: заданы логин/пароль, но не настроен сервер — укажи SMTP_PROVIDER или SMTP_HOST")
	}
	return hint, nil
}

// buildMailer собирает почтовый тракт по конфигу. outboxMailer —
// декоратор: копия письма кладётся в outbox независимо от того, ушло ли
// письмо по SMTP. Без SMTP наружу идёт только файл-«почтовый ящик».
func (s *Server) buildMailer() Mailer {
	out := fileMailer{dir: s.outboxDir}
	if !s.cfg.SMTP.Configured() {
		return out
	}
	return outboxMailer{inner: smtpMailer{cfg: s.cfg.SMTP}, outbox: out}
}

// sendMail доставляет письмо и пишет в лог, куда оно делось.
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
		s.log.Info("SMTP не настроен — письмо сохранено только в outbox", "to", to)
	}
	return nil
}

// --- реализации Mailer ---

// fileMailer кладёт письма в outboxDir — «почтовый ящик» из методички.
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

// outboxMailer — декоратор: каждое письмо сначала копируется в outbox
// (журнал отправок), потом уходит через внутренний Mailer.
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

// smtpMailer шлёт письмо по SMTP.
type smtpMailer struct {
	cfg SMTPConfig
}

func (m smtpMailer) Send(ctx context.Context, from, to string, _ string, raw []byte) error {
	if err := sendSMTP(ctx, m.cfg, from, to, raw); err != nil {
		return fmt.Errorf("SMTP: %w", err)
	}
	return nil
}

// Configured — настроен ли почтовый сервер.
func (c SMTPConfig) Configured() bool {
	return c.Host != "" && c.Port > 0
}

// sendSMTP отправляет собранное письмо. Порт 465 — сразу TLS (SMTPS),
// остальные — обычный SMTP, который сам поднимет STARTTLS, если сервер
// его предлагает. Дедлайн на всё соединение: висящий SMTP (принял TCP и
// молчит в DATA-фазе) не должен держать запрос на смену пароля
// бесконечно.
func sendSMTP(ctx context.Context, cfg SMTPConfig, from, to string, msg []byte) error {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))

	dialer := &net.Dialer{Timeout: 15 * time.Second}

	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}

	var conn net.Conn
	var err error
	implicitTLS := cfg.Port == 465 // 465 — сразу TLS (SMTPS)
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
	// Общий дедлайн на весь диалог: и на запись, и на чтение ответов.
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	// net/smtp сам не поднимает STARTTLS (это делает только его SendMail):
	// без этого шага submission-серверы на обычном порту (Gmail — 587)
	// отвергнут Auth как незашифрованное соединение.
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
	// Envelope-отправитель — тот же, что в заголовке From (sendMail
	// подставляет туда фолбэк): разные источники может отвергнуть сервер.
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

// safeFileName оставляет в адресе только безопасные символы — адрес
// попадает в имя файла письма.
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
