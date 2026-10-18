-- 0007_outbox: transactional outbox for the optional MongoDB sync (see docs/MONGO_SYNC.md).
-- (Spring: a Flyway V7__outbox.sql. Never edit older files, add new ones.)
--
-- A row says "this (kind, key) changed". It is inserted in the SAME write transaction as the game change, so a
-- rollback leaves no row and a commit never loses one. A worker later reads the current state, sends it to Mongo
-- and sets sent_at. kind is personal_bests, family_board or progress_stats; key is the SQLite natural key
-- (child id, parent id, or "world|skill"). No Mongo-side pseudonym is stored here.
CREATE TABLE outbox (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    kind            TEXT NOT NULL,
    key             TEXT NOT NULL,
    payload         TEXT NOT NULL DEFAULT '{}',                       -- JSON, extra hints for the resolver
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    sent_at         TEXT
);
-- Partial indexes: the worker only scans unsent rows, the cleanup only scans sent rows.
CREATE INDEX idx_outbox_due ON outbox(next_attempt_at) WHERE sent_at IS NULL;
CREATE INDEX idx_outbox_sent ON outbox(sent_at) WHERE sent_at IS NOT NULL;
