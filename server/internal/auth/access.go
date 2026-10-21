package auth

import (
	"context"
	"net/http"
)

// loginKey ключ контекста с логином авторизованного.
type loginKey struct{}

// Require пускает дальше только с живой сессией: гостя уводит на вход,
// логин кладёт в контекст запроса для Login.
func (s *Server) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, user := s.sessionFrom(r)
		if user == nil {
			http.Redirect(w, r, "/studio?auth=login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), loginKey{}, user.Login)))
	})
}

// Login логин авторизованного; пусто, если запрос не прошёл через Require.
func (s *Server) Login(r *http.Request) string {
	login, _ := r.Context().Value(loginKey{}).(string)
	return login
}

// CSRFToken подпись формы для закрытой страницы чужого среза.
func (s *Server) CSRFToken(w http.ResponseWriter, r *http.Request) string {
	return s.csrfToken(w, r)
}

// CSRFOK сверяет подпись присланной формы с сессией.
func (s *Server) CSRFOK(r *http.Request) bool {
	return s.csrfOK(r)
}
