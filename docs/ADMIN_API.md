# Admin API (`/admin/v1`)

Not part of the OpenAPI contract. Code: `internal/adminapi`, mounted in `cmd/api/main.go`. Mirrors the JS mock (`frontend/mock/lib/admin.mjs`); the admin app calls it through its `/api/*` proxy.

**Auth**: `Authorization: Bearer <ADMIN_API_TOKEN>`, constant-time compare. Missing or wrong token: `401`. Token unset in production: `503 admin_disabled`; unset elsewhere: dev token `dev-admin-token`. Optional header `X-Admin-Actor: name` (default `admin`, max 80) is written to the audit log.
**CORS**: only origins listed in `ADMIN_ORIGIN` (comma separated). `OPTIONS` preflight answers 204 without a token.
**Errors**: `application/problem+json` (`code` = `unauthorized`, `not_found`, `validation_failed` 422, `invalid_json` 400, `internal_error`). Body limit 1 MB.
**Lists**: `?page=1&page_size=20` (max 100) answer `{ "items": [...], "page": 1, "page_size": 20, "total": 57 }`.
**Audit**: every mutation and every child-data read writes a row to `admin_audit` (append-only, SQL triggers refuse UPDATE/DELETE) in the same transaction as the change. Migration `0005_admin.sql` also adds `parents.blocked_reason` and `admin_kv`.

## Endpoints

### GET /overview
Dashboard KPIs, computed from the live tables:
`parents_total, by_subscription{trial,active,unpaid,expired,canceled}, blocked, kids_total, active_today, mrr_cad_cents, currency, signups_30d[{date,count}], signups_30d_total, signups_by_day_30, signups_by_month_12[{month,count}], active_kids_by_day_30/minutes_by_day_30/sessions_by_day_30[{date,value}], activity_by_day_90[{date,active_kids,minutes,sessions}], subscription_mix[{status,count}], plan_mix[{plan,count,mrr_cad_cents}], mrr_by_month_12[{month,mrr_cad_cents}], kids_by_grade[{grade,count}], retention{pct,eligible,active}, churn_30d, arpu_cad_cents, top_worlds[{world,sessions_30d}], trend_30[{date,parents,kids,paid,mrr_cad_cents,arpu_cad_cents,churn_30d,retention_pct}]`.
Gap: no history tables, so `mrr_by_month_12` is 0 except the current month, and `trend_30` has real `paid/mrr/arpu/churn/retention` only on the last point (parents/kids are cumulative by creation date).

### GET /health
`200 {"status":"ok","db":"ok","time":"...","counts":{"parents":n,"children":n,"sessions":n,"notifications":n,"admin_audit":n}}`; `503 {"status":"degraded","db":"down"}`.

### Parents
- `GET /parents?q=&status=active|blocked&subscription=trial|active|unpaid|expired|canceled&plan=` list of parents:
  `{id,email,name,created_at,locale,status:"active"|"blocked",blocked_reason?,children_count,subscription:{status,plan,method?,renews_at?,trial_ends_at?,marked_by_admin?,can_play,days_left}}`.
  `subscription.status` is effective (an ended trial or paid period reads `expired`).
- `GET /parents/{id}`: same plus `children:[{id,display_name,grade,last_seen_at}]`. 404 if unknown.
- `PATCH /parents/{id}` body (all optional):
  `{"name":"..","locale":"fr-CA","status":"blocked"|"active","blocked_reason":"..","subscription":{"status":"active"|"paid"|"unpaid","plan":"free|family_monthly|family_yearly","extend_days":1..3650}}`.
  paid/active starts a 30 day (yearly: 365) period when none is running and switches `free` to `family_monthly`; `extend_days` extends trial end (trial) or period end. Answers the updated parent (200).
- `DELETE /parents/{id}`: hard delete, cascades to children and data (consents are kept). 204.

