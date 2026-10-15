package store

import (
	"context"
)

// Action — одно действие пользователя для журнала: вход, смена пароля,
// заявка с формы. Структура вместо трёх строк подряд — чтобы нельзя было
// перепутать порядок аргументов.
type Action struct {
	Login  string
	Action string
	IP     string
}

// Entry — строка журнала действий со временем записи.
type Entry struct {
	At     string
	Login  string
	Action string
	IP     string
}

// Record пишет действие в журнал действий — его потом показывает
// админ-панель. Ошибка не должна ронять запрос: вызывающий логирует и
// идёт дальше (журнал — побочный эффект, а не бизнес-действие).
func (s *Store) Record(ctx context.Context, a Action) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO actions (login, action, ip) VALUES ($1, $2, $3)`,
		a.Login, a.Action, a.IP,
	)
	return err
}

// Recent отдаёт последние действия пользователей, свежие первыми.
func (s *Store) Recent(ctx context.Context, limit int) ([]Entry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT at, login, action, ip FROM actions ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.At, &e.Login, &e.Action, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
