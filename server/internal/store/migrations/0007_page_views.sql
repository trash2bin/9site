-- 0007: журнал просмотров страниц: кто смотрел (пустой login у анонима),
-- адрес страницы, IP, браузер и откуда пришёл.

CREATE TABLE page_views (
  id      INTEGER PRIMARY KEY AUTOINCREMENT,
  at      TEXT NOT NULL DEFAULT (datetime('now')),
  login   TEXT NOT NULL DEFAULT '',
  path    TEXT NOT NULL,
  ip      TEXT NOT NULL DEFAULT '',
  ua      TEXT NOT NULL DEFAULT '',
  referer TEXT NOT NULL DEFAULT ''
);

CREATE INDEX page_views_login_idx ON page_views (login, id);
CREATE INDEX page_views_path_idx ON page_views (path);
