-- 0004: роль пользователя. admin ведёт альбом и права, user смотрит кадры,
-- грузит свои и правит профиль. CHECK не ставим: пересоздание users ради
-- него тянет внешние ключи из sessions и reset_tokens, значения проверяет Go
-- (store.ValidRole).

ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
