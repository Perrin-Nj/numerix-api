-- 0008_assets: reusable images with licence, tags and the bytes themselves (docs/CONTENT_PLAN.md sections 4.2, 4.3).
-- The image bytes live in SQLite (table asset_variants) so one backup and one Litestream replica cover everything.
-- An asset id is "a_" + the first 20 hex digits of the SHA-256 of its normalised master, so the same picture
-- always gets the same id and the URL can be cached forever.
-- Its foreign keys name worlds and bank_questions, which 0009_content creates (SQLite checks them at write time).

CREATE TABLE assets (
    id                   TEXT PRIMARY KEY,
    alias                TEXT UNIQUE,            -- optional short name used by seed files ("fluent:cookie")
    kind                 TEXT NOT NULL CHECK (kind IN ('photo','illustration','sprite','lottie')),
    world_id             TEXT REFERENCES worlds(id),
    grade_band           TEXT NOT NULL DEFAULT 'all' CHECK (grade_band IN ('all','1-2','3-4','5-6')),
    mood                 TEXT,
    dominant_color       TEXT NOT NULL,
    lqip                 TEXT NOT NULL,
    width                INTEGER NOT NULL,
    height               INTEGER NOT NULL,
    alt_fr               TEXT NOT NULL,
    alt_en               TEXT NOT NULL,
    source_name          TEXT NOT NULL,
    source_url           TEXT NOT NULL,
    creator              TEXT NOT NULL,
    creator_url          TEXT,
    licence              TEXT NOT NULL CHECK (licence IN ('CC0','PDM','CC-BY-4.0','CC-BY-3.0','CC-BY-2.0','MIT','PEXELS','OWNED')),
    licence_url          TEXT NOT NULL,
    attribution_text     TEXT NOT NULL,
    attribution_required INTEGER NOT NULL DEFAULT 0,
    people_free          INTEGER NOT NULL DEFAULT 1,
    brand_free           INTEGER NOT NULL DEFAULT 1,
    review_status        TEXT NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending','approved','rejected')),
    reviewed_by          TEXT,
    reviewed_at          TEXT,
    content_hash         TEXT NOT NULL DEFAULT '',
    created_at           TEXT NOT NULL
);
CREATE INDEX idx_assets_pick ON assets(kind, review_status, world_id);

CREATE TABLE asset_tags (
    asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    facet    TEXT NOT NULL CHECK (facet IN ('world','topic','object','mood','setting')),
    value    TEXT NOT NULL,
    PRIMARY KEY (asset_id, facet, value)
);
CREATE INDEX idx_asset_tags_lookup ON asset_tags(facet, value);

-- Controlled vocabulary: media criteria may only use these values (no free tags).
CREATE TABLE asset_vocab (
    facet    TEXT NOT NULL,
    value    TEXT NOT NULL,
    label_fr TEXT,
    label_en TEXT,
    parent   TEXT,
    PRIMARY KEY (facet, value)
);

-- The bytes. WITHOUT ROWID keeps big BLOBs out of the way of the small hot columns of other tables.
CREATE TABLE asset_variants (
    asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    w        INTEGER NOT NULL,
    format   TEXT NOT NULL DEFAULT 'webp',
    bytes    BLOB NOT NULL,
    size     INTEGER NOT NULL,
    PRIMARY KEY (asset_id, w, format)
) WITHOUT ROWID;

-- A question's media slots: an explicit asset (asset_ref is the id or alias as written) or criteria (JSON).
CREATE TABLE question_media (
    question_id   TEXT NOT NULL REFERENCES bank_questions(id) ON DELETE CASCADE,
    role          TEXT NOT NULL,
    ord           INTEGER NOT NULL DEFAULT 0,
    asset_ref     TEXT,
    asset_id      TEXT REFERENCES assets(id),
    criteria_json TEXT,
    PRIMARY KEY (question_id, role, ord)
);
CREATE INDEX idx_question_media_asset ON question_media(asset_id) WHERE asset_id IS NOT NULL;
