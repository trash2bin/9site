package auth

import (
	"crypto/tls"
	"fmt"
	"mime"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

// sendMail доставляет письмо. Всегда сохраняет копию в outbox — это и
// «почтовый ящик» из методички, и журнал отправок. Если SMTP настроен,
// письмо дополнительно уходит по-настоящему.
func (s *Server) sendMail(to, subject, body string) error {
	from := s.cfg.SMTP.From
	if from == "" {
		from = s.cfg.SMTP.Username
	}
	if from == "" {
		from = "no-reply@localhost"
	}

	msg := message(from, to, subject, body)

	name := filepath.Join(s.outboxDir, time.Now().Format("20060102-150405.000")+".eml")
	if err := os.WriteFile(name, msg, 0o644); err != nil {
		return fmt.Errorf("исходящее письмо: %w", err)
	}
	s.log.Info("письмо сохранено в outbox", "to", to, "file", name)

	if !s.cfg.SMTP.Configured() {
		return nil
	}

	if err := sendSMTP(s.cfg.SMTP, to, msg); err != nil {
		return fmt.Errorf("SMTP: %w", err)
	}
	s.log.Info("письмо отправлено", "to", to, "smtp", s.cfg.SMTP.Host, "port", s.cfg.SMTP.Port)
	return nil
}

// Configured — настроен ли почтовый сервер.
func (c SMTPConfig) Configured() bool {
	return c.Host != "" && c.Port > 0
}

// sendSMTP отправляет собранное письмо. Порт 465 — сразу TLS (SMTPS),
// остальные — обычный SMTP, который сам поднимет STARTTLS, если сервер
// его предлагает.
func sendSMTP(cfg SMTPConfig, to string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	var auth smtp.Auth
	if cfg.Username != "" {
		auth = smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
	}

	if cfg.Port == 465 {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.Host})
		if err != nil {
			return err
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			return err
		}
		defer client.Close()

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
		if err := client.Mail(cfg.From); err != nil {
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

	return smtp.SendMail(addr, auth, cfg.From, []string{to}, msg)
}
