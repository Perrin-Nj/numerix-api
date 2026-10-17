-- 0006_devices: known devices (cookie sessions), passkeys (WebAuthn) and their short-lived ceremony state.
-- (Spring: a Flyway V6__devices.sql. Never edit older files, add new ones.)

-- A device is one browser a parent signed in on. The cookie value is 256 random bits; only its SHA-256 is stored.
-- The cookie is rotated on each use, so token_hash changes; an old value then matches nothing (reuse is refused).
CREATE TABLE devices (
    id                TEXT PRIMARY KEY,
    parent_id         TEXT NOT NULL REFERENCES parents(id) ON DELETE CASCADE,
    token_hash        TEXT NOT NULL UNIQUE,
    label             TEXT NOT NULL,                 -- for example "Chrome on Windows"
    user_agent_family TEXT NOT NULL,                 -- Chrome, Safari, Firefox, Edge or Other (never the full user agent)
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_used_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    expires_at        TEXT NOT NULL,                 -- slides forward on each rotation
    revoked_at        TEXT
);
CREATE INDEX idx_devices_parent ON devices(parent_id);

-- One row per passkey. credential_id is what the browser sends back; public_key is the COSE key.
CREATE TABLE passkeys (
    id                TEXT PRIMARY KEY,
    parent_id         TEXT NOT NULL REFERENCES parents(id) ON DELETE CASCADE,
    credential_id     BLOB NOT NULL UNIQUE,
    public_key        BLOB NOT NULL,
    sign_count        INTEGER NOT NULL DEFAULT 0,
    aaguid            BLOB,
    attestation_type  TEXT NOT NULL DEFAULT '',
    transports        TEXT NOT NULL DEFAULT '[]',    -- JSON array of strings
    user_verified     INTEGER NOT NULL DEFAULT 0,
    backup_eligible   INTEGER NOT NULL DEFAULT 0,
    backup_state      INTEGER NOT NULL DEFAULT 0,
    nickname          TEXT NOT NULL,
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_used_at      TEXT
);
CREATE INDEX idx_passkeys_parent ON passkeys(parent_id);

-- Server-side state of each Begin call (challenge etc.), valid 5 minutes, deleted when used.
CREATE TABLE webauthn_sessions (
    id         TEXT PRIMARY KEY,                     -- random, sent to the browser as session_id
    kind       TEXT NOT NULL CHECK (kind IN ('register','login')),
    parent_id  TEXT,                                 -- set for register, NULL for discoverable login
    data       TEXT NOT NULL,                        -- JSON of webauthn.SessionData
    expires_at TEXT NOT NULL
);
CREATE INDEX idx_webauthn_sessions_expires ON webauthn_sessions(expires_at);
