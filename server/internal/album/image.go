package album

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"net/http"

	"github.com/gen2brain/webp"
	"golang.org/x/image/draw"
)

// maxSide длинная сторона сохраняемого кадра: больше не нужно ни экрану, ни трафику.
const maxSide = 1600

// errNotImage файл не подходит по содержимому: текст уходит в форму.
var errNotImage = errors.New("нужен файл png, jpeg или gif")

// encoded готовый к записи файл: байты, расширение и размеры итогового кадра.
type encoded struct {
	data   []byte
	ext    string
	width  int
	height int
}

// prepareImage проверяет картинку по содержимому и готовит файл для диска:
// GIF остаётся собой (иначе умрёт анимация), PNG и JPEG ужимаются в WebP.
func prepareImage(raw []byte) (*encoded, error) {
	switch http.DetectContentType(raw) {
	case "image/gif":
		cfg, err := gif.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			return nil, errNotImage
		}
		return &encoded{data: raw, ext: "gif", width: cfg.Width, height: cfg.Height}, nil

	case "image/png", "image/jpeg":
		img, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, errNotImage
		}
		img = shrink(img, maxSide)

		var buf bytes.Buffer
		if err := webp.Encode(&buf, img, webp.Options{Quality: 82, Lossless: hasAlpha(img)}); err != nil {
			return nil, err
		}
		b := img.Bounds()
		return &encoded{data: buf.Bytes(), ext: "webp", width: b.Dx(), height: b.Dy()}, nil
	}

	return nil, errNotImage
}

// shrink ужимает кадр до стороны side с сохранением пропорций; мелкий отдаётся как есть.
func shrink(img image.Image, side int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= side && h <= side {
		return img
	}

	scale := float64(side) / float64(max(w, h))
	nw := max(1, int(math.Round(float64(w)*scale)))
	nh := max(1, int(math.Round(float64(h)*scale)))

	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// hasAlpha прозрачность: такой кадр кодируется без потерь, иначе качество 82
// испортило бы мягкие края.
func hasAlpha(img image.Image) bool {
	o, ok := img.(interface{ Opaque() bool })
	return ok && !o.Opaque()
}
