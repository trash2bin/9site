package store

import (
	"context"
	"database/sql"
	"errors"
)

// Роли: admin правит альбом и права, user грузит кадры и ведёт профиль.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// ValidRole роль из разрешённого набора: в базу идёт только она.
func ValidRole(role string) bool { return role == RoleAdmin || role == RoleUser }

// IsAdmin роль admin.
func (u User) IsAdmin() bool { return u.Role == RoleAdmin }

const userColumns = `id, login, pass_hash, full_name, email, avatar, role`

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
	return &User{ID: id, Login: login, PassHash: passHash, FullName: fullName, Email: email, Role: RoleUser}, nil
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
	if err := row.Scan(&u.ID, &u.Login, &u.PassHash, &u.FullName, &u.Email, &u.Avatar, &u.Role); err != nil {
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

// SetUserRole меняет роль по логину: ErrNotFound, если такого логина нет.
func (s *Store) SetUserRole(ctx context.Context, login, role string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET role = $1 WHERE login = $2`, role, login,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListUsers отдаёт страницу пользователей, новые первыми.
func (s *Store) ListUsers(ctx context.Context, limit, offset int) ([]User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+userColumns+` FROM users ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []User
	for rows.Next() {
		var u User
		err := rows.Scan(&u.ID, &u.Login, &u.PassHash, &u.FullName, &u.Email, &u.Avatar, &u.Role)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountUsers сколько всего аккаунтов: из него считается число страниц.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// AdminCount сколько админов: последнего не даём снять.
func (s *Store) AdminCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE role = $1`, RoleAdmin,
	).Scan(&n)
	return n, err
}

// EnsureAdmin заводит админа из окружения или обновляет ему роль и пароль:
// секретный логин из deploy.env работает после любого рестарта.
func (s *Store) EnsureAdmin(ctx context.Context, login, passHash string) (created bool, err error) {
	_, err = s.UserByLogin(ctx, login)
	switch {
	case err == nil:
		_, err = s.db.ExecContext(ctx,
			`UPDATE users SET role = $1, pass_hash = $2 WHERE login = $3`,
			RoleAdmin, passHash, login,
		)
		return false, err
	case !errors.Is(err, ErrNotFound):
		return false, err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (login, pass_hash, role) VALUES ($1, $2, $3)`,
		login, passHash, RoleAdmin,
	)
	return err == nil, err
}
