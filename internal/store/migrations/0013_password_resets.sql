-- 0013 password resets: one-time 8 digit codes an admin creates for a parent who forgot the password.
-- The code is never stored: only SHA-256(salt || code) with a per-row salt. Additive: down = DROP TABLE password_resets.
CREATE TABLE password_resets (
    id         TEXT PRIMARY KEY,
    parent_id  TEXT NOT NULL REFERENCES parents(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    salt       TEXT NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    expires_at TEXT NOT NULL,
    used_at    TEXT,
    burned_at  TEXT,
    created_by TEXT NOT NULL DEFAULT 'admin' CHECK (created_by IN ('admin'))
);
CREATE INDEX idx_password_resets_parent ON password_resets(parent_id);
