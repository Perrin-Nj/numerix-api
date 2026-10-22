# Mongo sync

SQLite is the source of truth and answers every endpoint of the contract. The contract has no leaderboard, personal-best, family-board or stats operation, so Mongo is **write-only**: there is no read-through. (If a future endpoint needs the family board, read it from SQLite first and add a Mongo read behind a health check only if SQLite proves too slow.)

Mongo (Atlas M0) receives only three things. There is no ranking against strangers, ever.

## Flow

1. A chapter ends (last answer in `AnswerBeat`, or `FinishSession`). In the SAME write transaction the code inserts up to three small rows in the `outbox` table (migration `0007`). A rollback removes them with the game change.
2. The worker goroutine wakes every 5 s, reads up to 100 due rows, collapses duplicate keys, and asks the resolver for the current state of each key (read pool, so it never blocks writers).
3. It sends one unordered bulk upsert per collection and marks the rows `sent_at`. Sent rows are deleted after 24 h.

Delivery is at-least-once. Every write replaces the document with a unique `key`, so a replay leaves the same document.

Disabled when `MONGODB_URI` is empty: no rows are written, no goroutine runs, the log says so once.

## Data model

| Collection | `key` | Document |
|---|---|---|
| `personal_bests` | HMAC of child id | `child`, `longest_streak_days`, `current_streak_days`, `chapters_completed`, `updated_at` (same streak computation as `GET /children/{id}/engagement`) |
| `family_board` | HMAC of parent id + `\|` + week start (Monday, parent time zone) | `family`, `week_start`, `members[]` (`child`, `chapters`, `days_played`), `total_chapters`, `updated_at` |
| `progress_stats` | `world\|skill` | `world`, `skill`, `children`, `attempts`, `correct_rate`, `updated_at` |

Unique index on `key` in each collection, created at start (retried on the first write if Atlas was unreachable). No TTL.

## Privacy rules (Law 25)

- No names, emails, birth years, free text, answers, or real ids in Mongo. Ids are `HMAC-SHA256(MONGO_PSEUDO_SECRET, domain:id)`, domains `child` and `family`. Without the secret a pseudonym cannot be reversed or tested against guesses.
- Display names for a family board are resolved from SQLite at read time.
- k-anonymity: a stats bucket is written only when at least 5 distinct children contributed. Below that the document is deleted (it shrank because a child was removed).
- Erasure: deleting a child queues a delete of its `personal_bests` document and a refresh of the family board; deleting the account (or a soft-deleted or missing parent) deletes every `family_board` document of that family.
- Known gap: older weeks of a family board still list the pseudonym of a child deleted later. The pseudonym cannot be linked to a person without the secret and SQLite.
- `MONGO_PSEUDO_SECRET` is required (32+ characters) whenever `MONGODB_URI` is set, in every environment. Never rotate it: pseudonyms would change.

## Failure modes

| Event | Result |
|---|---|
| Atlas unreachable or credentials wrong | App unaffected. Rows stay in SQLite, retried with backoff 5 s doubling to 15 min, with jitter. `last_error` shows the cause. |
| Process stops | One final flush on shutdown (3 s); anything left is sent on the next start. |
| Crash after Mongo write, before `sent_at` | The rows are sent again. Safe because writes are idempotent. |
| Atlas full (512 MB) or write rejected | Same as unreachable: retries, visible in `last_error`. A permanently rejected document retries forever at the 15 min cap. |
| Free tier has no backups | Mongo can always be rebuilt: delete the collections and re-enqueue (all state derives from SQLite). |

Health: `GET /admin/v1/health` (admin token) has `mongo_sync`: `enabled`, `pending`, `sent_total`, `failures_total`, `last_sync_at`, `last_error`. The public `/health` is unchanged on purpose (it is in the contract and must not expose infrastructure errors).

## Config

`MONGODB_URI` (empty = off), `MONGODB_DB` (default `numerix`), `MONGO_PSEUDO_SECRET`. See `deploy/vps/env.example` and the Atlas steps in `docs/DEPLOY_ORACLE.md`.

## Tests

```
go test ./internal/mongosync ./internal/app ./internal/config -count=1
```

Unit tests use a fake sink: same-transaction outbox and rollback, batching, coalescing, retry and backoff, idempotent replay, pseudonym stability, k-anonymity floor, disabled mode, PII scan of the documents.

Real Mongo (skipped without the variable; use a throw-away cluster or a local `mongod`, the test creates and drops its own database):

```
MONGODB_URI_TEST='mongodb://localhost:27017' go test ./internal/mongosync -run Integration -v
```

## Content collections (question bank)

Same outbox, worker and unique `key` index. Content is not personal data, so no pseudonym; every change of a bank question, world or quest queues its row in the SAME transaction (`internal/contentdb`). The resolver reads the current state, so replays are safe, and a missing object becomes a delete.

| Collection | `key` | Document |
|---|---|---|
| `question_bank` | question id | `world, quest, skill, type, grade, difficulty, status, version, content_hash, author, source, ai_drafted, reviewed_by, updated_at, published_at, question{full object}`. Queued on create, edit, publish, unpublish, retire, restore, seed, rollback, import. |
| `question_audit` | `question_audit.id` | `at, actor, action, question_id, from_version, to_version, content_hash, detail` |
| `question_stats` | question id | `world, quest, skill, attempts, children, correct_rate, typical_error_rate, top_misconceptions[{code,count}], median_response_ms, updated_at`. Queued after each answer to a curated question. Written only when at least 5 distinct children contributed (below that the document is deleted); codes seen fewer than 5 times are left out; no child id. |
| `content_worlds` | world id | names, tagline, hook, colour, grades, strand, hero, status |
| `content_quests` | quest id | world, skill, names, goal, grades, target_count, icon, status |

A seed run of N changed questions queues about 2N rows, collapsed by key before sending. `cmd/seed` does not run the worker: rows wait in the outbox until the API drains them; with Mongo off no rows are written (`cmd/seed` queues rows when `MONGODB_URI` is set in its environment). To rebuild a collection, re-save the objects (admin import with a changed hash) or restore from SQLite with a one-off script.
Tests: `TestWorldsQuestsCoverageAndMongoDocs` and `TestStatsFloorAndShape` in `internal/adminapi`, outbox keys in `internal/seedload`.
