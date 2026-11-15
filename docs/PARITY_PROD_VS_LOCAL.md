# Parity: prod (A) vs local main 450cd73 (B) vs local hardening/sec-perf (C)

Run: 2026-11-15. Script: one bash+curl script (`parity.sh`, scratchpad), 54 checks, identical on all targets.
A = https://api.numerix.pro (47 + 7 requests, one throwaway parent), B/C = local builds, APP_ENV=development, fresh seeded DB, FEATURE_WELCOME_GIFT=false (prod config/public shows welcome_gift=false).
Branch diff vs main is deps only (go 1.27.4, oapi-codegen runtime, mongo-driver, x/crypto, sqlite), GOMEMLIMIT, CI. No handler code changed.

## Status codes (all identical in A, B, C)
health 200, ready 200, config/public 200, unknown route 404, register 201, login 200, bad password 401, unknown user 401, malformed JSON 422, text/plain content-type 200 (accepted everywhere), refresh 200, bad refresh 401, account 200, no token 401, bad token 401, create child bad 422 / ok 201, list 200, get 200, token before consent 403, consent 204, wrong PIN 403, child token 200, worlds 200, settings 200, export 200, parent on child route 403, child on parent route 403, home 200, journey 200, feed 200, session start without Idempotency-Key 422, bad world 422, start 201, next beat 201, bad answer JSON 422, answer 200, answer again 409, session get 200, finish 200, summary parent 200 / child 403, progress 200, CORS preflight from app origin 204, from evil origin 404, logout 204, delete wrong password 401, delete 204, login after delete 401, account with old token 401, child token after delete 404.
Only exception: `discover` A=200, B=C=503 feed_empty (local DB has no discover content seeded; environment difference).

## B vs C (regression candidates)
Statuses: 0 differences. Header sets: 0 (only random nx_device cookie value, chunked encoding). JSON key sets: only random content of the randomly picked question (beat_next visual vs choices) and answer.correct_answer type (str vs int, depends on the question drawn). No regression found.

## A vs B (environment)
- Strict-Transport-Security duplicated/includeSubDomains: added by Caddy on prod.
- nx_device cookie `Secure`: production env only.
- discover 200 vs 503: content seeding.
- beat_next: random question shape.

## Web and admin (prod vs local docker stack)
GET /, /login, /fr, /en, /api/health, /nope on app and admin: same status codes (app / 200, /login 404, /fr 308; admin / 307, /login 200, /api/health 401) and identical security headers on `/` (app) and `/login` (admin). Admin /login renders (200). The local stack runs the existing image, not rebuilt from the branch.

## Not done
Browser (Chrome extension not connected): UI click-through, console and network errors not checked.

## Cleanup
Throwaway parent qa-secperf-1794754380@example.com deleted via deleteAccount (204); login afterwards 401; old token 401.

## Verdict: SAFE TO PROMOTE (API contract level). Caveat: no browser UI check.
