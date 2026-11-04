package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/ivan223/web1-personal-site/server/internal/logging"
	"github.com/ivan223/web1-personal-site/server/internal/store"
	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// handleStudio витрина закрытого раздела, открыта намеренно: проверка
// живёт в handleVault, там по прямой ссылке без входа не попасть.
func (s *Server) handleStudio(w http.ResponseWriter, r *http.Request) {
	_, user := s.sessionFrom(r)

	data := studioData{CSRF: s.csrfToken(w, r)}
	if user != nil {
		data.User = user
		data.Avatar = avatarURL(user)
	} else {
		switch open := r.URL.Query().Get("auth"); open {
		case "login", "register", "forgot":
			data.Open = open
		}
		if r.URL.Query().Get("reset") == "1" {
			data.Message = "пароль обновлён: войдите с новым паролем"
		}
	}

	s.renderStudio(w, data)
}

func (s *Server) handleVault(w http.ResponseWriter, r *http.Request) {
	_, user := s.sessionFrom(r)
	if user == nil {
		http.Redirect(w, r, "/studio?auth=login", http.StatusSeeOther)
		return
	}

	s.renderVault(w, studioData{
		User:   user,
		Avatar: avatarURL(user),
		CSRF:   s.csrfToken(w, r),
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	// Лимит висит в Register, здесь ip только для журнала.
	ip := webutil.ClientIP(r)

	login := strings.TrimSpace(r.PostFormValue("login"))
	password := r.PostFormValue("password")

	if !s.csrfOK(r) {
		s.loginWall(w, r, "форма устарела: попробуйте ещё раз")
		return
	}

	user, err := s.userByLogin(r.Context(), login)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		// Сбой хранилища не «неверный пароль»: иначе поломка маскируется
		// под опечатку пользователя и не видна в логах.
		s.log.Error("login: storage unavailable", "err", err)
		s.loginWall(w, r, "не удалось войти: попробуйте позже")
		return
	}
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte(password)) != nil {
		// Одно и то же сообщение на неверный логин и неверный пароль:
		// иначе форма превращается в перечислитель существующих логинов.
		// В журнал попадает обрезанный логин: валидация в этой ветке не прошла.
		s.log.Action(logging.Action{
			Login: webutil.TruncateRunes(login, 30), Event: EventLoginFailed, Result: logging.ResultFailure, IP: ip,
		})
		s.loginWall(w, r, "неверный логин или пароль")
		return
	}

	if err := s.startSession(r.Context(), w, r, user); err != nil {
		s.log.Error("session not created", "err", err)
		s.loginWall(w, r, "не удалось войти: попробуйте позже")
		return
	}

	s.log.Action(logging.Action{
		Login: user.Login, Event: EventLoginSucceeded, Result: logging.ResultSuccess, IP: ip,
	})
	http.Redirect(w, r, "/studio", http.StatusSeeOther)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	ip := webutil.ClientIP(r)

	if !s.csrfOK(r) {
		s.loginWall(w, r, "форма устарела: попробуйте ещё раз")
		return
	}

	login := strings.TrimSpace(r.PostFormValue("login"))
	password := r.PostFormValue("password")
	fullName := cleanName(r.PostFormValue("full_name"))
	email := strings.TrimSpace(r.PostFormValue("email"))

	errText := firstError(
		validateLogin(login),
		validatePassword(password),
		validateEmail(email),
	)
	if errText != "" {
		s.renderStudio(w, studioData{
			Error: errText,
			Open:  "register",
			CSRF:  s.csrfToken(w, r),
		})
		return
	}

	passHash, err := hashPassword(password)
	if err != nil {
		s.internalError(w, err)
		return
	}

	// Аватар проверяем ДО создания аккаунта: иначе повтор с исправленным
	// файлом отвечал бы «логин уже занят» на полузарегистрированном аккаунте.
	var (
		avatarData []byte
		avatarKind string
	)
	if file, _, err := r.FormFile("avatar"); err == nil {
		defer func() { _ = file.Close() }()
		if avatarData, avatarKind, err = readAvatar(file); err != nil {
			s.renderStudio(w, studioData{
				Error: err.Error(),
				Open:  "register",
				CSRF:  s.csrfToken(w, r),
			})
			return
		}
	}

	user, err := s.createUser(r.Context(), login, passHash, fullName, email)
	if errors.Is(err, errLoginTaken) {
		s.renderStudio(w, studioData{
			Error: "такой логин уже занят: придумайте другой",
			Open:  "register",
			CSRF:  s.csrfToken(w, r),
		})
		return
	}
	if errors.Is(err, errEmailTaken) {
		s.renderStudio(w, studioData{
			Error: "этот почтовый адрес уже зарегистрирован: восстановите через него пароль или укажите другой",
			Open:  "register",
			CSRF:  s.csrfToken(w, r),
		})
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}

	if avatarData != nil {
		name, err := s.writeAvatar(user, avatarData, avatarKind)
		if err != nil {
			s.internalError(w, err)
			return
		}
		if err := s.updateAvatar(r.Context(), user, name); err != nil {
			s.internalError(w, err)
			return
		}
	}

	s.log.Action(logging.Action{
		Login: user.Login, Event: EventRegistered, Result: logging.ResultSuccess, IP: ip,
	})
	if err := s.startSession(r.Context(), w, r, user); err != nil {
		s.internalError(w, err)
		return
	}
	http.Redirect(w, r, "/studio", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if !s.csrfOK(r) {
		http.Redirect(w, r, "/studio", http.StatusSeeOther)
		return
	}

	ip := webutil.ClientIP(r)
	_, user := s.sessionFrom(r)
	if cookie, err := r.Cookie(cookieSID); err == nil {
		s.deleteSession(r.Context(), cookie.Value)
	}
	setCookie(w, r, cookieSID, "", -1)

	if user != nil {
		s.log.Action(logging.Action{
			Login: user.Login, Event: EventLoggedOut, Result: logging.ResultSuccess, IP: ip,
		})
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleProfile смена регистрационных данных; логин не меняется.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	_, user := s.sessionFrom(r)
	if user == nil {
		http.Redirect(w, r, "/studio?auth=login", http.StatusSeeOther)
		return
	}
	if !s.csrfOK(r) {
		http.Redirect(w, r, "/studio", http.StatusSeeOther)
		return
	}

	fullName := cleanName(r.PostFormValue("full_name"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	newPassword := r.PostFormValue("new_password")
	ip := webutil.ClientIP(r)
	profileFields := changedProfileFields(user, fullName, email)

	if errText := firstError(validateEmail(email)); errText != "" {
		s.renderStudio(w, studioData{User: user, Avatar: avatarURL(user), Error: errText, CSRF: s.csrfToken(w, r)})
		return
	}

	if newPassword != "" {
		if errText := validatePassword(newPassword); errText != "" {
			s.renderStudio(w, studioData{User: user, Avatar: avatarURL(user), Error: errText, CSRF: s.csrfToken(w, r)})
			return
		}
		if bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte(r.PostFormValue("cur_password"))) != nil {
			s.renderStudio(w, studioData{User: user, Avatar: avatarURL(user), Error: "текущий пароль указан неверно", CSRF: s.csrfToken(w, r)})
			return
		}

		passHash, err := hashPassword(newPassword)
		if err != nil {
			s.internalError(w, err)
			return
		}
		if err := s.updatePassword(r.Context(), user, passHash); err != nil {
			s.internalError(w, err)
			return
		}
		s.log.Action(logging.Action{
			Login: user.Login, Event: EventPasswordChanged, Result: logging.ResultSuccess, IP: ip,
		})
		// updatePassword гасит все сессии, включая текущую: без новой
		// сессии пользователь вылетит сразу после смены пароля.
		if err := s.startSession(r.Context(), w, r, user); err != nil {
			s.internalError(w, err)
			return
		}
	}

	if err := s.updateProfile(r.Context(), user, fullName, email); err != nil {
		if errors.Is(err, errEmailTaken) {
			s.renderStudio(w, studioData{User: user, Avatar: avatarURL(user),
				Error: "этот почтовый адрес уже занят другим аккаунтом", CSRF: s.csrfToken(w, r)})
			return
		}
		s.internalError(w, err)
		return
	}
	if profileFields != "" {
		s.log.Action(logging.Action{
			Login: user.Login, Event: EventProfileUpdated, Target: profileFields,
			Result: logging.ResultSuccess, IP: ip,
		})
	}
	user.FullName, user.Email = fullName, email

	if file, _, err := r.FormFile("avatar"); err == nil {
		defer func() { _ = file.Close() }()
		data, kind, err := readAvatar(file)
		if err != nil {
			s.renderStudio(w, studioData{User: user, Avatar: avatarURL(user), Error: err.Error(), CSRF: s.csrfToken(w, r)})
			return
		}
		name, err := s.writeAvatar(user, data, kind)
		if err != nil {
			s.internalError(w, err)
			return
		}
		if err := s.updateAvatar(r.Context(), user, name); err != nil {
			s.internalError(w, err)
			return
		}
		s.log.Action(logging.Action{
			Login: user.Login, Event: EventAvatarUpdated, Target: name,
			Result: logging.ResultSuccess, IP: ip,
		})
	}

	s.renderStudio(w, studioData{
		User:    user,
		Avatar:  avatarURL(user),
		Message: "данные сохранены",
		CSRF:    s.csrfToken(w, r),
	})
}

// handleForgot заявка на восстановление пароля; ответ всегда одинаковый,
// иначе форма выдала бы, какие почты зарегистрированы на сайте.
func (s *Server) handleForgot(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	ip := webutil.ClientIP(r)

	if !s.csrfOK(r) {
		s.loginWall(w, r, "форма устарела: попробуйте ещё раз")
		return
	}

	email := strings.TrimSpace(r.PostFormValue("email"))
	// Аккаунт с пустой почтой (админ из env) не должен получить токен сброса.
	if msg := validateEmail(email); msg != "" {
		s.renderStudio(w, studioData{Error: msg, Open: "forgot", CSRF: s.csrfToken(w, r)})
		return
	}
	user, err := s.userByEmail(r.Context(), email)

	link := ""
	var mailErr error
	if err == nil {
		resetToken, err := s.createResetToken(r.Context(), user.ID)
		if err != nil {
			s.internalError(w, err)
			return
		}

		link = s.cfg.BaseURL + "/reset?token=" + resetToken
		body := fmt.Sprintf(
			"Здравствуйте, %s!\n\n"+
				"Кто-то (надеемся, вы) запросил смену пароля на сайте %s.\n"+
				"Перейдите по ссылке, чтобы задать новый пароль:\n\n%s\n\n"+
				"Ссылка действует один час и только один раз.\n"+
				"Если вы ничего не запрашивали, просто проигнорируйте письмо.\n",
			user.Login, s.cfg.BaseURL, link,
		)

		s.log.Action(logging.Action{
			Login: user.Login, Event: EventPasswordResetAsked,
			Result: logging.ResultSuccess, IP: ip,
		})
		mailErr = s.sendMail(r.Context(), user.Email, "Смена пароля на "+s.cfg.BaseURL, body)
		if mailErr != nil {
			s.log.Error("password reset email not sent", "to", user.Email, "err", mailErr)
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		s.internalError(w, err)
		return
	}

	// Ссылку на экране отдаём только localhost: на публичном сайте любой
	// мог бы запросить сброс чужого аккаунта и получить рабочую ссылку.
	data := studioData{
		Message: "если почта указана верно: письмо со ссылкой отправлено",
		CSRF:    s.csrfToken(w, r),
	}
	if link != "" && (!s.cfg.SMTP.Configured() || mailErr != nil) {
		loopback := isLoopback(ip)
		switch {
		case loopback && mailErr != nil:
			data.ResetLink = link
			data.Message = "письмо не ушло: ссылка показана здесь и сохранена в файл"
		case loopback:
			data.ResetLink = link
			data.Message = "SMTP не настроен, поэтому ссылка показана здесь и сохранена в файл"
		case mailErr != nil:
			s.log.Warn("password reset email not delivered, link not shown, request was not from localhost",
				"login", user.Login, "err", mailErr)
			data.Message = "письмо не удалось отправить: напишите администратору"
		default:
			// Почта не настроена, а не «письмо не ушло»: сообщения должны
			// отличаться, иначе администратор ищет поломку там, где её нет.
			s.log.Warn("password reset SMTP not configured, link not shown, request was not from localhost",
				"login", user.Login)
			data.Message = "восстановление пароля пока не настроено: напишите администратору"
		}
	}
	s.renderStudio(w, data)
}

func (s *Server) handleResetForm(w http.ResponseWriter, r *http.Request) {
	tokenValue := r.URL.Query().Get("token")
	if !tokenPattern.MatchString(tokenValue) {
		http.Redirect(w, r, "/studio?auth=login", http.StatusSeeOther)
		return
	}
	s.renderReset(w, resetData{Token: tokenValue, CSRF: s.csrfToken(w, r)})
}

func (s *Server) handleResetApply(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	tokenValue := r.PostFormValue("token")
	password := r.PostFormValue("password")

	if !s.csrfOK(r) {
		s.renderReset(w, resetData{Token: tokenValue, Error: "форма устарела: откройте ссылку из письма заново", CSRF: s.csrfToken(w, r)})
		return
	}
	if !tokenPattern.MatchString(tokenValue) {
		s.renderReset(w, resetData{Error: "ссылка недействительна или уже использована", CSRF: s.csrfToken(w, r)})
		return
	}
	if errText := validatePassword(password); errText != "" {
		s.renderReset(w, resetData{Token: tokenValue, Error: errText, CSRF: s.csrfToken(w, r)})
		return
	}

	userID, err := s.consumeResetToken(r.Context(), tokenValue)
	if errors.Is(err, store.ErrNotFound) {
		s.renderReset(w, resetData{Error: "ссылка недействительна или уже использована", CSRF: s.csrfToken(w, r)})
		return
	}
	if err != nil {
		// Сбой хранилища, а не «ссылка сгорела»: правдивое сообщение и сбой в лог.
		s.log.Error("password reset: storage unavailable", "err", err)
		s.renderReset(w, resetData{Error: "не удалось проверить ссылку: попробуйте позже", CSRF: s.csrfToken(w, r)})
		return
	}

	user, err := s.userByID(r.Context(), userID)
	if err != nil {
		s.internalError(w, err)
		return
	}
	passHash, err := hashPassword(password)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.updatePassword(r.Context(), user, passHash); err != nil {
		s.internalError(w, err)
		return
	}

	s.log.Action(logging.Action{
		Login: user.Login, Event: EventPasswordResetApplied,
		Result: logging.ResultSuccess, IP: webutil.ClientIP(r),
	})
	http.Redirect(w, r, "/studio?auth=login&reset=1", http.StatusSeeOther)
}

// handleAvatar отдаёт файлы аватаров. Наружу выходит только имя, прошедшее
// строгую проверку: выйти за каталог или подсунуть чужой файл нельзя.
func (s *Server) handleAvatar(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Path)
	if !avatarName.MatchString(name) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", contentTypeFor(name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.avatarDir, name))
}

// --- внутренние помощники обработчиков ---

func (s *Server) startSession(ctx context.Context, w http.ResponseWriter, r *http.Request, user *User) error {
	tokenValue, _, err := s.createSession(ctx, user.ID)
	if err != nil {
		return err
	}
	setCookie(w, r, cookieSID, tokenValue, int(sessionTTL.Seconds()))
	return nil
}

// sessionFrom достаёт сессию и пользователя по cookie. Ошибку хранилища
// не путаем с отсутствием сессии: иначе падение базы выглядит как
// молчаливый разлогин всех.
func (s *Server) sessionFrom(r *http.Request) (*Session, *User) {
	ctx := r.Context()

	cookie, err := r.Cookie(cookieSID)
	if err != nil || cookie.Value == "" {
		return nil, nil
	}

	sess, err := s.sessionByToken(ctx, cookie.Value)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		s.log.Error("session cannot be read", "err", err)
		return nil, nil
	}

	user, err := s.userByID(ctx, sess.UserID)
	if errors.Is(err, store.ErrNotFound) {
		// Пользователь удалён, а сессия осталась: приберём её.
		s.deleteSession(ctx, cookie.Value)
		return nil, nil
	}
	if err != nil {
		s.log.Error("session user cannot be read", "err", err)
		return nil, nil
	}
	return sess, user
}

// csrfToken отдаёт подпись форм; у авторизованного она привязана к сессии
// и не подделывается даже при подмене cookie.
func (s *Server) csrfToken(w http.ResponseWriter, r *http.Request) string {
	if sess, _ := s.sessionFrom(r); sess != nil {
		return sess.CSRF
	}

	if cookie, err := r.Cookie(cookieCSRF); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	value, err := token()
	if err != nil {
		return ""
	}
	setCookie(w, r, cookieCSRF, value, int(sessionTTL.Seconds()))
	return value
}

// csrfOK сверяет подпись: у авторизованного с подписью сессии, у гостя
// с cookie (double submit). Сравнение в константное время, чтобы подбор
// не определялся по времени ответа.
func (s *Server) csrfOK(r *http.Request) bool {
	got := r.PostFormValue("csrf")
	if got == "" {
		return false
	}

	if sess, _ := s.sessionFrom(r); sess != nil &&
		subtle.ConstantTimeCompare([]byte(sess.CSRF), []byte(got)) == 1 {
		return true
	}

	cookie, err := r.Cookie(cookieCSRF)
	return err == nil && cookie.Value != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(got)) == 1
}

func isLoopback(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

func (s *Server) loginWall(w http.ResponseWriter, r *http.Request, errText string) {
	s.renderStudio(w, studioData{
		Error: errText,
		Open:  "login",
		CSRF:  s.csrfToken(w, r),
	})
}

// internalError непредвиденная ошибка: детали в ответ не уходят.
func (s *Server) internalError(w http.ResponseWriter, err error) {
	s.log.Error("internal error", "err", err)
	http.Error(w, "внутренняя ошибка сервера", http.StatusInternalServerError)
}

func firstError(errs ...string) string {
	for _, e := range errs {
		if e != "" {
			return e
		}
	}
	return ""
}

func changedProfileFields(user *User, fullName, email string) string {
	var fields []string
	if user.FullName != fullName {
		fields = append(fields, "full_name")
	}
	if user.Email != email {
		fields = append(fields, "email")
	}
	return strings.Join(fields, ",")
}

func (s *Server) renderStudio(w http.ResponseWriter, data studioData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := studioTemplate.Execute(w, data); err != nil {
		s.log.Error("studio template failed", "err", err)
	}
}

func (s *Server) renderReset(w http.ResponseWriter, data resetData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := resetTemplate.Execute(w, data); err != nil {
		s.log.Error("reset template failed", "err", err)
	}
}

func (s *Server) renderVault(w http.ResponseWriter, data studioData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := vaultTemplate.Execute(w, data); err != nil {
		s.log.Error("vault template failed", "err", err)
	}
}
