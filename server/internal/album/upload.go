package album

import (
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
)

const (
	// maxPhoto предел одного файла.
	maxPhoto = 5 << 20
	// maxBody запас формы поверх файла: поля и границы multipart.
	maxBody = maxPhoto + 1<<20
	// maxName длина подписи кадра.
	maxName = 100
)

// albumSlug вид раздела: строчная латиница, цифры и дефис.
var albumSlug = regexp.MustCompile(`^[a-z][a-z0-9-]{1,29}$`)

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
	if !strings.HasPrefix(url, "/photos/") {
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
