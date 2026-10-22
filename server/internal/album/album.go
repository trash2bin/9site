// Package album фотоальбом: снимки из базы, отдача файлов загрузок и
// закрытая страница /vault/album.
package album

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ivan223/web1-personal-site/server/internal/api"
	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// BudgetUpload имя бюджета ограничителя загрузок: контракт с main.go,
// порог поднимается переменной LIMIT_ALBUM_UPLOAD.
const BudgetUpload = "album.upload"

// decodeSlots сколько кадров готовится одновременно. Лимит частоты считает
// requests за окно, а не одновременность: десяток параллельных загрузок по
// 60 Мпикс съел бы гигабайты памяти до первого ответа.
const decodeSlots = 2

// Photo алиас записи хранилища: срез работает с ней как со своей.
type Photo = store.Photo

// Store минимум, что срезу нужно от хранилища; интерфейс объявлен у потребителя.
type Store interface {
	ListPhotos(ctx context.Context) ([]Photo, error)
	PhotoByID(ctx context.Context, id int64) (*Photo, error)
	CreatePhoto(ctx context.Context, p Photo) (*Photo, error)
	DeletePhoto(ctx context.Context, id int64) (*Photo, error)
	Tags(ctx context.Context) ([]string, error)
	TagsByPhoto(ctx context.Context) (map[int64][]string, error)
	EnsureTag(ctx context.Context, slug string) (int64, error)
	AttachTags(ctx context.Context, photoID int64, slugs []string) error
}

// Access закрытый раздел из среза auth: сессия, роль и подпись формы.
type Access interface {
	// Require пускает дальше только с живой сессией, гостя уводит на вход.
	Require(next http.Handler) http.Handler
	// Login логин авторизованного, пусто без сессии.
	Login(r *http.Request) string
	// IsAdmin пришёл ли запрос от админа: чужой кадр удаляет только он.
	IsAdmin(r *http.Request) bool
	// CSRFToken подпись формы для страницы альбома.
	CSRFToken(w http.ResponseWriter, r *http.Request) string
	// CSRFOK сверяет присланную подпись.
	CSRFOK(r *http.Request) bool
}

// Throttle ограничение частоты; реализация *webutil.Limits, собирается в main.
type Throttle interface {
	Middleware(name string, over, next http.Handler) http.Handler
}

// Config настройки среза.
type Config struct {
	Store Store
	// PhotosDir каталог файлов загрузок: картинки на диске, не в базе.
	PhotosDir string
	Access    Access
	Throttle  Throttle
	Log       *logging.Logger
}

// Server состояние среза.
type Server struct {
	store     Store
	photosDir string
	access    Access
	throttle  Throttle
	log       *logging.Logger
	decode    chan struct{}
}

// New собирает срез и создаёт каталог загрузок.
func New(cfg Config) (*Server, error) {
	if cfg.Store == nil {
		return nil, errors.New("album: нужно хранилище (Store)")
	}
	if cfg.Access == nil {
		return nil, errors.New("album: нужен закрытый раздел (Access)")
	}
	if cfg.Throttle == nil {
		return nil, errors.New("album: нужен ограничитель частоты (Throttle)")
	}
	if cfg.Log == nil {
		return nil, errors.New("album: нужен логгер (Log)")
	}
	if cfg.PhotosDir == "" {
		return nil, errors.New("album: PhotosDir обязателен")
	}
	if err := os.MkdirAll(cfg.PhotosDir, 0o755); err != nil {
		return nil, fmt.Errorf("%s: %w", cfg.PhotosDir, err)
	}

	return &Server{
		store:     cfg.Store,
		photosDir: cfg.PhotosDir,
		access:    cfg.Access,
		throttle:  cfg.Throttle,
		log:       cfg.Log,
		decode:    make(chan struct{}, decodeSlots),
	}, nil
}

// Register подключает маршруты альбома; лимит и проверка сессии вешаются
// здесь, при регистрации, а не внутри обработчиков.
func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+api.PhotosPath, s.handlePhotos)
	mux.HandleFunc("GET "+api.FilesPath+"{name}", s.handlePhotoFile)

	mux.Handle("GET /vault/album", s.access.Require(http.HandlerFunc(s.handleAdmin)))
	// Спека закрыта логином: снаружи без сессии её не видно.
	mux.Handle("GET "+api.SpecPath, s.access.Require(api.Handler()))

	// Бюджет внутри Require: иначе исчерпанный лимит рисовал бы страницу
	// загрузки гостю, ещё до проверки сессии.
	over := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.renderAdmin(w, r, adminData{Error: "слишком много загрузок: подождите несколько минут"})
	})
	mux.Handle("POST /vault/album", s.access.Require(s.throttle.Middleware(BudgetUpload,
		over, http.HandlerFunc(s.handleUpload))))
	// Свой кадр автор убирает сам, чужой только админ: роль проверяет обработчик.
	mux.Handle("POST "+api.DeletePhotoPath, s.access.Require(http.HandlerFunc(s.handleDelete)))
}

// photosFile путь файла загрузки внутри каталога.
func (s *Server) photosFile(name string) string {
	return filepath.Join(s.photosDir, name)
}
