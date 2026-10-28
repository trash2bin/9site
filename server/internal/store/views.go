package store

import (
	"context"
	"time"
)

// Аудитории выборки просмотров.
const (
	AudienceAll       = ""
	AudienceUsers     = "users"
	AudienceAnonymous = "anonymous"
)

// View один просмотр страницы; пустой Login означает анонима.
type View struct {
	Login   string
	Path    string
	IP      string
	UA      string
	Referer string
}

// ViewEntry строка журнала просмотров, время как datetime('now') UTC.
type ViewEntry struct {
	At      string
	Login   string
	Path    string
	IP      string
	UA      string
	Referer string
}

// ViewFilter сузить выборку просмотров: пустое поле не фильтрует,
// Audience берёт только AudienceUsers или AudienceAnonymous.
type ViewFilter struct {
	Login    string
	Path     string
	Audience string
	// Since нижняя граница at в формате datetime('now'), пусто = вся история.
	Since string
}

// PathCount сколько раз страницу смотрели.
type PathCount struct {
	Path string
	N    int
}

// RecordView пишет один просмотр в журнал.
func (s *Store) RecordView(ctx context.Context, v View) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO page_views (login, path, ip, ua, referer) VALUES ($1, $2, $3, $4, $5)`,
		v.Login, v.Path, v.IP, v.UA, v.Referer,
	)
	return err
}

const viewFilter = `WHERE ($1 = '' OR login LIKE $2 ESCAPE '\')
  AND ($3 = '' OR path LIKE $4 ESCAPE '\')
  AND ($5 = '' OR ($5 = 'users' AND login <> '') OR ($5 = 'anonymous' AND login = ''))
  AND ($6 = '' OR at >= $6)`

// ListViews возвращает просмотры, новые сверху.
func (s *Store) ListViews(ctx context.Context, filter ViewFilter, limit, offset int) ([]ViewEntry, error) {
	if limit < 1 {
		return nil, nil
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT at, login, path, ip, ua, referer FROM page_views `+viewFilter+
			` ORDER BY id DESC LIMIT ? OFFSET ?`,
		filter.Login, likePattern(filter.Login),
		filter.Path, likePattern(filter.Path),
		filter.Audience, filter.Since,
		limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []ViewEntry
	for rows.Next() {
		var e ViewEntry
		if err := rows.Scan(&e.At, &e.Login, &e.Path, &e.IP, &e.UA, &e.Referer); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountViews считает просмотры по тому же фильтру.
func (s *Store) CountViews(ctx context.Context, filter ViewFilter) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM page_views `+viewFilter,
		filter.Login, likePattern(filter.Login),
		filter.Path, likePattern(filter.Path),
		filter.Audience, filter.Since,
	).Scan(&count)
	return count, err
}

// TopPaths самые смотримые страницы по тому же фильтру.
func (s *Store) TopPaths(ctx context.Context, filter ViewFilter, limit int) ([]PathCount, error) {
	if limit < 1 {
		return nil, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT path, COUNT(*) FROM page_views `+viewFilter+
			` GROUP BY path ORDER BY COUNT(*) DESC, path LIMIT ?`,
		filter.Login, likePattern(filter.Login),
		filter.Path, likePattern(filter.Path),
		filter.Audience, filter.Since,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []PathCount
	for rows.Next() {
		var p PathCount
		if err := rows.Scan(&p.Path, &p.N); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PurgeViews удаляет просмотры старше before, возвращает число строк.
func (s *Store) PurgeViews(ctx context.Context, before time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM page_views WHERE at < ?`,
		before.UTC().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
