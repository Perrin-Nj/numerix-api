-- 0014 answer retries. A child may try a wrong question once more. The first answer stays in `answers` (it is the only
-- one mastery sees); the second try lives here, at most one per beat. Additive: nothing reads it unless the client
-- sends attempt 2, so the table can stay unused after a rollback of the code.
CREATE TABLE answer_retries (
    beat_id         TEXT PRIMARY KEY REFERENCES beats(id) ON DELETE CASCADE,
    session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    outcome         TEXT NOT NULL,
    json            TEXT NOT NULL DEFAULT '{}',
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE INDEX idx_answer_retries_session ON answer_retries(session_id);
