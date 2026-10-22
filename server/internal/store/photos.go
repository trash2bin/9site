package store

import "context"

// Photo запись фотоальбома: файл лежит на диске, в базе только метаданные.
type Photo struct {
	ID         int64
	Name       string // подпись под карточкой
	URL        string // путь от корня сайта
	Album      string // слаг раздела
	Width      int
	Height     int
	Bytes      int64
	UploadedBy string // логин загрузившего, пусто у засева
	CreatedAt  string // datetime('now') из SQLite как есть
}

const photoColumns = `id, name, url, album, width, height, bytes, uploaded_by, created_at`

func scanPhoto(row interface{ Scan(...any) error }) (*Photo, error) {
	var p Photo
	err := row.Scan(&p.ID, &p.Name, &p.URL, &p.Album, &p.Width, &p.Height,
		&p.Bytes, &p.UploadedBy, &p.CreatedAt)
	if err != nil {
		return nil, noRows(err)
	}
	return &p, nil
}

// ListPhotos отдаёт снимки в порядке добавления: засев идёт группами по разделам.
func (s *Store) ListPhotos(ctx context.Context) ([]Photo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+photoColumns+` FROM photos ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Photo
	for rows.Next() {
		p, err := scanPhoto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// PhotoByID ищет кадр по номеру: по нему проверяется, чей он.
func (s *Store) PhotoByID(ctx context.Context, id int64) (*Photo, error) {
	return scanPhoto(s.db.QueryRowContext(ctx,
		`SELECT `+photoColumns+` FROM photos WHERE id = $1`, id))
}

// CreatePhoto добавляет запись и возвращает её с id и датой.
func (s *Store) CreatePhoto(ctx context.Context, p Photo) (*Photo, error) {
	return scanPhoto(s.db.QueryRowContext(ctx,
		`INSERT INTO photos (name, url, album, width, height, bytes, uploaded_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING `+photoColumns,
		p.Name, p.URL, p.Album, p.Width, p.Height, p.Bytes, p.UploadedBy,
	))
}

// DeletePhoto удаляет запись и возвращает её: по url обработчик убирает файл.
func (s *Store) DeletePhoto(ctx context.Context, id int64) (*Photo, error) {
	return scanPhoto(s.db.QueryRowContext(ctx,
		`DELETE FROM photos WHERE id = $1 RETURNING `+photoColumns, id))
}