### Children
- `GET /children?parent=&q=` (audited `children.list`): `{id,parent_id,parent_name,display_name,grade,avatar_id,created_at,last_seen_at,minutes_today,sessions_total,cards_total,skills_mastered,settings}`.
- `GET /children/{id}` (audited `child.read`): same plus `parent:{id,name,email}`.
- `PATCH /children/{id}`: `{"display_name"|"name":"..","grade":1..6,"avatar_id":"..","settings":{daily_goal_chapters 1-5, daily_time_limit_minutes null|5-180, enabled_worlds[], read_aloud, sound, kid_can_change_language}}`; `daily_time_limit_minutes` and `kid_can_change_language` are also accepted flat. Settings merge into `child_settings.json`.
- `GET /children/{id}/activity` (audited `child.activity.read`): `{child_id,sessions:[{id,world,skill_id,skill_name,started_at,status,correct,total,card_awarded}],cards:[{id,world,name,earned_at}],skills:[]}`.
  Reads `correct`, `total`, `skill_id`, `skill_name` from `sessions.json` and `world`, `name` from `collection.json` when the game module writes them (else 0 or empty). `skills` is `[]` and `skills_mastered` is 0 until the BKT profile format is wired.

### Plans and config
- `GET /subscriptions/plans`: `{currency:"CAD",plans:[{id,price_cad_cents,period:"none|month|year",name{fr,en},features{fr[],en[]}}],methods:[{id,label{fr,en}}]}` (defaults until saved).
- `PUT /subscriptions/plans`: body `{plans?,methods?}`, absent part keeps current. `plans` must hold exactly `free`, `family_monthly`, `family_yearly`; methods ids in `card,interac,paypal,apple_pay,google_pay`. Stored in `admin_kv` key `plans`.
- `GET /config`, `PUT /config` (partial): `card_threshold_pct 0-100, unpaid_policy "block"|"allow", trial_days 0-365, daily_time_limit_minutes_default null|5-180, reminder_hour_local 0-23, expiring_warning_days 0-60, default_locale "fr-CA", features{name:bool}`. Stored in `admin_kv` key `config`. Platform code reads it with `adminapi.LoadConfig(ctx, store.R)` (defaults overlaid with saved values).

### Notifications and audit
- `POST /notifications/broadcast`: `{"audience":"parent"|"child","title":"txt"|{"fr","en"},"body":"txt"|{"fr","en"},"filter":{"subscription":[..],"status":[..],"locale":[..],"plan":[..],"parent_ids":[..]}}` (each filter value may be a string or a list). Inserts one `notifications` row (kind `announcement`, json `{title,body,broadcast_id}`) per matching parent, in the parent's language. `201 {"broadcast_id","recipients":n}`.
- `GET /notifications`: broadcasts grouped by `broadcast_id`: `{id,kind,title,body,created_at,audience,recipients,broadcast,parent_id}`.
- `GET /audit`: `{id,at,actor,action,target_type,target_id,detail}` newest first. Actions: `parent.update|block|unblock|delete`, `child.update|read|activity.read`, `children.list`, `plans.update`, `config.update`, `notification.broadcast`.

## Content (question bank, worlds, media)

Code: `internal/adminapi/{content,assets,aidraft}.go`. Same auth, CORS, problem+json and audit rules as above. Every mutation runs in ONE write transaction that also writes a `question_versions` snapshot, a `question_audit` row, an `admin_audit` row (action `question.<action>`) and the Mongo outbox rows; the serving cache is dropped after commit. New problem codes: `version_conflict` 409, `transition_not_allowed` 409, `needs_reviewer` 409, `not_configured` 503, `draft_failed` 502, `draft_unreadable` 422.

