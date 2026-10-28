package auth

import (
	"net/http"
	"strings"
	"time"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// viewsPageSize строк на странице журнала просмотров.
const viewsPageSize = 50

// topPathsLimit сколько строк топа страниц показывается.
const topPathsLimit = 10

type viewView struct {
	At      string
	Login   string
	Path    string
	IP      string
	UA      string
	Referer string
}

type topPathView struct {
	Path string
	N    int
}

// viewsData данные страницы журнала просмотров.
type viewsData struct {
	Entries     []viewView
	Top         []topPathView
	LoginFilter string
	PathFilter  string
	// Period пресет времени: today|7|30, пусто = вся история.
	Period string
	// Audience кто в выборке: store.AudienceUsers|store.AudienceAnonymous, пусто = все.
	Audience string
	Total    int
	Page     int
	Pages    int
	Prev     int
	Next     int
	Login    string
}

func (s *Server) handleViews(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	period, since := viewsPeriod(q.Get("period"))
	filter := ViewFilter{
		Login:    strings.TrimSpace(q.Get("login")),
		Path:     strings.TrimSpace(q.Get("path")),
		Audience: viewsAudience(q.Get("audience")),
		Since:    since,
	}
	page := pageNumber(q.Get("page"))

	total, err := s.views.CountViews(r.Context(), filter)
	if err != nil {
		s.internalError(w, err)
		return
	}

	pages := (total + viewsPageSize - 1) / viewsPageSize
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}

	entries, err := s.views.ListViews(r.Context(), filter, viewsPageSize, (page-1)*viewsPageSize)
	if err != nil {
		s.internalError(w, err)
		return
	}
	top, err := s.views.TopPaths(r.Context(), filter, topPathsLimit)
	if err != nil {
		s.internalError(w, err)
		return
	}

	data := viewsData{
		Entries:     make([]viewView, 0, len(entries)),
		Top:         make([]topPathView, 0, len(top)),
		LoginFilter: filter.Login,
		PathFilter:  filter.Path,
		Period:      period,
		Audience:    filter.Audience,
		Total:       total,
		Page:        page,
		Pages:       pages,
		Login:       s.Login(r),
	}
	for _, entry := range entries {
		data.Entries = append(data.Entries, viewView{
			At:      formatActionTime(entry.At),
			Login:   fallbackActionValue(entry.Login),
			Path:    fallbackActionValue(entry.Path),
			IP:      fallbackActionValue(entry.IP),
			UA:      fallbackActionValue(entry.UA),
			Referer: fallbackActionValue(entry.Referer),
		})
	}
	for _, p := range top {
		data.Top = append(data.Top, topPathView{Path: fallbackActionValue(p.Path), N: p.N})
	}
	if page > 1 {
		data.Prev = page - 1
	}
	if page < pages {
		data.Next = page + 1
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := viewsTemplate.Execute(w, data); err != nil {
		s.log.Error("views template failed", "err", err)
	}
}

// viewsPeriod пресет времени журнала: мусор сводится к «вся история».
func viewsPeriod(raw string) (period, since string) {
	now := time.Now().UTC()
	format := "2006-01-02 15:04:05"
	switch raw {
	case "today":
		return raw, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Format(format)
	case "7":
		return raw, now.AddDate(0, 0, -7).Format(format)
	case "30":
		return raw, now.AddDate(0, 0, -30).Format(format)
	default:
		return "", ""
	}
}

// viewsAudience допустимая аудитория выборки; мусор = все.
func viewsAudience(raw string) string {
	switch raw {
	case store.AudienceUsers, store.AudienceAnonymous:
		return raw
	default:
		return store.AudienceAll
	}
}
