-- 0008: мета сессии для админки: адрес и браузер, последняя активность.
-- У сессий, созданных до миграции, поля пустые, заполняются при первом
-- запросе через Identify.

ALTER TABLE sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN ua TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN last_seen TEXT NOT NULL DEFAULT '';
