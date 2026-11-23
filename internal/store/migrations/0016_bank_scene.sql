-- 0016 3D scene spec of a bank question (nullable, additive). JSON {question, reveal, hash}, written by cmd/seed from seed/scene/specs.json;
-- served only for ids in seed/scene/approved.json while platform.scenes_enabled is on.
ALTER TABLE bank_questions ADD COLUMN scene TEXT;
