package auth

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
)

const actionsPageSize = 50

type actionView struct {
	At     string
	Login  string
	Event  string
	Target string
	Result string
	IP     string
}

type actionsData struct {
	Entries      []actionView
	Events       []logging.Event
	LoginFilter  string
	EventFilter  string
	ResultFilter string
	Total        int
	Page         int
	Pages        int
	Prev         int
	Next         int
	Login        string
}

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	filter := ActionFilter{
		Login:  strings.TrimSpace(r.URL.Query().Get("login")),
		Event:  strings.TrimSpace(r.URL.Query().Get("event")),
		Result: strings.TrimSpace(r.URL.Query().Get("result")),
	}
	page := pageNumber(r.URL.Query().Get("page"))

	total, err := s.actions.CountActions(r.Context(), filter)
	if err != nil {
		s.internalError(w, err)
		return
	}

	pages := (total + actionsPageSize - 1) / actionsPageSize
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}

	entries, err := s.actions.ListActions(r.Context(), filter, actionsPageSize, (page-1)*actionsPageSize)
	if err != nil {
		s.internalError(w, err)
		return
	}

	data := actionsData{
		Entries:      make([]actionView, 0, len(entries)),
		Events:       logging.Events(),
		LoginFilter:  filter.Login,
		EventFilter:  filter.Event,
		ResultFilter: filter.Result,
		Total:        total,
		Page:         page,
		Pages:        pages,
		Login:        s.Login(r),
	}
	for _, entry := range entries {
		data.Entries = append(data.Entries, actionView{
			At:     formatActionTime(entry.At),
			Login:  fallbackActionValue(entry.Login),
			Event:  fallbackActionValue(entry.Event),
			Target: fallbackActionValue(entry.Target),
			Result: fallbackActionValue(entry.Result),
			IP:     fallbackActionValue(entry.IP),
		})
	}
	if page > 1 {
		data.Prev = page - 1
	}
	if page < pages {
		data.Next = page + 1
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := actionsTemplate.Execute(w, data); err != nil {
		s.log.Error("actions template failed", "err", err)
	}
}

func formatActionTime(value string) string {
	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
	if err != nil {
		return fallbackActionValue(value)
	}
	return fmt.Sprintf("%s UTC", parsed.Format("2006-01-02 15:04:05"))
}

func fallbackActionValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
