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
		`SELECT token_hash, user_id, csrf, expires_at, ip, ua, last_seen FROM sessions WHERE token_hash = $1`,
		tokenHash,
	).Scan(&sess.TokenHash, &sess.UserID, &sess.CSRF, &expires, &sess.IP, &sess.UA, &sess.LastSeen)
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

// LastSessionMeta самая свежая по активности сессия пользователя; ErrNotFound,
// если сессий не было. Строки с пустой last_seen сортируются последними.
func (s *Store) LastSessionMeta(ctx context.Context, userID int64) (SessionMeta, error) {
	var m SessionMeta
	err := s.db.QueryRowContext(ctx,
		`SELECT ip, ua, last_seen FROM sessions WHERE user_id = $1
		 ORDER BY last_seen DESC LIMIT 1`, userID,
	).Scan(&m.IP, &m.UA, &m.LastSeen)
	return m, noRows(err)
}

// TouchSession штампует мету активности; повтор внутри минуты не пишется,
// чтобы каждый просмотр страницы не доставал базу.
func (s *Store) TouchSession(ctx context.Context, tokenHash, ip, ua string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET ip = $2, ua = $3, last_seen = datetime('now')
		 WHERE token_hash = $1 AND last_seen < datetime('now', '-60 seconds')`,
		tokenHash, ip, ua,
	)
	return err
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
