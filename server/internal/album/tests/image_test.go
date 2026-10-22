package album_test

import (
	"bytes"
	"image/gif"
	"net/http"
	"strings"
	"testing"
)

// brokenGIF заголовок цел, а кадры не читаются: браузер показал бы битое
// полотно вместо анимации.
func brokenGIF(t *testing.T) []byte {
	t.Helper()

	good := animatedGIF(t)
	if _, err := gif.DecodeConfig(bytes.NewReader(good)); err != nil {
		t.Fatal("эталонная GIF собрана битой")
	}
	// Отрезаем вторую половину: дескриптор экрана уцелел, данные кадра нет.
	broken := good[:len(good)/2]
	_, err := gif.DecodeAll(bytes.NewReader(broken))
	if err == nil {
		t.Fatal("обрезанная GIF читается: тест не разучился ломать файл")
	}
	return broken
}

func TestUploadKeepsGoodGIF(t *testing.T) {
	env := testServer(t)

	res, _ := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "живая анимация", "album": "mascots",
	}, "alive.gif", animatedGIF(t))
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("код %d, ожидался 303", res.StatusCode)
	}

	_, body := get(t, env.ts.URL+"/api/photos")
	if !strings.Contains(string(body), ".gif") {
		t.Error("в ответе витрины нет файла анимации")
	}
}

func TestUploadRejectsBrokenGIF(t *testing.T) {
	env := testServer(t)

	res, _ := upload(t, env, map[string]string{
		"csrf": "test-csrf", "name": "битая анимация", "album": "mascots",
	}, "broken.gif", brokenGIF(t))
	if res.StatusCode == http.StatusSeeOther {
		t.Fatal("битая GIF прошла как нормальная")
	}

	_, body := get(t, env.ts.URL+"/api/photos")
	if strings.Contains(string(body), "broken.gif") {
		t.Error("битая анимация попала в ответ API")
	}
}
