package auth

import (
	"context"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// createSession регистрирует сессию и возвращает токен для cookie и подпись форм.
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

	// Раз в десяток входов прибираем протухшие: таблица не растёт
	// бесконечно даже без внешней чистки.
	if time.Now().Unix()%10 == 0 {
		if _, err := s.sessions.PurgeExpiredSessions(ctx, time.Now()); err != nil {
			s.log.Warn("протухшие сессии не прибраны", "err", err)
		}
	}

	return tokenValue, csrf, nil
}

// sessionByToken находит сессию по токену из cookie; просроченная
// удаляется и считается не найденной.
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

func (s *Server) deleteSession(ctx context.Context, tokenValue string) {
	if err := s.sessions.DeleteSession(ctx, hashToken(tokenValue)); err != nil {
		s.log.Warn("сессия не удалена", "err", err)
	}
}

// --- сброс пароля ---

// createResetToken делает одноразовую ссылку на смену пароля.
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

// consumeResetToken проверяет токен и гасит его; атомарность на стороне
// хранилища, повторный переход возвращает store.ErrNotFound.
func (s *Server) consumeResetToken(ctx context.Context, tokenValue string) (int64, error) {
	return s.sessions.ConsumeResetToken(ctx, hashToken(tokenValue))
}
