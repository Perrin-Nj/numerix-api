# Password reset (admin gives a code, parent chooses a new password)

Status: spec first, then tests, then code. Branch `feat/password-reset`. Owner decision: no email at all, everything happens on screen.

## Goals
- A parent who forgot the password contacts support. The admin creates a one-time code and reads it to the parent. The parent enters email, code and a new password. Done in under a minute.
- The admin never sees or sets a password.
- Secure, minimal, additive: no change to existing routes, no new dependency, no email, no SMTP.

## Contract
1. Admin `POST /admin/v1/parents/{id}/password-reset` (admin bearer, same auth and audit conventions as the other parent routes) -> `201 {code, expires_in_seconds: 3600, parent: {id, email}}`. `code` is 8 digits shown as `NNNN-NNNN`, valid 1 hour, replaces (burns) any earlier code of that parent. Audit action `parent.password_reset`, without the code. `404` for an unknown or deleted parent. Parent and child tokens are refused (401) like on every admin route. Response is `Cache-Control: no-store`.
2. Parent `POST /v1/auth/password/reset` body `{email, code, password}` -> `204`. The code is accepted with or without dash and spaces. `400` code `reset_code_invalid` for EVERY failure cause: unknown email, no active code, wrong or malformed code, expired, used, burned, blocked or deleted parent (one body, one status). `422` when the password breaks the register rule (10 to 128 characters, argon2id); the password rule is checked first and does not consume the code. `429` when the IP limit is hit. No auto login: the web sends the parent to the login page.
   On success, in one transaction: new argon2id hash, code marked used, all refresh tokens of the parent deleted (every session signed out), remembered devices revoked, audit row `parent.password_reset_used`. Then the per-email login lockout is cleared.

## Data (migration `0013_password_resets.sql`)
`password_resets(id PK, parent_id FK parents ON DELETE CASCADE, code_hash, salt, attempts INT default 0, created_at, expires_at, used_at NULL, burned_at NULL, created_by 'admin')` + index on `parent_id`. Additive only; down = drop the table. Another unmerged branch may also use number 0013: the later merge renames its file to the next free number (the table name does not clash).

## Code
8 digits from crypto/rand (uniform, 10^8 values). Only `HMAC-SHA256(key, salt || code)` is stored (random 16 byte salt per row; key = HMAC(JWT_SECRET, "numerix:password-reset:v1"), never the raw secret), compared with hmac.Equal, so a database copy alone cannot verify a guess. Single use: `UPDATE ... SET used_at WHERE id=? AND used_at IS NULL AND burned_at IS NULL AND expires_at>now` inside the write transaction; of two racing resets exactly one changes a row. One active code per parent: issuing burns the earlier ones.

## Brute force
8 digits is guessable by a patient attacker, so the budget is tight: after 5 wrong codes for an email the active code is burned (`attempts` is in the database, so a restart does not reset it); the admin must issue a new one. Per IP: 10 attempts per 15 minutes (`ResetPassword` rule in the httpapi guard). Wrong attempts for unknown emails are counted too, in memory by SHA-256 of the email, and every failure path does the same lookup and hash work, so status, body and timing class do not reveal whether an account or a code exists. Argon2 runs only after a correct code.
Residual risk: an attacker who knows an email can burn a live code with 5 wrong guesses (a nuisance, never a takeover; the admin issues another). A leak of the database AND the signing secret is needed to brute force codes offline, and then only within the 1 hour life.

## Security rules
No enumeration. Code only in the admin 201, never in logs, audit rows or other responses. Expired, used, burned and replaced codes are rejected with the same error. Blocked or deleted parents cannot reset. Sessions and devices revoked. Lockout cleared. SQL parameterised. Request bodies are size limited by the HTTP layer (413). Admin routes reject parent and child tokens.

## Tests (written first)
Admin: 201 shape and 1 hour ttl, hash salted not plain, audit without code, replacement, 404 unknown/deleted, 401 for missing, parent and child style tokens. Parent: full flow (old password stops, new works, refresh revoked, device revoked, lockout cleared, audit, no code in logs, salted hash stored), dash and spaces accepted, every failure cause gives the same 400, timing class equal for known and unknown email, brute force burns the code after 5 wrong, expiry, replacement, replay, password policy keeps the code, oversize body, concurrency (exactly one winner), blocked after issue, IP rate limit, cascade delete.
