#!/usr/bin/env bash
# Smoke test of a deployed API. One line per check, exit 1 if any check fails (so a workflow fails loudly).
#   bash scripts/smoke-prod.sh https://api.example.ca [expected-version]
# Optional env:
#   SMOKE_WRITE=1       also register a throw-away parent, read the account, then delete it (STAGING ONLY, never production)
#   ADMIN_API_TOKEN     also check the admin API answers (health + mongo sync fields)
#   SMOKE_RETRIES=20    how many times (3 s apart) to wait for the first health answer after a restart
# shellcheck disable=SC2015 # ok() and bad() never fail, so "A && ok || bad" is a real if/else here
set -uo pipefail
BASE="${1:?usage: bash scripts/smoke-prod.sh https://api.example.ca [expected-version]}"
BASE="${BASE%/}"
WANT="${2:-}"
FAILS=0
ok() { echo "ok   $*"; }
bad() { echo "FAIL $*"; FAILS=$((FAILS + 1)); }
code() { curl -s -o /dev/null -w '%{http_code}' -m 10 "$@" || echo 000; }

[[ "$BASE" == https://* || "$BASE" == http://127.0.0.1* || "$BASE" == http://localhost* ]] || { echo "refusing a non-https remote URL: $BASE"; exit 2; }

# 1. Health, with a wait loop because a restart takes a few seconds.
body=""
for _ in $(seq 1 "${SMOKE_RETRIES:-20}"); do
  body="$(curl -fsS -m 5 "$BASE/v1/health" 2>/dev/null)" && break
  sleep 3
done
if [[ "$body" == *'"status":"ok"'* ]]; then ok "health answers status ok"; else bad "health: got '${body:0:120}'"; fi
if [ -n "$WANT" ]; then
  if [[ "$body" == *"\"version\":\"$WANT\""* ]]; then ok "version is $WANT"; else bad "version: wanted $WANT, health said '${body:0:160}'"; fi
fi

# 2. Database ready.
[ "$(code "$BASE/v1/ready")" = 200 ] && ok "ready (database answers)" || bad "ready is not 200"

# 3. Auth is enforced (a 5xx here would mean a crash, a 200 would mean no protection).
c="$(code "$BASE/v1/children")"; [ "$c" = 401 ] && ok "children without token: 401" || bad "children without token: $c (want 401)"
c="$(code -X POST -H 'content-type: application/json' -d '{}' "$BASE/v1/auth/login")"
{ [ "$c" = 422 ] || [ "$c" = 400 ] || [ "$c" = 401 ]; } && ok "login with empty body: $c" || bad "login with empty body: $c (want 4xx)"

# 4. Admin API is closed without the token.
c="$(code "$BASE/admin/v1/health")"
{ [ "$c" = 401 ] || [ "$c" = 403 ] || [ "$c" = 503 ]; } && ok "admin without token: $c" || bad "admin without token: $c (want 401/403/503)"
if [ -n "${ADMIN_API_TOKEN:-}" ]; then
  ah="$(curl -fsS -m 10 -H "Authorization: Bearer $ADMIN_API_TOKEN" "$BASE/admin/v1/health" 2>/dev/null)" && ok "admin health with token" || bad "admin health with token"
  [[ "$ah" == *mongo_sync* ]] && ok "admin health reports mongo_sync" || bad "admin health has no mongo_sync field"
fi

# 5. Production-only checks: HTTPS certificate, security headers, no profiler.
if [[ "$BASE" == https://* ]]; then
  host="${BASE#https://}"; host="${host%%/*}"
  if command -v openssl >/dev/null; then
    if echo | openssl s_client -servername "$host" -connect "$host:443" 2>/dev/null | openssl x509 -noout -checkend $((14 * 86400)) >/dev/null 2>&1; then ok "certificate valid for 14+ more days"; else bad "certificate expires within 14 days or cannot be read"; fi
  fi
  [ "$(code "$BASE/debug/pprof/")" != 200 ] && ok "pprof is not exposed" || bad "pprof answers 200"
fi

# 6. Staging only: real write path.
if [ "${SMOKE_WRITE:-0}" = 1 ]; then
  email="smoke-$(date -u +%s)-$RANDOM@example.invalid"; pw="Smoke-$(date -u +%s)-pw-$RANDOM"
  reg="$(curl -sS -m 15 -X POST -H 'content-type: application/json' -d "{\"email\":\"$email\",\"password\":\"$pw\",\"locale\":\"fr-CA\"}" "$BASE/v1/auth/register")"
  tok="$(printf '%s' "$reg" | sed -n 's/.*"access_token":"\([^"]*\)".*/\1/p')"
  if [ -n "$tok" ]; then
    ok "register a throw-away parent"
    [ "$(code -H "Authorization: Bearer $tok" "$BASE/v1/children")" = 200 ] && ok "list children with token" || bad "list children with token"
    c="$(code -X DELETE -H "Authorization: Bearer $tok" -H 'content-type: application/json' -d "{\"password\":\"$pw\"}" "$BASE/v1/account")"
    [ "$c" = 204 ] && ok "delete the throw-away account (204)" || bad "delete account: $c (want 204)"
  else
    bad "register: ${reg:0:160}"
  fi
fi

if [ "$FAILS" -gt 0 ]; then echo "SMOKE FAILED: $FAILS check(s)"; exit 1; fi
echo "SMOKE PASSED"
