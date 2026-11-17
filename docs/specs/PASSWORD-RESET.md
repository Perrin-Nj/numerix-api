# Password reset (parents and admin)

Status: spec first, then tests, then code. Branch `feat/password-reset`.

## Goals
- A parent who forgot the password recovers the account alone, in two steps: ask by email, click the link, choose a new password.
- Support can unblock a parent when email is down or not configured: the admin creates a link and shares it. The admin never sees or sets a password.
- Secure by default, minimal, additive: no change to existing routes.

## Contract
1. `POST /v1/auth/password/forgot` body `{email, locale?: fr-CA|en-CA}` -> `202 {}` for every well-formed email (same status, same body, same time class whether the account exists or not). `429` problem+json when limited. `422` only for a malformed body. For an existing, non-deleted, non-blocked parent it creates a one-time token (30 minutes) and sends an email through the Mailer, asynchronously.
2. `POST /v1/auth/password/reset` body `{token, password}` -> `204`. `400` code `reset_token_invalid` for an unknown, expired, used or replaced token (one generic error). `422` when the password breaks the register rule (10 to 128 characters; argon2id). `429` when limited. On success, in one transaction: new hash, token marked used, all other unused tokens of the parent invalidated, all refresh tokens deleted (every session signed out), audit row `parent.password_reset_self`. After commit: per-email login lockout cleared, and a "your password was changed" email when the Mailer is configured. No auto-login.
3. `POST /admin/v1/parents/{id}/password-reset` (admin bearer, audited like the other parent routes) -> `201 {token, expires_in_seconds, parent:{id,email}}`. Token valid 24 hours, earlier tokens invalidated. Audit action `parent.password_reset` without the token. Works without email. `404` for an unknown or deleted parent. Parent and child tokens are rejected (the admin API only accepts the admin credential).
4. `GET /v1/config/public` gains `password_reset_email: boolean`, true only when the Mailer is configured. Additive and optional in the schema.

## Data (migration `0013_password_resets.sql`)
`password_resets(id PK, parent_id FK parents ON DELETE CASCADE, token_hash UNIQUE, created_at, expires_at, used_at NULL, created_by 'self'|'admin', request_ip_hash NULL)` and index on `parent_id`. Additive only; down = drop the table. Note: another unmerged branch may also use number 0013; the later merge renames its file to the next free number (the migration runner orders by file name, the table name does not clash).

## Token
32 bytes from crypto/rand, base64url (no padding). Only the SHA-256 hex is stored; lookups are by hash (equality on a high-entropy hash, not a secret-dependent compare). One active token per parent: creating one marks every other unused token of that parent used. Single use is enforced by `UPDATE ... SET used_at=? WHERE id=? AND used_at IS NULL AND expires_at>?` inside the write transaction (SQLite has one writer, so two racing resets cannot both win).

## Rate limits
- forgot: 3 per hour per normalised email, 30 per hour per IP. Every request counts (existing or not), so the limiter reveals nothing.
- reset: 10 per 15 minutes per IP (counts every attempt).
- admin route: the existing admin rules (no extra limit; it needs the admin credential).
- Password length is checked (max 128) before any hashing. The email is capped at 254 characters.

## Mailer
`internal/mailer`: interface `Mailer { Send(ctx, Message) error }`, SMTP implementation with the standard library (STARTTLS on 587 or implicit TLS on 465). Env: `SMTP_HOST` (empty = disabled), `SMTP_PORT` (default 587), `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM` (display name allowed), `MAIL_BASE_URL` (https origin of the parent web app; https enforced in production). Partial config is an error in production. CR/LF in addresses or subjects is refused. Sending runs in a goroutine with a 15 s timeout and at most 4 in flight (excess is dropped and logged); the HTTP answer never waits for mail and never fails because of mail.
When disabled: forgot still answers 202, sends nothing and logs one line `password reset requested but email disabled` with no address and no token.
Email: bilingual (the parent's saved locale; request `locale` only when the parent has none, default fr-CA), short, no tracking, link `MAIL_BASE_URL/parent/reset#token=<token>` (fragment, so the token never reaches server logs or Referer), says it expires in 30 minutes and to ignore it if not requested.

## Security rules
No enumeration (status, body, timing class). Token only in the email and in the admin 201, never in logs, audit rows or other responses. Expired, used and replaced tokens are rejected with the same error. Blocked and deleted parents cannot reset (and get no email). Sessions revoked on success. Lockout cleared. SQL always parameterised. Bodies are size-limited by the HTTP layer. Admin routes reject parent and child tokens.

## Tests (written first)
store/migration present; forgot: 202 existing vs unknown identical (status, body), slow mailer does not slow the answer, mail sent once with a fragment link and no token in logs, blocked/deleted no mail, rate limits (email, IP), malformed 422, disabled mailer logs no email; reset: success then login with new password and old fails, replay fails, expired fails, replaced token fails, unknown token 400, password policy 422, oversize bounded, concurrency two resets exactly one 204, refresh tokens deleted, lockout cleared, blocked parent cannot reset, injection string as token is a plain 400, rate limit 429, notice email; admin: 201 shape, 24 h, earlier token invalid, audit without token, 404, parent/child token 401, works without mailer; config: public flag, partial SMTP is a production error, https base URL; mailer: header injection refused, message formatting, bilingual body.
