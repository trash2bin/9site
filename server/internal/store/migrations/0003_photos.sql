-- 0003: снимки фотоальбома переехали из JS-массива в базу: витрина /catalog
-- берёт их из GET /api/photos, загрузки складывают сюда метаданные.
-- Засев: 37 файлов из public/assets/img, размеры и подписи прежние.

CREATE TABLE IF NOT EXISTS photos (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  name        TEXT    NOT NULL,
  url         TEXT    NOT NULL UNIQUE,
  album       TEXT    NOT NULL,
  width       INTEGER NOT NULL,
  height      INTEGER NOT NULL,
  bytes       INTEGER NOT NULL DEFAULT 0,
  uploaded_by TEXT    NOT NULL DEFAULT '',
  created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS photos_album ON photos (album);

INSERT INTO photos (name, url, album, width, height) VALUES
  ('Nintendo 3DS', '/assets/img/console-3ds.jpg', 'gadgets', 1600, 1200),
  ('NES и контроллер', '/assets/img/console-nes-web.jpg', 'gadgets', 900, 489),
  ('Game Boy', '/assets/img/console-gameboy-web.jpg', 'gadgets', 577, 700),
  ('Двигатель', '/assets/img/auto-engine.jpg', 'garage', 800, 600),
  ('Авторазбор', '/assets/img/auto-store-web.jpg', 'garage', 900, 600),
  ('Свалка моторов', '/assets/img/junkyard-engines.jpg', 'garage', 1600, 1200),
  ('Тормозной диск', '/assets/img/part-brake-web.jpg', 'garage', 834, 700),
  ('Свечи зажигания', '/assets/img/part-spark-plugs-web.jpg', 'garage', 900, 600),
  ('Запчасти россыпью', '/assets/img/car-parts.png', 'garage', 1032, 602),
  ('Схема карбюратора', '/assets/img/part-carburetor.svg', 'garage', 1967, 1846),
  ('Tux без сна 16 часов', '/assets/img/tux-16hours.gif', 'mascots', 115, 86),
  ('Tux машет рукой', '/assets/img/tux-big.gif', 'mascots', 127, 150),
  ('Колония Linux', '/assets/img/tux-colony.gif', 'mascots', 115, 108),
  ('обнимашки бесплатны', '/assets/img/tux-cuddle.gif', 'mascots', 150, 63),
  ('kernel boot', '/assets/img/tux-kernelboot.gif', 'mascots', 100, 150),
  ('Tux майнкрафтит', '/assets/img/tux-minecraft.gif', 'mascots', 120, 117),
  ('Радужный Tux', '/assets/img/tux-rainbow.gif', 'mascots', 120, 120),
  ('Tux у серверов', '/assets/img/tux-servers.gif', 'mascots', 140, 140),
  ('Tux сидит', '/assets/img/tux-sit.gif', 'mascots', 82, 110),
  ('sudo rm -rf yourself', '/assets/img/tux-sudorm.gif', 'mascots', 150, 135),
  ('Tux и Windows', '/assets/img/tux-windows.gif', 'mascots', 120, 120),
  ('Tux с флагом', '/assets/img/tux-winflag.gif', 'mascots', 130, 122),
  ('9mice - Soloist', '/assets/img/9mice-album-soloist.jpg', 'music', 1000, 1000),
  ('9mice ч/б портрет', '/assets/img/9mice-in-face.png', 'music', 1000, 1000),
  ('Kai Angel - shh! (синий)', '/assets/img/kaiangel-album-shh1.jpg', 'music', 1200, 1200),
  ('Kai Angel - shh! (потёки)', '/assets/img/kaiangel-shh!-album.jpg', 'music', 1000, 1000),
  ('Kai Angel - DAMAGE', '/assets/img/kaiangel-album-damage.png', 'music', 300, 300),
  ('Аниме-аватар', '/assets/img/avatar-anime.gif', 'misc', 2048, 2048),
  ('under construction', '/assets/img/under-construction-real.gif', 'misc', 200, 200),
  ('construction', '/assets/img/construction-real-2.gif', 'misc', 177, 175),
  ('Искра', '/assets/img/spark.gif', 'misc', 24, 24),
  ('Кнопка-фактура', '/assets/img/texture-badge.gif', 'misc', 88, 31),
  ('Полосатая фактура', '/assets/img/texture-hyper.gif', 'misc', 642, 122),
  ('Бейдж макета', '/assets/img/badge-maket.svg', 'misc', 88, 31),
  ('Паутина', '/assets/img/cobweb.svg', 'misc', 80, 80),
  ('Паутина угол', '/assets/img/cobweb-br.svg', 'misc', 80, 80),
  ('Леопардовая фактура', '/assets/img/leopard-red.svg', 'misc', 72, 72);
