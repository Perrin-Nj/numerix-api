-- Numerix initial schema. Ids are UUID text, timestamps are UTC ISO-8601 text, json columns hold JSON text.
-- Names are stable: other modules extend these tables (see docs/SCHEMA.md).

CREATE TABLE parents (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    name          TEXT,
    locale        TEXT NOT NULL DEFAULT 'fr-CA',
    timezone      TEXT NOT NULL DEFAULT 'America/Toronto',
    blocked_at    TEXT,
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    deleted_at    TEXT
);

-- kind 'refresh' = rotating refresh token, 'device' = long-lived device token. Only the SHA-256 hash is stored.
CREATE TABLE refresh_tokens (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT NOT NULL REFERENCES parents(id) ON DELETE CASCADE,
    family_id  TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'refresh' CHECK (kind IN ('refresh','device')),
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    used_at    TEXT,
    revoked_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_refresh_tokens_family ON refresh_tokens(family_id);
CREATE INDEX idx_refresh_tokens_parent ON refresh_tokens(parent_id);

CREATE TABLE children (
    id                   TEXT PRIMARY KEY,
    parent_id            TEXT NOT NULL REFERENCES parents(id) ON DELETE CASCADE,
    display_name         TEXT NOT NULL,
    birth_year           INTEGER,
    grade                INTEGER,
    avatar_id            TEXT,
    pin_hash             TEXT,
    locale               TEXT NOT NULL DEFAULT 'fr-CA',
    timezone             TEXT NOT NULL DEFAULT 'America/Toronto',
    favorite_world       TEXT,
    onboarding_completed INTEGER NOT NULL DEFAULT 0,
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    deleted_at           TEXT
);
CREATE INDEX idx_children_parent ON children(parent_id);

CREATE TABLE child_settings (
    child_id   TEXT PRIMARY KEY REFERENCES children(id) ON DELETE CASCADE,
    json       TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Law 25 evidence. No foreign keys on purpose: evidence outlives the child profile (kept 2 years).
CREATE TABLE consents (
    id             TEXT PRIMARY KEY,
    parent_id      TEXT NOT NULL,
    child_id       TEXT NOT NULL,
    notice_version TEXT NOT NULL,
    notice_hash    TEXT,
    ip_prefix      TEXT,
    user_agent     TEXT,
    accepted_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    revoked_at     TEXT
);
CREATE INDEX idx_consents_child ON consents(child_id);

CREATE TABLE subscriptions (
    id                 TEXT PRIMARY KEY,
    parent_id          TEXT NOT NULL REFERENCES parents(id) ON DELETE CASCADE,
    plan_id            TEXT,
    status             TEXT NOT NULL DEFAULT 'trial',
    trial_ends_at      TEXT,
    current_period_end TEXT,
    canceled_at        TEXT,
    provider_ref       TEXT,
    json               TEXT NOT NULL DEFAULT '{}',
    created_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_subscriptions_parent ON subscriptions(parent_id);

CREATE TABLE notifications (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT NOT NULL REFERENCES parents(id) ON DELETE CASCADE,
    child_id   TEXT,
    audience   TEXT NOT NULL DEFAULT 'parent',
    kind       TEXT NOT NULL,
    json       TEXT NOT NULL DEFAULT '{}',
    read_at    TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_notifications_parent ON notifications(parent_id, created_at);

CREATE TABLE push_subscriptions (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT NOT NULL REFERENCES parents(id) ON DELETE CASCADE,
    endpoint   TEXT NOT NULL UNIQUE,
    p256dh     TEXT NOT NULL,
    auth       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Anonymised: child_hash is a salted hash, never the child id. No names, emails or answers.
CREATE TABLE analytics_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    child_hash TEXT,
    name       TEXT NOT NULL,
    json       TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Generic game tables: other modules add columns or tables through new migrations.
CREATE TABLE questions (
    id           TEXT PRIMARY KEY,
    params_hash  TEXT NOT NULL UNIQUE,
    skill_id     TEXT NOT NULL,
    json         TEXT NOT NULL DEFAULT '{}',
    served_count INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_questions_skill ON questions(skill_id);

CREATE TABLE child_questions (
    child_id    TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    question_id TEXT NOT NULL REFERENCES questions(id),
    served_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (child_id, question_id)
);

CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    child_id    TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'active',
    world_id    TEXT,
    json        TEXT NOT NULL DEFAULT '{}',
    started_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    paused_at   TEXT,
    finished_at TEXT
);
CREATE INDEX idx_sessions_child ON sessions(child_id, started_at);

CREATE TABLE beats (
    id          TEXT PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    idx         INTEGER NOT NULL,
    question_id TEXT REFERENCES questions(id),
    json        TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (session_id, idx)
);

CREATE TABLE answers (
    id              TEXT PRIMARY KEY,
    beat_id         TEXT NOT NULL REFERENCES beats(id) ON DELETE CASCADE,
    session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    idempotency_key TEXT,
    outcome         TEXT,
    json            TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (beat_id, idempotency_key)
);

-- BKT mastery state per child, as JSON.
CREATE TABLE child_profiles (
    child_id   TEXT PRIMARY KEY REFERENCES children(id) ON DELETE CASCADE,
    bkt        TEXT NOT NULL DEFAULT '{}',
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE feed_items (
    id         TEXT PRIMARY KEY,
    child_id   TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    json       TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_feed_items_child ON feed_items(child_id, created_at);

CREATE TABLE reactions (
    feed_item_id TEXT NOT NULL REFERENCES feed_items(id) ON DELETE CASCADE,
    child_id     TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    kind         TEXT NOT NULL,
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (feed_item_id, child_id)
);

CREATE TABLE collection (
    child_id    TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    card_id     TEXT NOT NULL,
    json        TEXT NOT NULL DEFAULT '{}',
    acquired_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (child_id, card_id)
);

CREATE TABLE quests (
    id         TEXT PRIMARY KEY,
    child_id   TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'active',
    json       TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_quests_child ON quests(child_id);

CREATE TABLE challenges (
    id         TEXT PRIMARY KEY,
    child_id   TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'active',
    json       TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_challenges_child ON challenges(child_id);

CREATE TABLE gifts (
    id         TEXT PRIMARY KEY,
    child_id   TEXT NOT NULL REFERENCES children(id) ON DELETE CASCADE,
    status     TEXT NOT NULL DEFAULT 'available',
    json       TEXT NOT NULL DEFAULT '{}',
    claimed_at TEXT,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_gifts_child ON gifts(child_id);