- `GET /questions?world=&quest=&skill=&type=&grade=&difficulty=&status=&author=&tag=&q=&ai=1&page=&page_size=` list of `{id,world,quest,skill,type,grade,difficulty,status,version,author,ai_drafted,prompt_fr,prompt_en,updated_at}`.
- `GET /questions/{id}` gives `{id,world,quest,status,version,content_hash,ai_drafted,reviewed_by,published_at,question{...},versions[],issues[]}`; `issues` is the live validation report (same code as the seed and the publish step).
- `POST /questions` body = question object (see `seed/README.md`; `id` optional, the next free `quest.NNN` is used). Always saved as `draft`, author = `X-Admin-Actor`, source `admin`; a draft with problems is saved and the issues are returned.
- `PUT /questions/{id}` saves a new version; header `If-Match: <version>` (409 `version_conflict` when stale). Author, source and the AI flag are kept; editing a published question sends it back to `review`; a retired one answers 409.
- `POST /questions/{id}/transition {to, comment}`: draft to review, published or retired; review to draft, published or retired; published to draft, review or retired; retired to draft. `review` and `published` need zero blocking issues (422, one field error per issue). Publishing records `reviewed_by` = actor; an AI draft cannot be published by its own author (409 `needs_reviewer`), and the SQL CHECK refuses a published AI draft without `reviewed_by`.
- `GET /questions/{id}/versions`, `GET /questions/{id}/versions/{n}`, `POST /questions/{id}/restore {version}` (the old version becomes a new draft version).
- `POST /questions/validate` (body = question, `id` optional) gives `{ok, issues[], unverified, calc_corrections[]}`. `POST /questions/validate-all[?world=]` gives counts and the first 500 failing questions.
- `GET /questions/export?world=&status=` gives `{schema:1, questions[]}`; `POST /questions/import?dry_run=1&publish=1` takes that shape or an array. Unchanged questions are left alone whatever their status (export then import is a no-op); new or changed ones are written as `draft` (with `publish=1` a valid non-AI question keeps `review` or `published`). Report `{added,changed,unchanged,invalid[]}`.
- `POST /questions/ai-draft {quest, type, grade?, difficulty?, topic_hint?, language_primary?}`: `503 not_configured` unless `ANTHROPIC_API_KEY` and `NARRATOR_MODEL` are set (`ANTHROPIC_DRAFT_MODEL` overrides the model; `GET /health` shows `ai: true|false`). The model proposes text and distractors from the allowed misconception codes; the server forces `status=draft`, `ai_drafted=1`, author `ai:<model>`, source `ai-draft`, recomputes every `calc` and overwrites the stored number with the exact result (`calc_corrections`), flags `unverified` when no calc exists, runs the full validator and returns `{question, unverified, calc_corrections, needs_review, valid}`. 30 drafts per hour per admin (429). The prompt and the key are never logged. The child API never calls the model.
- `GET /worlds`, `PUT /worlds/{id}`, `GET /quests?world=`, `PUT /quests/{id}`: edits are hotfixes; the seed files own worlds and quests.
- `GET /content/coverage` gives per quest the counts by status and type against `target_count`, and per world the missing quota by type.
- Media: `GET /assets?kind=&world=&status=&licence=&facet=&value=&q=`, `GET /assets/{id}`, `PATCH /assets/{id} {alt_fr, alt_en, review_status, people_free, brand_free, tags}` (approval needs alt in both languages, people_free and brand_free; a Pexels photo needs creator_url), `GET /assets/search?world=&topic=&object=&mood=&setting=&kind=&grade=` (scored candidates), `GET /assets/resolve?...&child=&question=` (what a child would get, or `fallback: gradient`). `POST /assets` answers 501: images come from `tools/assets` and `cmd/seed`.

## Runtime config (appconfig)

See docs/ADMIN_CONFIG_MATRIX.md. Endpoints: GET /appconfig, GET|PUT /appconfig/{ns}, GET /appconfig/{ns}/versions, POST /appconfig/{ns}/rollback. Audit actions: appconfig.set, appconfig.rollback. Problem codes: version_conflict 409, nothing_to_roll_back 409, ai_disabled 503.
