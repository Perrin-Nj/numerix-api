-- 0002_platform: platform config, plan catalog and the account purge marker.
-- (Spring: a Flyway V2__platform.sql. Never edit 0001, add files.)

-- One-row tables holding a JSON document, like the mock's `config` and `plans` keys.
-- No row means "use the built-in defaults" (see internal/app/platform_billing.go). The admin API writes them.
CREATE TABLE IF NOT EXISTS platform_config (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    json       TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE IF NOT EXISTS plan_catalog (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    json       TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Law 25: DELETE /account removes the family data at once and keeps a tombstone row until this date;
-- a purge job (BE-41) then deletes the row itself. deleted_at already exists in 0001.
ALTER TABLE parents ADD COLUMN purge_after TEXT;

-- One subscription per parent (the contract has no history endpoint yet).
CREATE UNIQUE INDEX IF NOT EXISTS idx_subscriptions_parent_unique ON subscriptions(parent_id);

-- Evidence of consent is read per child; one active row per (child, notice version).
CREATE INDEX IF NOT EXISTS idx_consents_parent ON consents(parent_id);
