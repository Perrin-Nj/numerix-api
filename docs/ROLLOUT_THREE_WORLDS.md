# Rollout and rollback: three worlds, grade-true play, second try

Branch `feat/three-worlds-api`. Notes on behaviour: `docs/specs/three-worlds-api.md`. Admin routes: `docs/ADMIN_API.md`.

## Ship order (the web goes with the API)

1. Backup the SQLite file (`deploy/vps` procedure). Run `go test ./... -race -count=1` in CI (it cannot run on the Windows dev laptop) and `go run ./cmd/seed --validate-only`.
2. Deploy the API. Migration 0014 (`answer_retries`) is additive and runs at start. Flags are ON by default but are no-ops for old clients: retry only starts when the client sends `attempt`.
3. Run `cmd/seed` (the 3 draft worlds `histoires`, `english`, `francais` are inserted as `draft`; nothing else changes visibly). Note the seed run id printed (also in table `seed_runs`).
4. Deploy the web in the same window, not later. Reason: the API no longer sends `trial_ends_at`, `days_left` and `trial_days`, and never sends `status: trial` to parents and children. An old web build that reads those fields shows `undefined` (no crash on the API side). A web build that is newer than the API is fine (every new field is optional).
5. Watch: 4xx/5xx rate, `world_inactive` and `retry_not_allowed` counts, session starts per world.

## Rollback

Behaviour only (preferred, no deploy): in the admin config page, namespace `platform`, set `grade_window` false and/or `retry_enabled` false. The grade window falls back to the origin/main code path at once (config cache 30 s). Existing `answer_retries` rows stay and are ignored.

Worlds only: `PUT /admin/v1/worlds/{id}/status {"status":"draft"}` closes a world (it must first be removed from `default_worlds` and `ready_worlds`).

Code rollback (previous API binary):
1. BEFORE starting the old binary, hide the 3 draft worlds, because the old binary does not filter worlds by status and would list them (23 worlds) to children. Either `PUT /admin/v1/worlds/<id>` with `{"status":"retired"}` for `histoires`, `english`, `francais`, or in SQLite `UPDATE worlds SET status='retired' WHERE id IN ('histoires','english','francais');`.
2. Undo the questions the seed run added: `cmd/seed --rollback <seed run id from step 3>` (retires the questions of that run and marks the run; it never deletes served copies). Quests of the draft worlds can stay.
3. Start the previous binary and the previous web build together. Table `answer_retries` can stay (unused) or be dropped: `DROP TABLE answer_retries;` (no other table references it). Migration 0014 stays recorded in `schema_migrations`; a later redeploy of the new code is fine.
4. Subscription data was never modified (free access lives in `subscriptions.json` under `free_access`, ignored by the old code). Child settings are never rewritten by this release.

A later redeploy: re-run `cmd/seed`; a world retired by hand stays retired until an admin sets it back (a seed run keeps the status of existing worlds).

## Content gate before leaving the defaults on for everyone

`go run ./cmd/seed --validate-only --strict` must show no `world_grade_quota` (20 published questions per world and grade) and no `style_required`. `go run ./cmd/seed --report-style` lists the questions of echecs, medecine and espace on an ARI-OPE drill skill (A01a, A02a, A02c, A03a, A04a, A05) that still have no explicit `style`; until the content authors tag them the picker derives drill for grades 1 and 2 and context for the rest.

## Seeding the three worlds' content (echecs, medecine, espace) and retiring the old espace ids

Numbers from a dry run and a real run on a copy of a database seeded from origin/main: questions added=213 (espace 97 with ids 101 and up, echecs and medecine new ids), changed=124 (existing echecs and medecine ids updated in place, ids unchanged), retired=71 (all of them old espace ids 001 to 071; nothing is retired in any other world), quests added=24 changed=7, worlds added=3 (the draft worlds) changed=2 (medecine and espace grade range). Children keep their history: served copies in `questions` are immutable, mastery is per skill, and the picker serves the new ids only (test `TestChildWithHistoryOnRetiredIds`).

Steps, in order:
1. Backup the database file first (`deploy/vps` procedure). Deploy the API: migrations 0013 (password resets), 0014 (answer retries) and 0015 (grade ranges of medecine and espace, additive: it only widens) run at start.
2. Dry run first, nothing is written: `cmd/seed --db <file> --exclude _examples --dry-run --allow-retire`. Check `retired=71`, that every retired id starts with `espace.`, and `added` for espace is 97.
3. Real run (the tool makes its own backup unless `--no-backup`, which is refused in production): `cmd/seed --db <file> --exclude _examples --allow-retire`. Note the run id.
4. Verify: `cmd/seed --validate-only --strict` (0 errors, no `world_grade_quota` or `style_required` for the three worlds) and `cmd/seed --report-style` (0 questions without an explicit style).
5. Undo, if needed: `cmd/seed --rollback <run id>` restores the question changes of that run (the 71 retired espace ids come back, the 97 new ones are retired). Migration 0015 can stay (it only widens ranges); to revert it by hand: `UPDATE worlds SET grade_min = 2 WHERE id IN ('medecine','espace');` and `UPDATE worlds SET strand = 'arithmetique,mesure' WHERE id = 'espace';`.

## Second try: window rules (retry QA fixes)

- While a second try is open (wrong first answer sent with `attempt: 1`, no `attempt: 2`, no skip), `GET .../narration?phase=denouement` answers `409 beat_not_answered`: the denouement (and its `reaction.wrong`) would reveal the answer. It opens after the second answer or after the child moves on. The scene stream and the hint are unaffected.
- A reload resumes the window: `GET /sessions/{id}` returns that beat as `current_beat` with `retry_available: true` (new optional field on `Beat`, only when `platform.retry_enabled` is on and the session is active). Flag off: the beat is not resumed.
- Moving on = `POST /sessions/{id}/beats` (next beat). It closes an open window (the second try is then `409`). On the LAST question of a chapter, a wrong first answer with the window open does not complete the chapter: no card, status stays `active`. The chapter is completed, with the card decided once, when the second answer arrives (half credit applies) or when the child asks for the next beat (first-try score, the call answers `409 session_completed`). Rewards of that last question are returned with the second answer; after a skip they are visible through `session.score.card_awarded` and the collection, because the next-beat call carries no rewards.
- Flag off mid-window: `attempt: 2` is refused; the next-beat call still closes the window and the chapter. Legacy clients (no `attempt`) never open a window, so their behaviour is unchanged.
- CONTINUE on a completed session (web): the API sends rewards only in the answer result. A chapter completed by the last answer returns `chapter_complete` and `card_earned` there; a reload or a late CONTINUE can only read `session.status`, `session.score` (`card_awarded`) and `GET /collection`. The web must show rewards from the answer result it already holds, and fall back to those two reads; the API cannot replay rewards for a session whose answer result was lost.

## Leak validation

`Question.Leaks` and the hint rule now cover true/false (a hint that says vrai, faux, true or false), order (two or more items named in the right order) and match (both sides of a pair named). In echecs, medecine and espace a leaking `hint` or `retry_hint` is an error; elsewhere a warning (5 old questions: construction.regles.012, hiver.horaire.008, nature.rivieres.012). `feedback_leak` is an error under `cmd/seed --strict` in the three worlds (none left today), a warning elsewhere.
