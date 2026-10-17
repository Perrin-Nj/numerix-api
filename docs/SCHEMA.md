# SQLite schema (migration `0001_init`)

Source: `internal/store/migrations/*.sql`, applied at start by `store.Migrate` (table `schema_migrations`). Ids are UUID text, timestamps UTC ISO-8601 text, `json` columns hold JSON text. Table and column names are stable: add columns or tables through a NEW migration file (`0002_...sql`), never edit an applied one.

| Table | Purpose | Key columns |
|---|---|---|
| `parents` | Parent accounts | `id`, `email` (unique, case-insensitive), `password_hash` (argon2id), `name`, `locale`, `timezone`, `blocked_at`, `created_at`, `deleted_at` |
| `refresh_tokens` | Rotating refresh tokens and device tokens, SHA-256 hash only | `parent_id`, `family_id`, `kind` (`refresh`/`device`), `token_hash` (unique), `expires_at`, `used_at`, `revoked_at` |
| `children` | Child profiles | `parent_id`, `display_name`, `birth_year`, `grade`, `avatar_id`, `pin_hash` (argon2id, null = no PIN), `locale`, `timezone`, `favorite_world`, `onboarding_completed`, `deleted_at` |
| `child_settings` | Per-child settings | `child_id` (PK), `json` |
| `consents` | Law 25 evidence, no FKs (kept 2 years after deletion) | `parent_id`, `child_id`, `notice_version`, `notice_hash`, `ip_prefix`, `user_agent`, `accepted_at`, `revoked_at` |
| `subscriptions` | Trial and paid state | `parent_id`, `plan_id`, `status`, `trial_ends_at`, `current_period_end`, `canceled_at`, `provider_ref`, `json` |
| `notifications` | In-app notifications | `parent_id`, `child_id`, `audience`, `kind`, `json`, `read_at` |
| `push_subscriptions` | Web push endpoints | `parent_id`, `endpoint` (unique), `p256dh`, `auth` |
| `analytics_events` | Anonymised events (salted `child_hash`, no names, emails, answers) | `child_hash`, `name`, `json` |
| `questions` | Question bank | `params_hash` (unique), `skill_id`, `json`, `served_count` |
| `child_questions` | Never serve the same question twice to a child | PK (`child_id`, `question_id`) |
| `sessions` | Play sessions | `child_id`, `status`, `world_id`, `json`, `started_at`, `paused_at`, `finished_at` |
| `beats` | Steps of a session | `session_id`, `idx` (unique with session), `question_id`, `json` |
| `answers` | Answers to beats | `beat_id`, `session_id`, `idempotency_key` (unique with beat), `outcome`, `json` |
| `child_profiles` | BKT mastery per child | `child_id` (PK), `bkt` (json) |
| `feed_items`, `reactions` | Feed and reactions | `child_id`, `kind`, `json`; reactions PK (`feed_item_id`, `child_id`) |
| `collection` | Cards owned | PK (`child_id`, `card_id`), `json` |
| `quests`, `challenges`, `gifts` | Engagement | `child_id`, `status`, `json` (gifts also `claimed_at`) |

Foreign keys are enforced (`PRAGMA foreign_keys=1`); deleting a parent cascades to children and everything under them.

Connections: one write pool (`MaxOpenConns(1)`, transactions via `Store.WithWriteTx`, `BEGIN IMMEDIATE`) and a read pool (`query_only`). Pragmas: WAL, `busy_timeout=5000`, `foreign_keys=1`, `synchronous=NORMAL`.
