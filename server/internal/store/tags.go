package store

import "context"

// Tags отдаёт теги по алфавиту, только висящие хоть на одном кадре: тег без
// кадров пропадает из фильтра и формы, и опечатка не живёт в витрине вечно.
func (s *Store) Tags(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT t.slug
		FROM tags t
		JOIN photo_tags pt ON pt.tag_id = t.id
		ORDER BY t.slug`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, rows.Err()
}

// EnsureTag заводит тег, если такого ещё нет, и отдаёт его номер.
func (s *Store) EnsureTag(ctx context.Context, slug string) (int64, error) {
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tags (slug) VALUES ($1) ON CONFLICT (slug) DO NOTHING`, slug,
	); err != nil {
		return 0, err
	}

	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM tags WHERE slug = $1`, slug).Scan(&id)
	return id, err
}

// AttachTags вешает на кадр теги по слагам; неизвестный слаг пропускается,
// потому что список проверяет вызывающий.
func (s *Store) AttachTags(ctx context.Context, photoID int64, slugs []string) error {
	for _, slug := range slugs {
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO photo_tags (photo_id, tag_id) SELECT $1, id FROM tags WHERE slug = $2
			 ON CONFLICT DO NOTHING`, photoID, slug,
		); err != nil {
			return err
		}
	}
	return nil
}

// TagsByPhoto отдаёт теги всех кадров одним запросом: витрина собирает
// карточки пачкой, а не по одному запросу на кадр.
func (s *Store) TagsByPhoto(ctx context.Context) (map[int64][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT pt.photo_id, t.slug FROM photo_tags pt
		 JOIN tags t ON t.id = pt.tag_id
		 ORDER BY pt.photo_id, t.slug`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var slug string
		if err := rows.Scan(&id, &slug); err != nil {
			return nil, err
		}
		out[id] = append(out[id], slug)
	}
	return out, rows.Err()
}
