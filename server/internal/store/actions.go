package store

import (
	"context"
	"strings"
)

// Action is one durable user action.
type Action struct {
	Login  string
	Event  string
	Target string
	Result string
	IP     string
}

// Entry is one action journal row.
type Entry struct {
	At     string
	Login  string
	Event  string
	Target string
	Result string
	IP     string
}

// ActionFilter narrows an action journal query.
type ActionFilter struct {
	Login  string
	Event  string
	Result string
}

// Record writes one action to the journal.
func (s *Store) Record(ctx context.Context, a Action) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO actions (login, action, target, result, ip) VALUES ($1, $2, $3, $4, $5)`,
		a.Login, a.Event, a.Target, a.Result, a.IP,
	)
	return err
}

const actionFilter = `WHERE ($1 = '' OR login LIKE $4 ESCAPE '\')
  AND ($2 = '' OR action = $2)
  AND ($3 = '' OR result = $3)`

// ListActions returns matching actions, newest first.
func (s *Store) ListActions(ctx context.Context, filter ActionFilter, limit, offset int) ([]Entry, error) {
	if limit < 1 {
		return nil, nil
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT at, login, action, target, result, ip FROM actions `+actionFilter+
			` ORDER BY id DESC LIMIT ? OFFSET ?`,
		filter.Login, filter.Event, filter.Result, actionLoginPattern(filter.Login), limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Entry
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.At, &e.Login, &e.Event, &e.Target, &e.Result, &e.IP); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountActions returns the number of actions matching the filter.
func (s *Store) CountActions(ctx context.Context, filter ActionFilter) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM actions `+actionFilter,
		filter.Login, filter.Event, filter.Result, actionLoginPattern(filter.Login),
	).Scan(&count)
	return count, err
}

func actionLoginPattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return "%" + value + "%"
}

// Recent returns the latest actions, newest first.
func (s *Store) Recent(ctx context.Context, limit int) ([]Entry, error) {
	return s.ListActions(ctx, ActionFilter{}, limit, 0)
}
