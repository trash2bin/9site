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
)

// Ограничения полей формы. Логин — как в методичке: буквы и цифры, 3–30.
var (
	loginPattern = regexp.MustCompile(`^[a-zA-Z0-9]{3,30}$`)
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	avatarName   = regexp.MustCompile(`^u[0-9]+-[0-9a-f]+\.(gif|png|jpg|webp)$`)
)

// Проверки полей регистрации/профиля. Возвращают текст ошибки для формы.
func validateLogin(login string) string {
	if !loginPattern.MatchString(login) {
		return "логин: 3–30 символов, только буквы латинского алфавита и цифры"
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
	if !emailPattern.MatchString(email) || len(email) > 254 {
		return "укажите корректный адрес почты — на него придёт ссылка для входа"
	}
	return ""
}

// cleanName убирает управляющие символы из ФИО и обрезает пробелы.
// Само экранирование от XSS делает html/template при выводе.
func cleanName(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
}

// hashPassword превращает пароль в bcrypt-хэш. В отличие от md5 из
// методички хэш медленный и с солью: перебор по радуге не работает.
func hashPassword(password string) (string, error) {
	raw, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(raw), err
}

// --- работа с пользователями. Всё хранение — за интерфейсом Users; здесь
// только тонкие обёртки с именами, которые знают обработчики. ---

func (s *Server) createUser(ctx context.Context, login, passHash, fullName, email string) (*User, error) {
	return s.users.CreateUser(ctx, login, passHash, fullName, email)
}

// userByLogin ищет пользователя по логину. store.ErrNotFound — «не найден».
func (s *Server) userByLogin(ctx context.Context, login string) (*User, error) {
	return s.users.UserByLogin(ctx, login)
}

// userByID ищет пользователя по идентификатору.
func (s *Server) userByID(ctx context.Context, id int64) (*User, error) {
	return s.users.UserByID(ctx, id)
}

// userByEmail ищет пользователя по почте — для сброса пароля.
func (s *Server) userByEmail(ctx context.Context, email string) (*User, error) {
	return s.users.UserByEmail(ctx, email)
}

// updateProfile меняет данные профиля (логин, как и положено, не меняется).
func (s *Server) updateProfile(ctx context.Context, u *User, fullName, email string) error {
	return s.users.UpdateProfile(ctx, u.ID, fullName, email)
}

// updatePassword меняет пароль и отзывает все сессии пользователя: чужая
// украденная cookie после смены пароля перестаёт работать. Правило отзыва
// живёт здесь, в auth, а не в хранилище пользователей — так оно переживёт
// переезд сессий в другое хранилище.
func (s *Server) updatePassword(ctx context.Context, u *User, passHash string) error {
	// Порядок — не случайный: сначала отзыв, потом пароль. Если между
	// операциями что-то упадёт, безопаснее «старый пароль, но все cookie
	// погашены» (пользователь просто введёт пароль заново), чем «новый
	// пароль со старой чужой cookie».
	if err := s.sessions.DeleteUserSessions(ctx, u.ID); err != nil {
		return err
	}
	return s.users.UpdatePassword(ctx, u.ID, passHash)
}

// updateAvatar запоминает имя нового файла аватара.
func (s *Server) updateAvatar(ctx context.Context, u *User, name string) error {
	if err := s.users.UpdateAvatar(ctx, u.ID, name); err != nil {
		return err
	}
	u.Avatar = name
	return nil
}

// --- аватар ---

// avatarExt определяет расширение по содержимому файла. Картинкой считаем
// только настоящие изображения: под видом аватара нельзя залить исполняемый
// файл и потом открыть его адрес в браузере (stored XSS).
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

// readAvatar читает и проверяет загруженный файл, возвращая содержимое
// и расширение. Вызывается ДО создания пользователя: невалидный файл —
// это ошибка формы, а не повод оставлять полузарегистрованный аккаунт
// (раньше порядок был обратным: аккаунт создавался, форма показывала
// ошибку, а повтор с исправленным файлом отвечал «логин уже занят»).
func readAvatar(file multipartFile) (data []byte, ext string, err error) {
	const maxAvatar = 512 << 10 // 512 КБ — с запасом на любую аватарку

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

// writeAvatar сохраняет уже проверенный файл под служебным именем и
// удаляет прежний. Имя генерирует сервер: имя файла от пользователя
// нигде не используется.
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

	// Прежний файл удаляем, чтобы каталог не рос.
	if u.Avatar != "" && avatarName.MatchString(u.Avatar) {
		if err := os.Remove(filepath.Join(s.avatarDir, u.Avatar)); err != nil {
			s.log.Warn("прежний аватар не удалён", "err", err)
		}
	}

	return name, nil
}

// multipartFile — то, что возвращает multipart-форма. Интерфейс нужен,
// чтобы тесты могли подставить свою реализацию.
type multipartFile interface {
	io.Reader
}

// avatarURL — путь, по которому отдаётся аватар. Пустой путь — «аватара нет».
func avatarURL(u *User) string {
	if u == nil || u.Avatar == "" {
		return ""
	}
	return "/avatars/" + u.Avatar
}

// contentTypeFor — Content-Type для отдачи аватара.
func contentTypeFor(name string) string {
	if ext := filepath.Ext(name); ext != "" {
		if t := mime.TypeByExtension(ext); t != "" {
			return t
		}
	}
	return "application/octet-stream"
}
