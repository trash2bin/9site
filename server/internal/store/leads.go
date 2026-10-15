package store

import "context"

// Lead — заявка с публичной формы Sign Up (страница /maket, POST /signup).
type Lead struct {
	Name      string
	Phone     string
	Email     string
	IP        string
	UserAgent string
}

// SaveLead сохраняет заявку. Пока заявки смотрят через sqlite3; когда
// появится админ-панель — данные будут браться отсюда же.
func (s *Store) SaveLead(ctx context.Context, l Lead) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO leads (name, phone, email, ip, user_agent) VALUES ($1, $2, $3, $4, $5)`,
		l.Name, l.Phone, l.Email, l.IP, l.UserAgent,
	)
	return err
}

// ListLeads отдаёт свежие заявки, новые первыми.
func (s *Store) ListLeads(ctx context.Context, limit int) ([]Lead, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, phone, email, ip, user_agent FROM leads ORDER BY id DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []Lead
	for rows.Next() {
		var l Lead
		if err := rows.Scan(&l.Name, &l.Phone, &l.Email, &l.IP, &l.UserAgent); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
