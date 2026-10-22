package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// usersPageSize сколько аккаунтов на одной странице прав.
const usersPageSize = 50

// bootstrapAdmin заводит секретного админа из окружения: логин и пароль
// живут в deploy.env, поэтому после рестарта админ есть всегда.
func (s *Server) bootstrapAdmin() error {
	login, password := s.cfg.AdminLogin, s.cfg.AdminPassword
	if login == "" && password == "" {
		return nil
	}
	if login == "" || password == "" {
		return errors.New("auth: ADMIN_LOGIN и ADMIN_PASSWORD задаются только вместе")
	}
	if msg := validateLogin(login); msg != "" {
		return fmt.Errorf("auth: ADMIN_LOGIN: %s", msg)
	}
	if msg := validatePassword(password); msg != "" {
		return fmt.Errorf("auth: ADMIN_PASSWORD: %s", msg)
	}

	passHash, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("auth: пароль админа из окружения: %w", err)
	}

	// Старт сервера: отменять нечего, ctx запроса ещё нет.
	created, err := s.users.EnsureAdmin(context.Background(), login, passHash)
	if err != nil {
		return fmt.Errorf("auth: админ из окружения: %w", err)
	}

	if created {
		s.log.Info("админ из окружения создан", "login", login)
	} else {
		s.log.Info("админ из окружения обновлён", "login", login)
	}
	return nil
}

// usersData данные страницы управления правами.
type usersData struct {
	Users   []User
	Admins  int
	Total   int
	Page    int
	Pages   int
	Prev    int // 0 значит «назад некуда»
	Next    int
	Login   string // кто смотрит страницу
	CSRF    string
	Error   string
	Message string
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	var data usersData
	if r.URL.Query().Get("ok") == "role" {
		data.Message = "роль изменена"
	}
	data.Page = pageNumber(r.URL.Query().Get("page"))
	s.renderUsers(w, r, data)
}

// pageNumber номер страницы из запроса: мусор и нули сводятся к первой.
func pageNumber(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// handleUserRole выдаёт и снимает роль admin. Последнего админа не снимаем:
// иначе страница прав останется без того, кто может её открыть.
func (s *Server) handleUserRole(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		s.renderUsers(w, r, usersData{Error: "форма битая"})
		return
	}
	if !s.csrfOK(r) {
		s.renderUsers(w, r, usersData{Error: "форма устарела: обновите страницу"})
		return
	}

	page := pageNumber(r.PostFormValue("page"))
	login := strings.TrimSpace(r.PostFormValue("login"))
	role := r.PostFormValue("role")
	if !store.ValidRole(role) {
		s.renderUsers(w, r, usersData{Error: "непонятная роль"})
		return
	}

	ctx := r.Context()
	target, err := s.users.UserByLogin(ctx, login)
	if errors.Is(err, store.ErrNotFound) {
		s.renderUsers(w, r, usersData{Error: "такого логина нет"})
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}

	if role == store.RoleUser && target.IsAdmin() {
		admins, err := s.users.AdminCount(ctx)
		if err != nil {
			s.internalError(w, err)
			return
		}
		if admins <= 1 {
			s.renderUsers(w, r, usersData{Error: "последнего админа снять нельзя"})
			return
		}
	}

	if err := s.users.SetUserRole(ctx, login, role); err != nil {
		s.internalError(w, err)
		return
	}

	s.log.Action(s.Login(r), "роль "+login+": "+role, webutil.ClientIP(r))
	http.Redirect(w, r, fmt.Sprintf("/vault/users?ok=role&page=%d", page), http.StatusSeeOther)
}

func (s *Server) renderUsers(w http.ResponseWriter, r *http.Request, data usersData) {
	total, err := s.users.CountUsers(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}
	admins, err := s.users.AdminCount(r.Context())
	if err != nil {
		s.internalError(w, err)
		return
	}

	if data.Page < 1 {
		data.Page = 1
	}

	// Запрошенную страницу подрезаем по числу аккаунтов, чтобы /vault/users?page=999
	// не показывал пустоту вместо последней страницы.
	pages := (total + usersPageSize - 1) / usersPageSize
	if pages < 1 {
		pages = 1
	}
	if data.Page > pages {
		data.Page = pages
	}

	users, err := s.users.ListUsers(r.Context(), usersPageSize, (data.Page-1)*usersPageSize)
	if err != nil {
		s.internalError(w, err)
		return
	}

	data.Users = users
	data.Admins = admins
	data.Total = total
	data.Pages = pages
	if data.Page > 1 {
		data.Prev = data.Page - 1
	}
	if data.Page < pages {
		data.Next = data.Page + 1
	}
	data.Login = s.Login(r)
	data.CSRF = s.csrfToken(w, r)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := usersTemplate.Execute(w, data); err != nil {
		s.log.Error("шаблон users", "err", err)
	}
}
