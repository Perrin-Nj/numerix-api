# Admin configuration matrix

Every tunable the app has, where it lives, and whether an admin can change it at runtime. Runtime = stored in SQLite table `app_config` (migration `0011_appconfig.sql`), edited through `/admin/v1/appconfig/{ns}`, cached in memory, dropped on every save. Each save inserts version N+1 with `old_value`, `actor`, `at` (append-only, SQL triggers refuse UPDATE/DELETE) and one `admin_audit` row (`appconfig.set` or `appconfig.rollback`). Code: `internal/appconfig`, `internal/adminapi/appconfig.go`.

Admin endpoints (Bearer admin token, checked before the body is read):
`GET /appconfig` (namespaces + read-only env view), `GET|PUT /appconfig/{ns}` (PUT body `{value, expected_version?}`, stale version = 409), `GET /appconfig/{ns}/versions`, `POST /appconfig/{ns}/rollback` (`{to_version?}`, default = undo the last change; undoing the first save restores the defaults). Legacy `GET|PUT /config` is the `platform` namespace with partial merge.
Public (no auth, `Cache-Control: public, max-age=60`): `GET /v1/config/public`, contract `AppPublicConfig`.

## Runtime-editable (admin can change)

| Knob | Namespace.key | Default | Enforced in | Admin screen |
|---|---|---|---|---|
| Unpaid policy | platform.unpaid_policy | block | `gamePlayAllowed` | Config |
| Trial length (days) | platform.trial_days | 14 | signup | Config |
| Card threshold % | platform.card_threshold_pct | 70 | answer/session | Config |
| Default daily time limit | platform.daily_time_limit_minutes_default | 60 / null | new child settings | Config |
| Reminder hour, expiring warning days, default locale | platform.* | 17, 7, fr-CA | notify, signup | Config |
| Feature flags (feed, challenges, push, payments) | platform.features | see code | payments gate, public config | Config |
| Default daily goal | platform.daily_goal_default | 1 | new child, feed goal | Config |
| Welcome gift (was env FEATURE_WELCOME_GIFT) | platform.welcome_gift | env value | gift claim, home | Config |
| AI question drafts on/off | ai.draft_enabled | true | `POST /questions/ai-draft` (503 ai_disabled) | Config |
| AI narrator on/off | ai.narrator_enabled | true | narrator (off = canned bank) | Config |
| Kid-visible copy (home tip, empty feed, chapter done) | copy.* (fr/en) | built-in | public config | Config |
| Music enabled, default volume, track list (enable/disable, titles) | music.* | on, 30, 7 tracks | new child settings, public config | Music |
| Human voice default, computer voice allowed, clip manifest | voice.* | on, off, empty | new child settings, public config | Voice |
| Scene physics kill switch, family on/off, disabled variants | scenes.* | all on | public config (client plays) | Scenes |
| Discover on/off, hidden item ids, grade stretch | feed.* | on, none, 1 | `GetDiscover` | Feed |
| Family board visible | feed.family_board_visible | true | public config (client hides) | Feed |
| Maintenance banner (level, fr/en text) | maintenance.* | off | public config | Maintenance |
| Close the game for maintenance | maintenance.block_play | false | `gamePlayAllowed` (503 maintenance) | Maintenance |
| Plans, prices, payment methods | admin_kv.plans (mirrored to plan_catalog) | built-in | catalog | Plans (existing; audited, not versioned) |
| Question, world, quest, asset publish status | content tables | draft | content API | Questions, Media (existing, versioned and audited) |
| Parent block, subscription state, child settings | parents, subscriptions, child_settings | - | admin parents/children | Parents, Children (existing, audited) |

Bug fixed on the way: before this change the admin Config and Plans screens wrote `admin_kv` while the game read `platform_config` and `plan_catalog`, so those edits never reached the app. The game now reads the config store, saved plans are mirrored into `plan_catalog`, and migration 0011 carries old saved values over.

## Env-only (shown read only as set / not set; secrets never show a value)

JWT_SECRET, ADMIN_API_TOKEN, MONGODB_URI, MONGO_PSEUDO_SECRET, LLM_API_KEY, ANTHROPIC_API_KEY: secrets, must not live in a web-editable table.
APP_ENV, PORT, DB_PATH, ALLOWED_ORIGINS, ADMIN_ORIGIN, TRUST_PROXY, PPROF, WEBAUTHN_RP_ID, WEBAUTHN_ORIGINS: deployment and security wiring, change needs a restart and a deploy decision.
RATE_LIMIT_IP_PER_MIN, MAX_CONCURRENT_REQUESTS, REQUEST_TIMEOUT_SECONDS, ACCESS_TTL_SECONDS, CHILD_TTL_SECONDS, REFRESH_TTL_SECONDS: read once into middleware and token issuers at start; a wrong value can lock everyone out, so restart-only.
ASSET_BASE_URL, MEDIA_DIR, MEDIA_BASE_URL, NARRATOR_MODEL: filesystem and provider wiring.

## Code-only (not admin-controllable, and why)

| Knob | Where | Why |
|---|---|---|
| Chapter length (3 beats) | `chapterLength` in game.go | Fixed by the contract (`ChapterLength`); the client relies on it. |
| Mastery threshold, BKT constants, half-life, spacing | `internal/engine/bkt.go` | Pedagogy model, changing it invalidates stored mastery; needs a reviewed release. |
| Skill graph, prerequisites, locked rules, grade bands | embedded curriculum JSON | Curriculum content, reviewed and versioned in git. |
| Discover puzzle gap (every 8th slot) | `discoverPuzzleGap` | Cursors encode positions; changing it live would reshuffle every child's feed. |
| Scene variant catalogue and physics bodies | frontend consequences code | The list of variant ids is code; admin switches families and ids off, not new ones on. |
| Music files and licences | frontend `public/music` | Files ship with the app (CC BY credits); admin toggles the list, not the audio. |
| Voice recordings themselves | admin voice recorder + import script | Binary assets; server `POST /assets` is 501; the manifest URLs are admin-editable. |
| PIN and login attempt limits (5 per 15 min) | `auth.AttemptLimiter` | Security control, kept out of reach of a web form. |
| Leaderboard | no ranking exists | Only the private family board exists; its visibility is the `family_board_visible` flag. |

## Risks to know

- Single process: the cache is dropped on save inside the process; a second API instance would see changes after at most 30 s (`cacheTTL`).
- The admin proxy shares one admin password, so the audit actor is `admin` unless the proxy forwards `X-Admin-Actor` (it does not yet).
- `block_play` also stops chapters already in progress (they answer 503 on the next request).
- Client wiring is minimal by design (music defaults, feature flags, maintenance banner); it must read `/v1/config/public` and fall back to its built-in defaults when the call fails.
