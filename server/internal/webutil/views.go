package webutil

import (
	"context"
	"net/http"
	"strings"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
)

// View один просмотр страницы; пустой Login означает анонима.
type View struct {
	Login   string
	Path    string
	IP      string
	UA      string
	Referer string
}

// ViewSink пишет журнал просмотров; реализация собирается в main.
type ViewSink interface {
	RecordView(ctx context.Context, v View) error
}

// Пределы строк записи просмотра: сырьё клиента не растит строку без края.
const (
	maxViewPath    = 300
	maxViewReferer = 512
)

// viewSkip маршруты, которые не страницы браузера: healthz, JSON API,
// картинки. Расширение имени файла отсекает PageView отдельно.
var viewSkip = []string{"/healthz", "/api/", "/avatars/", "/photos/"}

// PageView ли это навигация браузера на HTML-страницу: GET, Accept с
// text/html и путь без расширения файла. Один предикт и у Identify, и у
// ViewLog: оба стоят на одном пути запроса.
func PageView(r *http.Request) bool {
	if r.Method != http.MethodGet || !strings.Contains(r.Header.Get("Accept"), "text/html") {
		return false
	}
	for _, prefix := range viewSkip {
		if strings.HasPrefix(r.URL.Path, prefix) {
			return false
		}
	}
	last := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	return !strings.Contains(last, ".")
}

// ViewLog пишет просмотры страниц в журнал; ошибка записи не роняет запрос.
type ViewLog struct {
	// Sink куда писать просмотры.
	Sink ViewSink
	// Login резолвит авторизованного из запроса (auth.Server.Login), nil для гостя.
	Login func(*http.Request) string
	// Log куда уходят ошибки записи; может быть nil.
	Log *logging.Logger
}

func (v ViewLog) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if PageView(r) {
			var login string
			if v.Login != nil {
				login = v.Login(r)
			}
			err := v.Sink.RecordView(r.Context(), View{
				Login:   login,
				Path:    TruncateRunes(r.URL.Path, maxViewPath),
				IP:      ClientIP(r),
				UA:      TruncateRunes(r.UserAgent(), MaxUA),
				Referer: TruncateRunes(r.Referer(), maxViewReferer),
			})
			if err != nil && v.Log != nil {
				v.Log.Warn("page view write failed", "err", err)
			}
		}
		next.ServeHTTP(w, r)
	})
}
