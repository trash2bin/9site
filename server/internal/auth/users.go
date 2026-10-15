package auth

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/ivan223/web1-personal-site/server/internal/webutil"
)

// avatarName разрешённый вид имени файла аватара: u<id>-<случайное>.<тип>.
var (
	loginPattern = regexp.MustCompile(`^[a-zA-Z0-9]{3,30}$`)
	avatarName   = regexp.MustCompile(`^u[0-9]+-[0-9a-f]+\.(gif|png|jpg|webp)$`)
)

func validateLogin(login string) string {
	if !loginPattern.MatchString(login) {
		return "логин: 3-30 символов, только буквы латинского алфавита и цифры"
	}
	return ""
}

func validatePassword(pass string) string {
	if len(pass) < 8 {
		return "пароль должен быть не короче 8 символов"
	}
	if len(pass) > 72 {
		return "пароль должен быть не длиннее 72 символов (ограничение bcrypt)"
	}
	return ""
}

func validateEmail(email string) string {
	if !webutil.ValidEmail(email) {
		return "укажите корректный адрес почты: на него придёт ссылка для входа"
	}
	return ""
}

// cleanName убирает управляющие символы из ФИО; экранирование от XSS делает html/template.
func cleanName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
}

func hashPassword(password string) (string, error) {
	raw, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(raw), err
}

// --- пользователи: всё хранение за интерфейсом Users ---

func (s *Server) createUser(ctx context.Context, login, passHash, fullName, email string) (*User, error) {
	return s.users.CreateUser(ctx, login, passHash, fullName, email)
}

func (s *Server) userByLogin(ctx context.Context, login string) (*User, error) {
	return s.users.UserByLogin(ctx, login)
}

func (s *Server) userByID(ctx context.Context, id int64) (*User, error) {
	return s.users.UserByID(ctx, id)
}

func (s *Server) userByEmail(ctx context.Context, email string) (*User, error) {
	return s.users.UserByEmail(ctx, email)
}

func (s *Server) updateProfile(ctx context.Context, u *User, fullName, email string) error {
	return s.users.UpdateProfile(ctx, u.ID, fullName, email)
}

// updatePassword меняет пароль и отзывает все сессии: чужая cookie после
// смены пароля перестаёт работать.
func (s *Server) updatePassword(ctx context.Context, u *User, passHash string) error {
	// Сначала отзыв сессий: упав посередине, безопаснее «старый пароль без cookie», чем «новый пароль со старой чужой cookie».
	if err := s.sessions.DeleteUserSessions(ctx, u.ID); err != nil {
		return err
	}
	return s.users.UpdatePassword(ctx, u.ID, passHash)
}

func (s *Server) updateAvatar(ctx context.Context, u *User, name string) error {
	if err := s.users.UpdateAvatar(ctx, u.ID, name); err != nil {
		return err
	}
	u.Avatar = name
	return nil
}

// --- аватар ---

// avatarExt определяет расширение по содержимому: под видом аватара нельзя
// залить исполняемый файл и открыть его адрес в браузере (stored XSS).
func avatarExt(data []byte) (string, error) {
	switch http.DetectContentType(data) {
	case "image/gif":
		return "gif", nil
	case "image/png":
		return "png", nil
	case "image/jpeg":
		return "jpg", nil
	case "image/webp":
		return "webp", nil
	default:
		return "", fmt.Errorf("аватар должен быть изображением (gif, png, jpg или webp)")
	}
}

// readAvatar читает и проверяет файл. Вызывается ДО создания пользователя:
// невалидный файл ошибка формы, а не полузарегистрированный аккаунт.
func readAvatar(file multipartFile) (data []byte, ext string, err error) {
	const maxAvatar = 512 << 10 // 512 КБ с запасом

	data, err = io.ReadAll(io.LimitReader(file, maxAvatar+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxAvatar {
		return nil, "", fmt.Errorf("аватар больше 512 КБ")
	}
	ext, err = avatarExt(data)
	if err != nil {
		return nil, "", err
	}
	return data, ext, nil
}

func (s *Server) writeAvatar(u *User, data []byte, ext string) (string, error) {
	// Имя вида u<id>-<случайное>.<ext>: не пересекается с чужими файлами и
	// не содержит ничего из имени, присланного клиентом.
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return "", err
	}
	name := fmt.Sprintf("u%d-%x.%s", u.ID, suffix, ext)

	if err := os.WriteFile(filepath.Join(s.avatarDir, name), data, 0o644); err != nil {
		return "", err
	}

	if u.Avatar != "" && avatarName.MatchString(u.Avatar) {
		if err := os.Remove(filepath.Join(s.avatarDir, u.Avatar)); err != nil {
			s.log.Warn("прежний аватар не удалён", "err", err)
		}
	}

	return name, nil
}

// multipartFile хватает io.Reader, чтобы подставить свою реализацию.
type multipartFile interface {
	io.Reader
}

// avatarURL путь аватара; пустой путь значит «аватара нет».
func avatarURL(u *User) string {
	if u == nil || u.Avatar == "" {
		return ""
	}
	return "/avatars/" + u.Avatar
}

func contentTypeFor(name string) string {
	if ext := filepath.Ext(name); ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	return "application/octet-stream"
}
