-- 0010_feed_media: the curated Discover items (200 real photos, animations and short captioned videos).
-- The media files themselves live on disk (MEDIA_DIR); this table holds the texts and the file references.
-- Loaded and kept in sync by cmd/feedseed from seed/feed/items.json. A child's likes and saves reuse feed_reactions
-- (item id "m-<id>"), so nothing per child is stored here (Law 25: no personal data in this table).
CREATE TABLE IF NOT EXISTS feed_media (
    id         TEXT PRIMARY KEY,
    kind       TEXT NOT NULL CHECK (kind IN ('photo', 'animation', 'video')),
    world      TEXT NOT NULL,
    topic      TEXT NOT NULL,
    grade_min  INTEGER NOT NULL CHECK (grade_min BETWEEN 1 AND 6),
    sort       INTEGER NOT NULL,        -- position in the seed file, the stable base order
    body       TEXT NOT NULL,           -- JSON: texts fr/en, tags, file names, size, licence, attribution
    checksum   TEXT NOT NULL,           -- sha256 of body, so a re-run writes only what changed
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_feed_media_grade ON feed_media(grade_min, sort);
