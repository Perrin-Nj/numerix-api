-- 0004_feed: private reactions and puzzle answers of the discovery feed.
-- The feed items themselves are generated on the fly from their number (no table), so the 0001 tables
-- feed_items/reactions (which need a stored item per child) are not used.

-- One row per child and feed item while it is liked or saved; unliking and unsaving deletes the row.
-- liked_at is the moment the like started: challenge progress counts only likes after the challenge was accepted.
CREATE TABLE IF NOT EXISTS feed_reactions (
    child_id   TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    item_id    TEXT NOT NULL,
    liked      INTEGER NOT NULL DEFAULT 0,
    saved      INTEGER NOT NULL DEFAULT 0,
    liked_at   TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (child_id, item_id)
);

-- Every puzzle answer of the feed (outcome only, never the chosen value) for the puzzle_pro challenge.
CREATE TABLE IF NOT EXISTS feed_answers (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    child_id   TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    item_id    TEXT NOT NULL,
    outcome    TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_feed_answers_child ON feed_answers(child_id, created_at);
