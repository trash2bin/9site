package album

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// photoFile допустимое имя файла загрузки: генерируется сервером, снаружи
// ничего другого не отдаётся.
var photoFile = regexp.MustCompile(`^p[0-9a-f]{16}\.(gif|webp)$`)

// photosResponse ответ витрины: снимки и разделы, которые есть в базе.
type photosResponse struct {
	Photos []photoPayload `json:"photos"`
	Albums []string       `json:"albums"`
}

// photoPayload снимок в JSON: карточке и фильтру нужны только эти поля.
type photoPayload struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Album  string `json:"album"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// handlePhotos отдаёт список снимков для витрины и админки.
func (s *Server) handlePhotos(w http.ResponseWriter, r *http.Request) {
	photos, err := s.store.ListPhotos(r.Context())
	if err != nil {
		s.log.Error("альбом: список снимков не читается", "err", err)
		http.Error(w, "список снимков не читается", http.StatusInternalServerError)
		return
	}
	albums, err := s.store.Albums(r.Context())
	if err != nil {
		s.log.Error("альбом: список разделов не читается", "err", err)
		http.Error(w, "список разделов не читается", http.StatusInternalServerError)
		return
	}

	out := photosResponse{Photos: make([]photoPayload, 0, len(photos)), Albums: albums}
	if out.Albums == nil {
		out.Albums = []string{}
	}
	for _, p := range photos {
		out.Photos = append(out.Photos, photoPayload{
			Name: p.Name, URL: p.URL, Album: p.Album, Width: p.Width, Height: p.Height,
		})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.log.Error("альбом: ответ не ушёл", "err", err)
	}
}

// handlePhotoFile отдаёт файл загрузки. Имя проверено регулярным выражением,
// поэтому выйти за каталог нельзя.
func (s *Server) handlePhotoFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !photoFile.MatchString(name) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", contentTypeFor(name))
	// Имя уникально для каждой загрузки: кэш можно держать навсегда.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, s.photosFile(name))
}

func contentTypeFor(name string) string {
	if ext := filepath.Ext(name); ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	return "application/octet-stream"
}

// --- закрытая страница /vault/album ---

// adminPhoto снимок в списке админки.
type adminPhoto struct {
	Photo
	// Uploaded файл лежит в каталоге загрузок, а не в статике засева.
	Uploaded bool
}

// adminData данные страницы загрузки.
type adminData struct {
	Photos  []adminPhoto
	Albums  []string
	Login   string
	CSRF    string
	Error   string
	Message string
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	data := adminData{}
	switch r.URL.Query().Get("ok") {
	case "1":
		data.Message = "кадр загружен"
	case "deleted":
		data.Message = "кадр удалён"
	}
	s.renderAdmin(w, r, data)
}

// handleUpload принимает файл и подпись, кладёт файл на диск, запись в базу.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Лимит тела до разбора формы: без него PostFormValue прочитал бы файл целиком.
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(maxBody); err != nil {
		s.renderAdmin(w, r, adminData{Error: fmt.Sprintf("файл больше %d МБ или форма битая", maxPhoto>>20)})
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	if !s.access.CSRFOK(r) {
		s.renderAdmin(w, r, adminData{Error: "форма устарела: обновите страницу"})
		return
	}

	img, err := readImage(r)
	if err != nil {
		s.renderAdmin(w, r, adminData{Error: err.Error()})
		return
	}

	caption := cleanCaption(r.PostFormValue("name"))
	if caption == "" {
		s.renderAdmin(w, r, adminData{Error: "нужна подпись под кадром"})
		return
	}
	slug := r.PostFormValue("album")
	if !albumSlug.MatchString(slug) {
		s.renderAdmin(w, r, adminData{Error: "раздел: строчная латиница, цифры и дефис"})
		return
	}

	name, err := s.saveImage(img)
	if err != nil {
		s.internalError(w, err)
		return
	}

	photo, err := s.store.CreatePhoto(r.Context(), store.Photo{
		Name:       caption,
		URL:        "/photos/" + name,
		Album:      slug,
		Width:      img.width,
		Height:     img.height,
		Bytes:      int64(len(img.data)),
		UploadedBy: s.access.Login(r),
	})
	if err != nil {
		s.removeImageFile("/photos/" + name)
		s.internalError(w, err)
		return
	}

	s.log.Info("альбом: кадр загружен",
		"id", photo.ID, "размер", fmt.Sprintf("%dx%d", img.width, img.height), "байт", len(img.data))
	http.Redirect(w, r, "/vault/album?ok=1", http.StatusSeeOther)
}

// handleDelete убирает запись и файл загрузки; засев остаётся в статике.
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		s.renderAdmin(w, r, adminData{Error: "форма битая"})
		return
	}
	if !s.access.CSRFOK(r) {
		s.renderAdmin(w, r, adminData{Error: "форма устарела: обновите страницу"})
		return
	}

	id, err := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	if err != nil {
		s.renderAdmin(w, r, adminData{Error: "непонятный номер кадра"})
		return
	}

	photo, err := s.store.DeletePhoto(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.renderAdmin(w, r, adminData{Error: "кадр уже удалён"})
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}

	s.removeImageFile(photo.URL)
	s.log.Info("альбом: кадр удалён", "id", photo.ID, "url", photo.URL)
	http.Redirect(w, r, "/vault/album?deleted=1", http.StatusSeeOther)
}

func (s *Server) renderAdmin(w http.ResponseWriter, r *http.Request, data adminData) {
	photos, err := s.store.ListPhotos(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	albums, err := s.store.Albums(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}

	data.Login = s.access.Login(r)
	data.CSRF = s.access.CSRFToken(w, r)
	data.Albums = albums
	data.Photos = make([]adminPhoto, 0, len(photos))
	// Новые сверху: только что загруженный кадр видно без прокрутки.
	for i := len(photos) - 1; i >= 0; i-- {
		p := photos[i]
		data.Photos = append(data.Photos, adminPhoto{Photo: p, Uploaded: strings.HasPrefix(p.URL, "/photos/")})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := adminTemplate.Execute(w, data); err != nil {
		s.log.Error("альбом: шаблон не отрисован", "err", err)
	}
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.log.Error("альбом: внутренняя ошибка", "err", err)
	http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
}
