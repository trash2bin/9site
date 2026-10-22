-- 0005: теги кадра. Список тегов живёт в базе, поэтому набор расширяется
-- строкой, а не правкой фронта; на кадр вешается до пяти тегов.

CREATE TABLE tags (
    id INTEGER PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE photo_tags (
    photo_id INTEGER NOT NULL REFERENCES photos (id) ON DELETE CASCADE,
    tag_id INTEGER NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (photo_id, tag_id)
);

CREATE INDEX photo_tags_tag ON photo_tags (tag_id);
