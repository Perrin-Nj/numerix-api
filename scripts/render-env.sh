#!/usr/bin/env bash
set -euo pipefail

HERE="$(cd "$(dirname "$0")/../deploy/oracle" && pwd)"
DOMAIN=""
TARGET_ENV=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --domain) [ "$#" -ge 2 ] || { echo "--domain requires a value" >&2; exit 2; }; DOMAIN="$2"; shift 2 ;;
    --env) [ "$#" -ge 2 ] || { echo "--env requires a value" >&2; exit 2; }; TARGET_ENV="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

[[ "$DOMAIN" =~ ^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$ ]] || {
  echo "domain must be a plain DNS name such as example.ca" >&2; exit 2;
}
case "$TARGET_ENV" in staging|production) ;; *) echo "--env must be staging or production" >&2; exit 2 ;; esac

OUT="$HERE/generated/$TARGET_ENV"
[ ! -e "$OUT" ] || { echo "refusing to overwrite $OUT; move existing files first" >&2; exit 1; }
umask 077
mkdir -p "$OUT"
for component in api web admin; do
  sed "s/YOURDOMAIN/$DOMAIN/g" "$HERE/$component.$TARGET_ENV.env.example" | tr -d '\r' >"$OUT/$component.env"
done
chmod 600 "$OUT"/*.env

printf 'Rendered %s templates to %s (secrets remain blank).\n' "$TARGET_ENV" "$OUT"
printf '%s\n' \
  'On the VM, generate distinct secrets with: openssl rand -hex 32' \
  'Use one ADMIN_API_TOKEN value in both api.env and admin.env.' \
  'If enabling Mongo, generate MONGO_PSEUDO_SECRET once and keep it stable.'
