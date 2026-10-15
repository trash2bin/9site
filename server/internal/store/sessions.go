package store

import (
	"context"
	"time"
)

// CreateSession сохраняет сессию: в базу идёт только sha256 токена,
// сам токен живёт в cookie у пользователя.
func (s *Store) CreateSession(ctx context.Context, userID int64, tokenHash, csrf string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf, expires_at) VALUES ($1, $2, $3, $4)`,
		tokenHash, userID, csrf, expiresAt.UTC().Format(time.RFC3339),
	)
	return err
}

// SessionByTokenHash возвращает сессию по хэшу токена: ErrNotFound, если
// её нет. Проверка срока на стороне auth.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	var (
		sess    Session
		expires string
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, user_id, csrf, expires_at FROM sessions WHERE token_hash = $1`,
		tokenHash,
	).Scan(&sess.TokenHash, &sess.UserID, &sess.CSRF, &expires)
	if err != nil {
		return nil, noRows(err)
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return nil, err
	}
	sess.ExpiresAt = exp
	return &sess, nil
}

// DeleteSession гасит одну сессию («выйти»).
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE token_hash = $1`, tokenHash,
	)
	return err
}

// DeleteUserSessions гасит все сессии пользователя: при смене пароля чужая
// cookie после неё перестаёт работать.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE user_id = $1`, userID,
	)
	return err
}

// PurgeExpiredSessions убирает протухшие сессии.
func (s *Store) PurgeExpiredSessions(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expires_at < $1`, before.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// CreateResetToken сохраняет одноразовый токен сброса пароля: в базу
// только хэш, сам токен уходит в письмо.
func (s *Store) CreateResetToken(ctx context.Context, userID int64, tokenHash string, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO reset_tokens (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		tokenHash, userID, expiresAt.UTC().Format(time.RFC3339),
	)
	return err
}

// ConsumeResetToken проверяет токен и помечает использованным в одной транзакции:
// параллельные переходы не используют его дважды, невалидный или просроченный - ErrNotFound.
func (s *Store) ConsumeResetToken(ctx context.Context, tokenHash string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		userID  int64
		expires string
		used    int
	)
	err = tx.QueryRowContext(ctx,
		`SELECT user_id, expires_at, used FROM reset_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&userID, &expires, &used)
	if err != nil {
		return 0, noRows(err)
	}
	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || used != 0 || exp.Before(time.Now()) {
		return 0, ErrNotFound
	}

	// AND used = 0 в той же транзакции - страховка атомарности: параллельный
	// переход не спишет токен дважды.
	res, err := tx.ExecContext(ctx,
		`UPDATE reset_tokens SET used = 1 WHERE token_hash = $1 AND used = 0`, tokenHash,
	)
	if err != nil {
		return 0, err
	}
	if affected, err := res.RowsAffected(); err != nil || affected == 0 {
		return 0, ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return userID, nil
}
