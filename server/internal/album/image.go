package album

import (
	"bytes"
	"errors"
	"fmt"
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

// Пределы присланной картинки, проверяются по заголовку до декодирования:
// иначе 5 МБ PNG-бомбы развернулись бы в гигабайты памяти ещё до ресайза.
const (
	maxUploadSide   = 12000
	maxUploadPixels = 60_000_000
)

// errNotImage файл не подходит по содержимому: текст уходит в форму.
var errNotImage = errors.New("нужен файл png, jpeg или gif")

// errTooBig картинка больше предела декодирования.
var errTooBig = fmt.Errorf("картинка слишком большая: до %d px по стороне и %d Мпикс",
	maxUploadSide, maxUploadPixels/1_000_000)

// errGifTooBig анимацию не ужать, поэтому большой GIF просто не принимается.
var errGifTooBig = fmt.Errorf("GIF не ужимается: длинная сторона до %d px, ужми кадр перед загрузкой",
	maxSide)

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
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, errNotImage
	}
	if cfg.Width > maxUploadSide || cfg.Height > maxUploadSide ||
		int64(cfg.Width)*int64(cfg.Height) > maxUploadPixels {
		return nil, errTooBig
	}

	switch http.DetectContentType(raw) {
	case "image/gif":
		// Анимация уходит на диск байт в байт, поэтому кадр проверяется
		// тем же потолком, что и ужатые: иначе браузер декодировал бы
		// полотно в разы больше экрана.
		if max(cfg.Width, cfg.Height) > maxSide {
			return nil, errGifTooBig
		}
		// Заголовок валиден ещё не значит, что кадры читаются: битая
		// анимация иначе доедет до витрины целиком.
		frames, err := gif.DecodeAll(bytes.NewReader(raw))
		if err != nil {
			return nil, errNotImage
		}
		return &encoded{data: raw, ext: "gif", width: frames.Config.Width, height: frames.Config.Height}, nil

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
