# Three worlds, grade-true play and second try: API notes

Branch `feat/three-worlds-api`. Everything is additive; old clients get the payloads of origin/main (key sets pinned by `internal/app/legacy_payload_test.go`).

## Flags (appconfig `platform`, edited in the admin config page)

| Key | Default | Effect |
|---|---|---|
| `grade_window` | true | Questions of the child's school year and the year before only; `[sameType, grade, difficulty, hash]` ranking; 15 percent deterministic review share; unseen-skill prior; grade 3 and 4 drills excluded (grade 3 keeps 10 percent); child with no grade is grade 1. Off: the origin/main code path (golden file `internal/engine/testdata/choose_flag_off.golden`). A world with no playable quest inside the window for a grade keeps the old rules for that grade (never a dead end). |
| `retry_enabled` | true | A client that sends `attempt: 1` gets the first wrong answer withheld (`retry_available`) and may send `attempt: 2`. Off: the full verdict at once. |
| `grade_prior_base`, `grade_prior_step` | 0.25, 0.15 | Starting mastery of an unseen skill: `clamp(base + step * (grade - annee_intro), base, 0.6)`. |
| `default_worlds` | echecs, medecine, espace | Worlds a new child starts with and the fallback for a child with no saved list. |
| `ready_worlds` | echecs, medecine, espace | Worlds a parent may switch on (decision 18). Existing stored settings are never rewritten. |
| `feed.home_sections` | all false | Which blocks the child home shows. The public config is cached 60 s, so a toggle shows within a minute. |

## Second try

`POST .../answer` with `attempt: 1` and a wrong first answer on a retry-capable question (not true/false) returns `{outcome, attempt: 1, retry_available: true, session, rewards: []}` with no `correct_answer`, `explain`, `feedback`, `method_visual`, `misconception` or `scene_key`. Mastery sees the first answer only. `POST .../hint` then answers `kind: retry_hint` (feedback of the typical error made, else `retry_hint`, else `hint`, else a canned nudge; a text that contains the answer is skipped). `attempt: 2` stores the row in `answer_retries` (migration 0014), returns the full reveal with `attempt: 2`, and counts half in the card threshold (a card can arrive with the second try when the chapter closed on that question). Errors: `409 retry_not_allowed`, `409 beat_answered` (another key after the retry), `422 world_inactive`, `422 world_not_ready`.

## Deploy order and rollback

1. Deploy the API (flags already default on; both are no-ops for clients that do not send `attempt`). Run `cmd/seed` to add the three draft worlds. 2. Deploy the web. 3. To roll back behaviour flip `grade_window` or `retry_enabled` in the admin config; `answer_retries` can stay unused. `world_inactive` and the world status switch need no rollback (flip the status).

## Planner rules with `grade_window` on, and the data gaps (release QA F1 to F3)

Rules: a skill is eligible in a world only when it has at least 8 published questions in the child's window (school year and the one before); when no quest of the world qualifies, every lower year is used; a higher year is never served. The planner (new skill, consolidation, revision and upkeep) and the journey use the same test (`PlayableFor`), so a locked node is never chosen and an available node always serves a question. Upkeep no longer picks a skill that was mastered in another world. From grade 3 a drill is served only inside the review share; when the unseen non-drill pool of the window is empty the picker looks in the lower years, then re-serves a seen question of this year or lower that this chapter has not shown. A world with no eligible quest at a grade answers a clean `422` on start (journey: every node locked with reason `grade`), never a 409 dead end. Flag off is the origin/main code (golden file). Also fixed: the bank counter multiplied the questions of a skill by the number of quests of the world that share it (it made thin pools look playable). Simulation: `internal/app/simulation_test.go` plays 40 beats for every world x grade x answer mode, with and without mastery in the other worlds.

Data gaps in the current seed (published questions; a quest is playable at a grade when the window, or all years up to the grade, holds 8 or more for its skill):

| World | Grade | Playable quests | Question pool | Quests too short (questions at or below that grade) |
|---|---|---|---|---|
| echecs | 1 | 1 | 13 | ARI-OPE-A04a (6), GEO-A01 (5), GEO-A02 (6) |
| echecs | 2 | 4 | 66 | none |
| echecs | 3 | 5 | 69 | MES-A06 (6) |
| echecs | 4 | 6 | 92 | ARI-OPE-A07a (6), MES-B01b (5), PRO-08a (5) |
| medecine | 1 | 0 | 0 | UNPLAYABLE at grade 1: ARI-OPE-A02a (6), ARI-SEN-A01c (7), MES-G01 (7), STA-03a (6); one more question each opens it |
| medecine | 2 | 3 | 52 | ARI-OPE-A02a (6), ARI-SEN-A01c (7) |
| medecine | 3 | 5 | 68 | ARI-OPE-A02a (6), ARI-SEN-A01c (7) |
| medecine | 4 | 5 | 81 | ARI-OPE-A02a (6), ARI-OPE-A06a (7), ARI-OPE-A07a (7), ARI-SEN-A01c (7), MES-B01b (7), STA-03b (7) |
| espace | 1 | 0 | 0 | UNPLAYABLE at grade 1: ARI-OPE-A04a (5), ARI-OPE-A14 (3), ARI-SEN-A02c (5), ARI-SEN-A08 (4), GEO-A01 (3), MES-G01 (4) |
| espace | 2 | 1 | 10 | thin (one quest, 10 questions): ARI-OPE-A03a (5), ARI-OPE-A04a (7), ARI-OPE-A14 (7), ARI-SEN-A02c (7), ARI-SEN-A08 (7), GEO-A01 (6) |
| espace | 3 | 3 | 30 | thin: ARI-OPE-A01a (5), ARI-OPE-A04a (7), ARI-OPE-A06a (6), ARI-SEN-A02c (7), ARI-SEN-A08 (7), GEO-A01 (6), MES-G02 (5) |
| espace | 4 | 7 | 70 | ARI-OPE-A04a (7), ARI-SEN-A02c (7), ARI-SEN-A08 (7), GEO-A01 (6) |

Medecine and espace are closed at grade 1 until content is added (the grade 1 child sees every node locked and a start answers 422); the web should not offer those two worlds to a grade 1 child. espace grades 2 and 3 are playable but repeat questions across chapters (10 and 30 questions); the Q1 quota wants 20 per grade and world. Playable at 8 questions per skill means a chapter of 5 can still repeat across chapters, never inside one.

M1: `GET /subscription/plans` shows the plan with id `free` under the display name `Standard` (fr and en) to parents, also for an admin-edited stored catalog; the id stays `free` and the admin API keeps the stored name.
