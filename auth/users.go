package auth

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// User — запись таблицы users.
type User struct {
	ID       int64
	Login    string
	PassHash string
	FullName string
	Email    string
	Avatar   string
}

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

// --- запросы к базе. Везде плейсхолдеры $N: подстановки строкой нет,
// поэтому SQL-инъекция через поля форм невозможна (контрольный вопрос N7). ---

// createUser добавляет пользователя и возвращает его запись.
func (s *Server) createUser(login, passHash, fullName, email string) (*User, error) {
	res, err := s.db.Exec(
		`INSERT INTO users (login, pass_hash, full_name, email) VALUES ($1, $2, $3, $4)`,
		login, passHash, fullName, email,
	)
	if err != nil {
		// UNIQUE по логину — естественный источник ошибки «логин занят».
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errLoginTaken
		}
		return nil, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &User{ID: id, Login: login, PassHash: passHash, FullName: fullName, Email: email}, nil
}

// errLoginTaken — сигнал «логин уже занят» наверх, в форму.
var errLoginTaken = errors.New("login taken")

// userByLogin ищет пользователя по логину. sql.ErrNoRows — «не найден».
func (s *Server) userByLogin(login string) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, login, pass_hash, full_name, email, avatar FROM users WHERE login = $1`,
		login,
	))
}

// userByID ищет пользователя по идентификатору.
func (s *Server) userByID(id int64) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, login, pass_hash, full_name, email, avatar FROM users WHERE id = $1`,
		id,
	))
}

// userByEmail ищет пользователя по почте — для сброса пароля.
func (s *Server) userByEmail(email string) (*User, error) {
	return s.scanUser(s.db.QueryRow(
		`SELECT id, login, pass_hash, full_name, email, avatar FROM users WHERE email = $1 COLLATE NOCASE`,
		email,
	))
}

func (s *Server) scanUser(row *sql.Row) (*User, error) {
	var u User
	if err := row.Scan(&u.ID, &u.Login, &u.PassHash, &u.FullName, &u.Email, &u.Avatar); err != nil {
		return nil, err
	}
	return &u, nil
}

// updateProfile меняет данные профиля (логин, как и положено, не меняется).
func (s *Server) updateProfile(u *User, fullName, email string) error {
	_, err := s.db.Exec(
		`UPDATE users SET full_name = $1, email = $2 WHERE id = $3`,
		fullName, email, u.ID,
	)
	return err
}

// updatePassword меняет пароль и отозвает все сессии пользователя: чужая
// украденная cookie после смены пароля перестаёт работать.
func (s *Server) updatePassword(u *User, passHash string) error {
	if _, err := s.db.Exec(`UPDATE users SET pass_hash = $1 WHERE id = $2`, passHash, u.ID); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = $1`, u.ID)
	return err
}

// updateAvatar запоминает имя нового файла аватара.
func (s *Server) updateAvatar(u *User, name string) error {
	_, err := s.db.Exec(`UPDATE users SET avatar = $1 WHERE id = $2`, name, u.ID)
	if err == nil {
		u.Avatar = name
	}
	return err
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

// saveAvatar проверяет и сохраняет загруженный файл под служебным именем.
// Имя генерирует сервер: имя файла от пользователя нигде не используется.
func (s *Server) saveAvatar(u *User, file multipartFile, filename string) (string, error) {
	const maxAvatar = 512 << 10 // 512 КБ — с запасом на любую аватарку

	data, err := io.ReadAll(io.LimitReader(file, maxAvatar+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxAvatar {
		return "", fmt.Errorf("аватар больше 512 КБ")
	}
	ext, err := avatarExt(data)
	if err != nil {
		return "", err
	}

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
		os.Remove(filepath.Join(s.avatarDir, u.Avatar))
	}
	_ = filename

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

// nowPlus — метка времени для сроков годности (формат базы: UTC RFC3339).
func nowPlus(d time.Duration) string {
	return time.Now().UTC().Add(d).Format(time.RFC3339)
}
