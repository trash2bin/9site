package auth

import "github.com/ivan223/web1-personal-site/server/internal/logging"

// Имена событий уезжают в базу: суффиксы править нельзя, только добавлять.
var reg = logging.NewRegistrar("auth")

var (
	EventLoginSucceeded       = reg.Register("login.succeeded", "успешный вход")
	EventLoginFailed          = reg.Register("login.failed", "неудачный вход")
	EventRegistered           = reg.Register("registered", "регистрация")
	EventLoggedOut            = reg.Register("logged_out", "выход")
	EventProfileUpdated       = reg.Register("profile.updated", "обновление профиля")
	EventPasswordChanged      = reg.Register("password.changed", "смена пароля")
	EventPasswordResetAsked   = reg.Register("password.reset_requested", "запрос сброса пароля")
	EventPasswordResetApplied = reg.Register("password.reset_completed", "пароль сброшен")
	EventAvatarUpdated        = reg.Register("avatar.updated", "смена аватара")
	EventRoleChanged          = reg.Register("role.changed", "смена роли")
)

// Имя system.* уехало в базу из миграции 0006, каталог среза ему не указка.
var sysReg = logging.NewForeignRegistrar("system")

var EventAdminBootstrapped = sysReg.Register("admin.bootstrapped", "бутстрап админа")
