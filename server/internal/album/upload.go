package album

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ivan223/web1-personal-site/server/internal/api"
)

const (
	// maxPhoto предел одного файла.
	maxPhoto = 5 << 20
	// maxBody запас формы поверх файла: поля и границы multipart.
	maxBody = maxPhoto + 1<<20
	// maxName длина подписи кадра.
	maxName = 100
	// maxTags сколько тегов вешается на кадр.
	maxTags = 5
)

// tagSlug вид тега: короче раздела, чтобы метка влезала в карточку.
var tagSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{1,19}$`)

// Ошибки поля «теги»: показываются в форме, наружу не уходят.
var (
	errBadTag   = errors.New("тег: строчная латиница, цифры и дефис, 2-20 символов")
	errManyTags = fmt.Errorf("тегов не больше %d", maxTags)
	errNewTag   = errors.New("новый тег заводит админ: выбери из списка")
)

// parseTags разбирает поле «теги» и проверяет права на новые: строки в tags
// пишет attachTags, уже после успешной записи кадра, иначе отказ файла
// оставлял бы в базе тег, которого нет ни на одной карточке.
func (s *Server) parseTags(ctx context.Context, spec string, admin bool) ([]string, error) {
	slugs, err := splitTags(spec)
	if err != nil || len(slugs) == 0 {
		return nil, err
	}

	known, err := s.store.Tags(ctx)
	if err != nil {
		return nil, err
	}
	knownSet := make(map[string]bool, len(known))
	for _, slug := range known {
		knownSet[slug] = true
	}

	for _, slug := range slugs {
		if !knownSet[slug] && !admin {
			return nil, errNewTag
		}
	}
	return slugs, nil
}

// attachTags заводит недостающие теги и вешает их на кадр: к этому моменту
// права проверены, файл и запись уже есть.
func (s *Server) attachTags(ctx context.Context, photoID int64, slugs []string) error {
	for _, slug := range slugs {
		if _, err := s.store.EnsureTag(ctx, slug); err != nil {
			return err
		}
	}
	return s.store.AttachTags(ctx, photoID, slugs)
}

// splitTags разбирает список через запятую: пустые и повторы пропускаются.
func splitTags(spec string) ([]string, error) {
	var out []string
	seen := make(map[string]bool)
	for _, raw := range strings.Split(spec, ",") {
		slug := strings.ToLower(strings.TrimSpace(raw))
		if slug == "" || seen[slug] {
			continue
		}
		if !tagSlug.MatchString(slug) {
			return nil, errBadTag
		}
		if len(out) == maxTags {
			return nil, errManyTags
		}
		seen[slug] = true
		out = append(out, slug)
	}
	return out, nil
}

// acquireDecode занимает слот подготовки кадра; отмена запроса отпускает ожидание.
func (s *Server) acquireDecode(ctx context.Context) error {
	select {
	case s.decode <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// releaseDecode освобождает слот: вызывается через defer сразу после acquire.
func (s *Server) releaseDecode() { <-s.decode }

// readImage берёт файл из формы и готовит его к записи.
func readImage(r *http.Request) (*encoded, error) {
	file, _, err := r.FormFile("photo")
	if err != nil {
		return nil, errors.New("выбери файл картинки")
	}
	defer func() { _ = file.Close() }()

	raw, err := io.ReadAll(io.LimitReader(file, maxPhoto+1))
	if err != nil {
		return nil, fmt.Errorf("файл не прочитан: %w", err)
	}
	if len(raw) > maxPhoto {
		return nil, fmt.Errorf("файл больше %d МБ", maxPhoto>>20)
	}
	return prepareImage(raw)
}

// saveImage кладёт файл в каталог загрузок и возвращает его имя. Имя
// случайное: ничего из присланного клиентом в путь не попадает.
func (s *Server) saveImage(img *encoded) (string, error) {
	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	name := "p" + hex.EncodeToString(suffix) + "." + img.ext

	final := s.photosFile(name)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, img.data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return name, nil
}

// removeImageFile убирает файл загрузки; засев лежит в статике, его не трогаем.
func (s *Server) removeImageFile(url string) {
	if !strings.HasPrefix(url, api.FilesPath) {
		return
	}
	name := filepath.Base(url)
	if !photoFile.MatchString(name) {
		return
	}
	if err := os.Remove(s.photosFile(name)); err != nil && !os.IsNotExist(err) {
		s.log.Warn("альбом: файл не удалён", "name", name, "err", err)
	}
}

// cleanCaption чистит подпись от управляющих символов и лишней длины.
func cleanCaption(text string) string {
	text = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(text))

	if utf8.RuneCountInString(text) > maxName {
		text = string([]rune(text)[:maxName])
	}
	return text
}
