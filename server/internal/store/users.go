package store

import (
	"context"
	"database/sql"
	"errors"
)

const userColumns = `id, login, pass_hash, full_name, email, avatar`

// CreateUser добавляет пользователя. Логин занят → ErrLoginTaken,
// почта занята → ErrEmailTaken.
func (s *Store) CreateUser(ctx context.Context, login, passHash, fullName, email string) (*User, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO users (login, pass_hash, full_name, email) VALUES ($1, $2, $3, $4)`,
		login, passHash, fullName, email,
	)
	if err != nil {
		if isUniqueViolation(err) {
			// Какой индекс сработал, логин или почта: спрашиваем базу,
			// текст ошибки SQLite не парсим.
			if _, e := s.UserByLogin(ctx, login); errors.Is(e, ErrNotFound) {
				return nil, ErrEmailTaken
			}
			return nil, ErrLoginTaken
		}
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Login: login, PassHash: passHash, FullName: fullName, Email: email}, nil
}

// UserByLogin ищет пользователя по логину: ErrNotFound, если его нет.
func (s *Store) UserByLogin(ctx context.Context, login string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE login = $1`, login,
	))
}

// UserByID ищет пользователя по идентификатору.
func (s *Store) UserByID(ctx context.Context, id int64) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id,
	))
}

// UserByEmail ищет пользователя по почте (для сброса пароля).
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE email = $1 COLLATE NOCASE`, email,
	))
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Login, &u.PassHash, &u.FullName, &u.Email, &u.Avatar); err != nil {
		return nil, noRows(err)
	}
	return &u, nil
}

// UpdateProfile меняет данные профиля (логин не меняется): почта уже
// занята другим аккаунтом → ErrEmailTaken.
func (s *Store) UpdateProfile(ctx context.Context, id int64, fullName, email string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET full_name = $1, email = $2 WHERE id = $3`,
		fullName, email, id,
	)
	if err != nil && isUniqueViolation(err) {
		return ErrEmailTaken
	}
	return err
}

// UpdatePassword меняет только хэш пароля: отзыв сессий не здесь, правило
// «смена пароля гасит все cookie» выполняет вызывающий (auth).
func (s *Store) UpdatePassword(ctx context.Context, id int64, passHash string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET pass_hash = $1 WHERE id = $2`, passHash, id,
	)
	return err
}

// UpdateAvatar запоминает имя нового файла аватара.
func (s *Store) UpdateAvatar(ctx context.Context, id int64, name string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET avatar = $1 WHERE id = $2`, name, id,
	)
	return err
}
