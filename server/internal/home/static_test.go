package home

// Тесты белого списка статики: проверяют, что наружу уходят только файлы
// сайта, а не каталоги и не чужие файлы выше public/. Сюда же оглавления
// каталогов — FileServer показал бы список файлов с именами и датами.
//
// Тест внутренний: staticOnly — неэкспортируемое middleware, а точность
// здесь важнее «чёрного» стиля (через ServeMux пути с ".." чистятся
// редиректом и до проверки не доходят).

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStaticOnly(t *testing.T) {
	// Контрольный обработчик: если запрос прошёл проверку, он отвечает
	// маркерным текстом.
	opened := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("открыто"))
	})
	h := staticOnly(opened)

	cases := []struct {
		path string
		code int
	}{
		// Разрешённая статика.
		{"/style.css", http.StatusOK},
		{"/design.css", http.StatusOK},
		{"/catalog.css", http.StatusOK},
		{"/print.css", http.StatusOK},
		{"/css/base.css", http.StatusOK},
		{"/css/auth/auth.css", http.StatusOK},
		{"/js/main.js", http.StatusOK},
		{"/assets/img/logo.png", http.StatusOK},

		// Чужие файлы и данные — 404.
		{"/main.go", http.StatusNotFound},
		{"/site.db", http.StatusNotFound},
		{"/visits.dat", http.StatusNotFound},
		{"/var/site.db", http.StatusNotFound},
		{"/../etc/passwd", http.StatusNotFound},
		{"/assets/../main.go", http.StatusNotFound},
		{"/assets/%2e%2e/main.go", http.StatusNotFound},

		// Оглавление каталога не отдаётся. (Корень каталога path.Clean
		// схлопывает без косой черты — запрос вида /css/auth/ отсекается
		// именно проверкой на косую черту в конце.)
		{"/css/auth/", http.StatusNotFound},
		{"/assets/img/", http.StatusNotFound},
		{"/js/", http.StatusNotFound},
	}

	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, c.path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != c.code {
			t.Errorf("%s: статус %d, ждали %d", c.path, w.Code, c.code)
		}
	}
}
