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
