-- Admin API tables (docs/ADMIN_API.md). Append-only audit log, small key/value store for plans and platform config.

-- Why a parent was blocked by an admin (shown in the admin app).
ALTER TABLE parents ADD COLUMN blocked_reason TEXT;

-- One row per admin mutation or child-data read. Never updated, never deleted (enforced by triggers below).
CREATE TABLE admin_audit (
    id          TEXT PRIMARY KEY,
    at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    actor       TEXT NOT NULL,
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id   TEXT NOT NULL,
    detail      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_admin_audit_at ON admin_audit(at);
CREATE TRIGGER admin_audit_no_update BEFORE UPDATE ON admin_audit
BEGIN SELECT RAISE(ABORT, 'admin_audit is append-only'); END;
CREATE TRIGGER admin_audit_no_delete BEFORE DELETE ON admin_audit
BEGIN SELECT RAISE(ABORT, 'admin_audit is append-only'); END;

-- key 'plans' = plans + payment methods document, key 'config' = platform public config (incl. unpaid_policy, features).
CREATE TABLE admin_kv (
    key        TEXT PRIMARY KEY,
    json       TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
