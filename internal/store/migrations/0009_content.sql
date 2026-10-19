-- 0009_content: the curated question bank (docs/CONTENT_PLAN.md section 3.1).
-- (Spring: a Flyway V9__content.sql. Never edit older files, add new ones.)
--
-- worlds and world_quests (the old "quests" table is the daily quest of 0001) are the catalogue (20 worlds, 89 quests after the seed); bank_questions holds the curated
-- questions as canonical JSON plus a few indexed columns; the old `questions` table stays the table of
-- SERVED instances (beats and child_questions point at it), so a later edit never changes a beat already served.
-- Foreign keys point at `assets` (created in 0008, so it comes first because this file inserts the 20 worlds).

CREATE TABLE worlds (
    id            TEXT PRIMARY KEY,
    ord           INTEGER NOT NULL,
    name_fr       TEXT NOT NULL,
    name_en       TEXT NOT NULL,
    tagline_fr    TEXT NOT NULL,
    tagline_en    TEXT NOT NULL,
    hook_fr       TEXT NOT NULL,
    hook_en       TEXT NOT NULL,
    accent_color  TEXT NOT NULL CHECK (accent_color GLOB '#[0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f]'),
    grade_min     INTEGER NOT NULL,
    grade_max     INTEGER NOT NULL,
    strand        TEXT NOT NULL,
    hero_asset_id TEXT REFERENCES assets(id),
    status        TEXT NOT NULL DEFAULT 'published' CHECK (status IN ('draft','published','retired')),
    content_hash  TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE world_quests (
    id            TEXT PRIMARY KEY,
    world_id      TEXT NOT NULL REFERENCES worlds(id),
    ord           INTEGER NOT NULL,
    skill_id      TEXT NOT NULL,
    name_fr       TEXT NOT NULL,
    name_en       TEXT NOT NULL,
    goal_fr       TEXT NOT NULL,
    goal_en       TEXT NOT NULL,
    grade_min     INTEGER NOT NULL,
    grade_max     INTEGER NOT NULL,
    target_count  INTEGER NOT NULL,
    icon_asset_id TEXT REFERENCES assets(id),
    status        TEXT NOT NULL DEFAULT 'published' CHECK (status IN ('draft','published','retired')),
    content_hash  TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);
CREATE INDEX idx_world_quests_world ON world_quests(world_id, ord);
CREATE INDEX idx_world_quests_skill ON world_quests(world_id, skill_id);

CREATE TABLE bank_questions (
    id           TEXT PRIMARY KEY,
    world_id     TEXT NOT NULL REFERENCES worlds(id),
    quest_id     TEXT NOT NULL REFERENCES world_quests(id),
    skill_id     TEXT NOT NULL,
    type         TEXT NOT NULL CHECK (type IN ('numeric','choice','truefalse','order','match','word','visual','estimate')),
    answer_kind  TEXT NOT NULL CHECK (answer_kind IN ('numeric','choice','truefalse','order','match','range')),
    grade        INTEGER NOT NULL CHECK (grade BETWEEN 1 AND 6),
    difficulty   INTEGER NOT NULL CHECK (difficulty BETWEEN 1 AND 5),
    status       TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','review','published','retired')),
    version      INTEGER NOT NULL DEFAULT 1,
    content_json TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    author       TEXT NOT NULL,
    source       TEXT NOT NULL,
    ai_drafted   INTEGER NOT NULL DEFAULT 0,
    reviewed_by  TEXT,
    reviewed_at  TEXT,
    published_at TEXT,
    retired_at   TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    -- an AI draft may sit in draft, review or retired, but only a named human reviewer lets it be published
    CHECK (ai_drafted = 0 OR status IN ('draft','review','retired') OR reviewed_by IS NOT NULL)
);
CREATE INDEX idx_bank_pick  ON bank_questions(skill_id, world_id, status, difficulty);
CREATE INDEX idx_bank_quest ON bank_questions(quest_id, status);

CREATE TABLE question_tags (
    question_id TEXT NOT NULL REFERENCES bank_questions(id) ON DELETE CASCADE,
    tag         TEXT NOT NULL,
    PRIMARY KEY (question_id, tag)
);
CREATE INDEX idx_question_tags_tag ON question_tags(tag);

-- Derived copy of the options of choice/estimate questions, rewritten on every save (search by misconception).
CREATE TABLE question_choices (
    question_id   TEXT NOT NULL REFERENCES bank_questions(id) ON DELETE CASCADE,
    choice_id     TEXT NOT NULL,
    ord           INTEGER NOT NULL,
    text_fr       TEXT NOT NULL,
    text_en       TEXT NOT NULL,
    is_correct    INTEGER NOT NULL DEFAULT 0,
    misconception TEXT,
    is_random     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (question_id, choice_id)
);
CREATE INDEX idx_question_choices_mis ON question_choices(misconception) WHERE misconception IS NOT NULL;

-- Append-only snapshot of every saved version (rollback and restore read it).
CREATE TABLE question_versions (
    question_id  TEXT NOT NULL,
    version      INTEGER NOT NULL,
    status       TEXT NOT NULL,
    content_json TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    actor        TEXT NOT NULL,
    action       TEXT NOT NULL,   -- create | edit | submit | publish | unpublish | retire | seed | rollback | restore
    at           TEXT NOT NULL,
    PRIMARY KEY (question_id, version)
);
CREATE TRIGGER question_versions_no_update BEFORE UPDATE ON question_versions
BEGIN SELECT RAISE(ABORT, 'question_versions is append-only'); END;
CREATE TRIGGER question_versions_no_delete BEFORE DELETE ON question_versions
BEGIN SELECT RAISE(ABORT, 'question_versions is append-only'); END;

-- One row per content change (who, what, which versions). Mirrors admin_audit for questions and is the source
-- of the Mongo collection question_audit. Never updated, never deleted.
CREATE TABLE question_audit (
    id           TEXT PRIMARY KEY,
    at           TEXT NOT NULL,
    actor        TEXT NOT NULL,
    action       TEXT NOT NULL,
    question_id  TEXT NOT NULL,
    from_version INTEGER,
    to_version   INTEGER,
    content_hash TEXT NOT NULL DEFAULT '',
    detail       TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_question_audit_q  ON question_audit(question_id, at);
CREATE INDEX idx_question_audit_at ON question_audit(at);
CREATE TRIGGER question_audit_no_update BEFORE UPDATE ON question_audit
BEGIN SELECT RAISE(ABORT, 'question_audit is append-only'); END;
CREATE TRIGGER question_audit_no_delete BEFORE DELETE ON question_audit
BEGIN SELECT RAISE(ABORT, 'question_audit is append-only'); END;

-- Known misconception codes: the curriculum codes (loaded by the seed from the embedded graph) plus mis_* codes.
CREATE TABLE misconceptions (
    code     TEXT PRIMARY KEY,
    skill_id TEXT,
    desc_fr  TEXT NOT NULL DEFAULT '',
    desc_en  TEXT NOT NULL DEFAULT '',
    source   TEXT NOT NULL DEFAULT 'seed'
);

-- One row per cmd/seed apply (and rollback): counts, backup file, outcome.
CREATE TABLE seed_runs (
    id          TEXT PRIMARY KEY,
    at          TEXT NOT NULL,
    actor       TEXT NOT NULL,
    dry_run     INTEGER NOT NULL DEFAULT 0,
    git_rev     TEXT,
    files_hash  TEXT,
    added       INTEGER NOT NULL DEFAULT 0,
    changed     INTEGER NOT NULL DEFAULT 0,
    retired     INTEGER NOT NULL DEFAULT 0,
    unchanged   INTEGER NOT NULL DEFAULT 0,
    backup_path TEXT,
    status      TEXT NOT NULL DEFAULT 'started'
);

-- The served-instance table gains the link to the curated bank. params_hash for a curated row is
-- "bank:<id>:v<version>", so a new version is a new served row and old beats keep their old text.
ALTER TABLE questions ADD COLUMN bank_id TEXT;
ALTER TABLE questions ADD COLUMN bank_version INTEGER;
ALTER TABLE questions ADD COLUMN origin TEXT NOT NULL DEFAULT 'generated';   -- generated | curated
CREATE INDEX idx_questions_bank ON questions(bank_id) WHERE bank_id IS NOT NULL;
CREATE INDEX idx_questions_origin_skill ON questions(origin, skill_id);

-- The 20 worlds, so every database has the full catalogue and the foreign keys above always resolve.
-- content_hash is empty here: the first cmd/seed run refreshes these rows from seed/worlds.json.
INSERT INTO worlds (id, ord, name_fr, name_en, tagline_fr, tagline_en, hook_fr, hook_en, accent_color, grade_min, grade_max, strand, status, content_hash, updated_at) VALUES
  ('nature', 1, 'Nature', 'Nature', 'Nature', 'Nature', 'Explore la forêt et ses secrets', 'Explore the forest and its secrets', '#3FA34D', 1, 4, 'arithmetique,statistique', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('espace', 2, 'Espace', 'Space', 'Espace', 'Space', 'Une mission vers les étoiles', 'A mission to the stars', '#5B5BD6', 2, 4, 'arithmetique,mesure', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('cuisine', 3, 'Cuisine', 'Cooking', 'Cuisine', 'Cooking', 'Un grand repas à préparer', 'A big meal to cook', '#F07E2B', 1, 4, 'arithmetique,mesure', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('sport', 4, 'Sport', 'Sport', 'Sport', 'Sport', 'Le grand match approche', 'The big match is coming', '#1E88E5', 1, 4, 'statistique,arithmetique', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('construction', 5, 'Construction', 'Building', 'Construction', 'Building', 'Bâtis quelque chose d’incroyable', 'Build something amazing', '#E0A100', 1, 4, 'geometrie,mesure', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('medecine', 6, 'Médecine', 'Medicine', 'Médecine', 'Medicine', 'Soigne et sauve la journée', 'Heal and save the day', '#14A0A0', 2, 4, 'mesure,arithmetique', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('science', 7, 'Science', 'Science', 'Science', 'Science', 'Le laboratoire a besoin de toi', 'The lab needs you', '#8E44AD', 2, 4, 'statistique,probabilite,mesure', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('echecs', 8, 'Échecs', 'Chess', 'Échecs', 'Chess', 'Un tournoi t’attend', 'A tournament awaits', '#4A5568', 1, 4, 'geometrie,arithmetique', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('ocean', 9, 'Océan', 'Ocean', 'Sous les vagues', 'Under the waves', 'Plonge au cœur du récif', 'Dive into the reef', '#0A6C94', 1, 4, 'arithmetique,mesure', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('dinosaures', 10, 'Dinosaures', 'Dinosaurs', 'Le musée des géants', 'The museum of giants', 'Des os géants t’attendent', 'Giant bones are waiting', '#6B6B1F', 1, 4, 'arithmetique,mesure', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('animaux', 11, 'Animaux', 'Animals', 'La ferme et le zoo', 'Farm and zoo', 'C’est l’heure du repas à la ferme', 'Feeding time on the farm', '#A0522D', 1, 3, 'arithmetique', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('musique', 12, 'Musique', 'Music', 'Au rythme de la fanfare', 'To the beat of the band', 'Le concert commence bientôt', 'The concert starts soon', '#B03A7A', 1, 4, 'arithmetique,statistique', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('transport', 13, 'Transports', 'Vehicles', 'Trains, avions et vélos', 'Trains, planes and bikes', 'Tout le monde à bord', 'All aboard', '#C2410C', 1, 4, 'mesure,arithmetique', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('pirates', 14, 'Pirates', 'Pirates', 'L’île au trésor', 'Treasure island', 'Une carte au trésor à suivre', 'A treasure map to follow', '#7B3F00', 1, 4, 'geometrie,arithmetique', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('marche', 15, 'Marché', 'Market', 'Prix, pièces et monnaie', 'Prices, coins and change', 'Compte tes pièces au marché', 'Count your coins at the market', '#1B7F3B', 1, 4, 'arithmetique', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('meteo', 16, 'Météo', 'Weather', 'Pluie, soleil et neige', 'Rain, sun and snow', 'Quel temps fera-t-il demain ?', 'What will the weather be tomorrow?', '#2F6FB5', 2, 4, 'statistique,probabilite', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('robots', 17, 'Robots', 'Robots', 'Le robot sur la grille', 'The robot on the grid', 'Programme ton robot', 'Program your robot', '#5A6B7B', 1, 4, 'arithmetique,geometrie', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('arts', 18, 'Arts', 'Art studio', 'Couleurs et formes', 'Colours and shapes', 'L’atelier est prêt', 'The studio is ready', '#C0306E', 1, 4, 'geometrie', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('hiver', 19, 'Hiver', 'Winter', 'Neige, patins et sirop', 'Snow, skates and syrup', 'La cabane à sucre ouvre ses portes', 'The sugar shack is open', '#3B6FA0', 1, 4, 'arithmetique,mesure', 'published', '', '2026-10-18T00:00:00.000Z'),
  ('jeux', 20, 'Jeux', 'Game night', 'Dés, cartes et pions', 'Dice, cards and tokens', 'C’est la soirée de jeux', 'It’s game night', '#7E3FB5', 1, 4, 'probabilite,arithmetique', 'published', '', '2026-10-18T00:00:00.000Z');
