package auth

import (
	"context"
	"net/http"

	"github.com/ivan223/web1-personal-site/server/internal/store"
)

// caller авторизованный пользователь в контексте запроса.
type caller struct {
	Login string
	Role  string
}

// callerKey ключ контекста с caller.
type callerKey struct{}

// require пускает дальше по живой сессии; admin пускает только админа.
// Логин и роль попадают в контекст, читаются через Login и IsAdmin.
func (s *Server) require(admin bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, user := s.sessionFrom(r)
		if user == nil {
			http.Redirect(w, r, "/studio?auth=login", http.StatusSeeOther)
			return
		}
		if admin && !user.IsAdmin() {
			http.Error(w, "раздел только для админа", http.StatusForbidden)
			return
		}

		who := caller{Login: user.Login, Role: user.Role}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), callerKey{}, who)))
	})
}

// Require пускает дальше только с живой сессией: гостя уводит на вход.
func (s *Server) Require(next http.Handler) http.Handler {
	return s.require(false, next)
}

// RequireAdmin то же для админа: вошедшему без прав 403.
func (s *Server) RequireAdmin(next http.Handler) http.Handler {
	return s.require(true, next)
}

// Login логин авторизованного; пусто, если запрос не прошёл через Require.
func (s *Server) Login(r *http.Request) string {
	who, _ := r.Context().Value(callerKey{}).(caller)
	return who.Login
}

// IsAdmin пришёл ли запрос от админа.
func (s *Server) IsAdmin(r *http.Request) bool {
	who, _ := r.Context().Value(callerKey{}).(caller)
	return who.Role == store.RoleAdmin
}

// CSRFToken подпись формы для закрытой страницы чужого среза.
func (s *Server) CSRFToken(w http.ResponseWriter, r *http.Request) string {
	return s.csrfToken(w, r)
}

// CSRFOK сверяет подпись присланной формы с сессией.
func (s *Server) CSRFOK(r *http.Request) bool {
	return s.csrfOK(r)
}
