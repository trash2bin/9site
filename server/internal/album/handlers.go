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

	"github.com/ivan223/web1-personal-site/server/internal/api"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// photoFile допустимое имя файла загрузки: генерируется сервером, снаружи
// ничего другого не отдаётся.
var photoFile = regexp.MustCompile(`^p[0-9a-f]{16}\.(gif|webp)$`)

// handlePhotos отдаёт список снимков для витрины и админки.
func (s *Server) handlePhotos(w http.ResponseWriter, r *http.Request) {
	photos, err := s.store.ListPhotos(r.Context())
	if err != nil {
		s.log.Error("альбом: список снимков не читается", "err", err)
		http.Error(w, "список снимков не читается", http.StatusInternalServerError)
		return
	}

	tags, err := s.store.Tags(r.Context())
	if err != nil {
		s.log.Error("альбом: список тегов не читается", "err", err)
		http.Error(w, "список тегов не читается", http.StatusInternalServerError)
		return
	}
	byPhoto, err := s.store.TagsByPhoto(r.Context())
	if err != nil {
		s.log.Error("альбом: теги кадров не читаются", "err", err)
		http.Error(w, "теги кадров не читаются", http.StatusInternalServerError)
		return
	}

	out := api.PhotosResponse{
		Photos: make([]api.Photo, 0, len(photos)),
		Albums: make([]api.Section, 0, len(Sections)),
		Tags:   tags,
	}
	if s.access.IsAdmin(r) {
		out.Viewer = api.Viewer{Admin: true, CSRF: s.access.CSRFToken(w, r)}
	}
	for _, section := range Sections {
		out.Albums = append(out.Albums, api.Section{Slug: section.Slug, Label: section.Label})
	}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	for _, p := range photos {
		photoTags := byPhoto[p.ID]
		if photoTags == nil {
			photoTags = []string{}
		}
		out.Photos = append(out.Photos, api.Photo{
			ID: p.ID, Name: p.Name, URL: p.URL, Album: p.Album, Width: p.Width, Height: p.Height,
			Author: authorOf(p), Tags: photoTags,
		})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.log.Error("альбом: ответ не ушёл", "err", err)
	}
}

// authorOf подпись автора для витрины: у засева автора нет, он помечен system.
func authorOf(p store.Photo) string {
	if p.UploadedBy == "" {
		return "system"
	}
	return p.UploadedBy
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
	Tags     []string
}

// adminData данные страницы загрузки.
type adminData struct {
	Photos   []adminPhoto
	Sections []Section
	Tags     []string
	Login    string
	Admin    bool
	CSRF     string
	Error    string
	Message  string
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

	if err := s.acquireDecode(r.Context()); err != nil {
		s.renderAdmin(w, r, adminData{Error: "слишком много одновременных загрузок: повтори через минуту"})
		return
	}
	defer s.releaseDecode()

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
	if !validSection(slug) {
		s.renderAdmin(w, r, adminData{Error: "раздел: выбери из списка"})
		return
	}

	// Галочки дают готовые теги из базы, поле «новые теги» есть только у админа.
	spec := r.PostFormValue("tags")
	if chosen := r.PostForm["tag"]; len(chosen) > 0 {
		spec = strings.Join(append(chosen, spec), ",")
	}
	tags, err := s.parseTags(r.Context(), spec, s.access.IsAdmin(r))
	switch {
	case errors.Is(err, errBadTag), errors.Is(err, errManyTags), errors.Is(err, errNewTag):
		s.renderAdmin(w, r, adminData{Error: err.Error()})
		return
	case err != nil:
		s.internalError(w, err)
		return
	}

	name, err := s.saveImage(img)
	if err != nil {
		s.internalError(w, err)
		return
	}

	photo, err := s.store.CreatePhoto(r.Context(), store.Photo{
		Name:       caption,
		URL:        api.FilesPath + name,
		Album:      slug,
		Width:      img.width,
		Height:     img.height,
		Bytes:      int64(len(img.data)),
		UploadedBy: s.access.Login(r),
	})
	if err != nil {
		s.removeImageFile(api.FilesPath + name)
		s.internalError(w, err)
		return
	}

	if err := s.attachTags(r.Context(), photo.ID, tags); err != nil {
		s.removeImageFile(photo.URL)
		_, _ = s.store.DeletePhoto(r.Context(), photo.ID)
		s.internalError(w, err)
		return
	}

	s.log.Info("альбом: кадр загружен",
		"id", photo.ID, "размер", fmt.Sprintf("%dx%d", img.width, img.height), "байт", len(img.data))
	http.Redirect(w, r, "/vault/album?ok=1", http.StatusSeeOther)
}

// handleDelete убирает запись и файл загрузки: свой кадр может убрать автор,
// чужой только админ. Засев из статики правит только админ.
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

	existing, err := s.store.PhotoByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.renderAdmin(w, r, adminData{Error: "кадр уже удалён"})
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	if !s.access.IsAdmin(r) && existing.UploadedBy != s.access.Login(r) {
		s.renderAdminCode(w, r, adminData{Error: "это чужой кадр: удаляет автор или админ"}, http.StatusForbidden)
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
	s.renderAdminCode(w, r, data, http.StatusOK)
}

// renderAdminCode рисует страницу загрузки с нужным статусом: отказ по
// правам отдаётся как 403, а не как успешная страница с текстом.
func (s *Server) renderAdminCode(w http.ResponseWriter, r *http.Request, data adminData, code int) {
	photos, err := s.store.ListPhotos(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}

	data.Login = s.access.Login(r)
	data.Admin = s.access.IsAdmin(r)
	data.CSRF = s.access.CSRFToken(w, r)
	data.Sections = Sections

	tags, err := s.store.Tags(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	byPhoto, err := s.store.TagsByPhoto(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	data.Tags = tags
	data.Photos = make([]adminPhoto, 0, len(photos))
	// Новые сверху: только что загруженный кадр видно без прокрутки.
	for i := len(photos) - 1; i >= 0; i-- {
		p := photos[i]
		data.Photos = append(data.Photos, adminPhoto{
			Photo:    p,
			Uploaded: strings.HasPrefix(p.URL, api.FilesPath),
			Tags:     byPhoto[p.ID],
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := adminTemplate.Execute(w, data); err != nil {
		s.log.Error("альбом: шаблон не отрисован", "err", err)
	}
}

func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.log.Error("альбом: внутренняя ошибка", "err", err)
	http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
}
