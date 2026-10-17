-- Game module (engine, question bank, narrator). Tables questions, child_questions, sessions, beats, answers
-- and child_profiles already exist from 0001; this file only adds what the game needs on top.

-- Exercise family of a question (set by the generator). The picker avoids the shape of the previous beat,
-- so two consecutive beats never share a shape and a wrong answer leads to a different kind of exercise.
ALTER TABLE questions ADD COLUMN shape TEXT NOT NULL DEFAULT '';
CREATE INDEX idx_questions_skill_shape ON questions(skill_id, shape);

-- Cached MethodVisual JSON, one row per (kind, params, language): the cache_key sent to the client.
CREATE TABLE question_visuals (
    cache_key  TEXT PRIMARY KEY,
    json       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Cached narrations produced by an LLM provider, one row per (question, world, language, phase, outcome,
-- prompt version). Canned texts are cheap and deterministic, so they are not stored.
CREATE TABLE narrations (
    cache_key  TEXT PRIMARY KEY,
    world      TEXT NOT NULL,
    lang       TEXT NOT NULL,
    phase      TEXT NOT NULL,
    text       TEXT NOT NULL,
    source     TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Fast lookups used by the progress, summary and growth screens.
CREATE INDEX idx_answers_session ON answers(session_id, created_at);
