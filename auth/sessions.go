package auth

import (
	"database/sql"
	"time"
)

// Session — активная сессия: кто вошёл, чем подписаны формы и до какого
// момента cookie действует.
type Session struct {
	TokenHash string
	UserID    int64
	CSRF      string
	ExpiresAt time.Time
}

// createSession регистрирует новую сессию. Возвращает сам токен (он же
// значение cookie) и CSRF-подпись форм. В базу попадает только хэш токена.
func (s *Server) createSession(userID int64) (tokenValue, csrf string, err error) {
	tokenValue, err = token()
	if err != nil {
		return "", "", err
	}
	csrf, err = token()
	if err != nil {
		return "", "", err
	}

	_, err = s.db.Exec(
		`INSERT INTO sessions (token_hash, user_id, csrf, expires_at) VALUES ($1, $2, $3, $4)`,
		hashToken(tokenValue), userID, csrf, nowPlus(sessionTTL),
	)
	if err != nil {
		return "", "", err
	}

	// Раз в десяток входов прибираем протухшие сессии — таблица не растёт
	// бесконечно даже без внешней чистки.
	if time.Now().Unix()%10 == 0 {
		s.db.Exec(`DELETE FROM sessions WHERE expires_at < $1`, time.Now().UTC().Format(time.RFC3339))
	}

	return tokenValue, csrf, nil
}

// sessionByToken достаёт сессию по cookie-токену. Просроченная сессия
// удаляется и не считается действующей.
func (s *Server) sessionByToken(tokenValue string) (*Session, error) {
	var sess Session
	var expires string

	err := s.db.QueryRow(
		`SELECT token_hash, user_id, csrf, expires_at FROM sessions WHERE token_hash = $1`,
		hashToken(tokenValue),
	).Scan(&sess.TokenHash, &sess.UserID, &sess.CSRF, &expires)
	if err != nil {
		return nil, err
	}

	sess.ExpiresAt, err = time.Parse(time.RFC3339, expires)
	if err != nil || sess.ExpiresAt.Before(time.Now()) {
		s.db.Exec(`DELETE FROM sessions WHERE token_hash = $1`, sess.TokenHash)
		return nil, sql.ErrNoRows
	}

	return &sess, nil
}

// deleteSession выкидывает одну сессию («выйти»).
func (s *Server) deleteSession(tokenValue string) {
	s.db.Exec(`DELETE FROM sessions WHERE token_hash = $1`, hashToken(tokenValue))
}

// --- сброс пароля ---

// createResetToken делает одноразовую ссылку на смену пароля и возвращает
// сам токен для письма. В базу — только хэш.
func (s *Server) createResetToken(userID int64) (string, error) {
	tokenValue, err := token()
	if err != nil {
		return "", err
	}

	_, err = s.db.Exec(
		`INSERT INTO reset_tokens (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hashToken(tokenValue), userID, nowPlus(resetTTL),
	)
	if err != nil {
		return "", err
	}
	return tokenValue, nil
}

// consumeResetToken проверяет токен из ссылки и помечает его использованным.
// Одноразовость: повторный переход по той же ссылке уже не работает.
func (s *Server) consumeResetToken(tokenValue string) (int64, error) {
	var (
		userID  int64
		expires string
		used    int
	)

	err := s.db.QueryRow(
		`SELECT user_id, expires_at, used FROM reset_tokens WHERE token_hash = $1`,
		hashToken(tokenValue),
	).Scan(&userID, &expires, &used)
	if err != nil {
		return 0, err
	}

	exp, err := time.Parse(time.RFC3339, expires)
	if err != nil || used != 0 || exp.Before(time.Now()) {
		return 0, sql.ErrNoRows
	}

	s.db.Exec(`UPDATE reset_tokens SET used = 1 WHERE token_hash = $1`, hashToken(tokenValue))
	return userID, nil
}
