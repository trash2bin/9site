package auth

import (
	"context"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// Этот файл — механика сессий поверх интерфейса Sessions: генерация
// токенов, проверка срока, чистка. Само хранение — за хранилищем.

// createSession регистрирует новую сессию. Возвращает сам токен (он же
// значение cookie) и CSRF-подпись форм. В базу попадает только хэш токена.
func (s *Server) createSession(ctx context.Context, userID int64) (tokenValue, csrf string, err error) {
	tokenValue, err = token()
	if err != nil {
		return "", "", err
	}
	csrf, err = token()
	if err != nil {
		return "", "", err
	}

	err = s.sessions.CreateSession(ctx, userID, hashToken(tokenValue), csrf, time.Now().Add(sessionTTL))
	if err != nil {
		return "", "", err
	}

	// Раз в десяток входов прибираем протухшие сессии — таблица не растёт
	// бесконечно даже без внешней чистки. Для Redis это бессмысленно:
	// чистит TTL.
	if time.Now().Unix()%10 == 0 {
		if _, err := s.sessions.PurgeExpiredSessions(ctx, time.Now()); err != nil {
			s.log.Warn("протухшие сессии не прибраны", "err", err)
		}
	}

	return tokenValue, csrf, nil
}

// sessionByToken находит сессию по токену из cookie. Просроченная
// удаляется и считается не найденной; «хранилище недоступно» — настоящая
// ошибка, и она не глотается.
func (s *Server) sessionByToken(ctx context.Context, tokenValue string) (*Session, error) {
	sess, err := s.sessions.SessionByTokenHash(ctx, hashToken(tokenValue))
	if err != nil {
		return nil, err
	}

	if sess.ExpiresAt.Before(time.Now()) {
		if err := s.sessions.DeleteSession(ctx, sess.TokenHash); err != nil {
			s.log.Warn("протухшая сессия не удалена", "err", err)
		}
		return nil, store.ErrNotFound
	}
	return sess, nil
}

// deleteSession выкидывает одну сессию («выйти»).
func (s *Server) deleteSession(ctx context.Context, tokenValue string) {
	if err := s.sessions.DeleteSession(ctx, hashToken(tokenValue)); err != nil {
		s.log.Warn("сессия не удалена", "err", err)
	}
}

// --- сброс пароля ---

// createResetToken делает одноразовую ссылку на смену пароля и возвращает
// сам токен для письма. В базу — только хэш.
func (s *Server) createResetToken(ctx context.Context, userID int64) (string, error) {
	tokenValue, err := token()
	if err != nil {
		return "", err
	}
	if err := s.sessions.CreateResetToken(ctx, userID, hashToken(tokenValue), time.Now().Add(resetTTL)); err != nil {
		return "", err
	}
	return tokenValue, nil
}

// consumeResetToken проверяет токен из ссылки и гасит его. Атомарность —
// на стороне хранилища: повторный переход по той же ссылке возвращает
// store.ErrNotFound.
func (s *Server) consumeResetToken(ctx context.Context, tokenValue string) (int64, error) {
	return s.sessions.ConsumeResetToken(ctx, hashToken(tokenValue))
}
