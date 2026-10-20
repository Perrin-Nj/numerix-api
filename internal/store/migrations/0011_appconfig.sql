-- 0011_appconfig: runtime configuration the admin edits (docs/ADMIN_CONFIG_MATRIX.md).
-- One row per (namespace, version). The current value is the highest version. Rows are never changed or deleted,
-- so the table is also the audit trail: who (actor), when (at), old (old_value) and new (value).
CREATE TABLE app_config (
    ns        TEXT NOT NULL,
    version   INTEGER NOT NULL,
    value     TEXT NOT NULL,                 -- JSON document of the namespace
    old_value TEXT NOT NULL DEFAULT '',      -- JSON before this change ('' = nothing was saved before)
    action    TEXT NOT NULL CHECK (action IN ('set', 'rollback', 'migrate')),
    actor     TEXT NOT NULL,
    at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    PRIMARY KEY (ns, version)
);
CREATE TRIGGER app_config_no_update BEFORE UPDATE ON app_config
BEGIN SELECT RAISE(ABORT, 'app_config is append-only'); END;
CREATE TRIGGER app_config_no_delete BEFORE DELETE ON app_config
BEGIN SELECT RAISE(ABORT, 'app_config is append-only'); END;

-- Before this migration the admin app wrote admin_kv.config / admin_kv.plans while the game read platform_config /
-- plan_catalog, so those admin edits never reached the app. Carry the saved values over (admin edits win).
INSERT INTO app_config (ns, version, value, action, actor, at)
SELECT 'platform', 1, json, 'migrate', 'migration', updated_at FROM admin_kv WHERE key = 'config';
INSERT INTO app_config (ns, version, value, action, actor, at)
SELECT 'platform', 1, json, 'migrate', 'migration', updated_at FROM platform_config
WHERE id = 1 AND NOT EXISTS (SELECT 1 FROM app_config WHERE ns = 'platform');
INSERT OR REPLACE INTO plan_catalog (id, json, updated_at)
SELECT 1, json, updated_at FROM admin_kv WHERE key = 'plans';
