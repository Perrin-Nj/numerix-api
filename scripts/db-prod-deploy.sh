#!/usr/bin/env bash
# Content deploy for the production database (docs/DB_PROD_DEPLOY.md).
# Dry run by default. Run on the VM (or on your PC against a copy):
#   bash scripts/db-prod-deploy.sh                 # dry run: shows added/changed/unchanged, writes nothing
#   bash scripts/db-prod-deploy.sh --apply         # backup, migrate, apply only what changed
#   bash scripts/db-prod-deploy.sh --rollback <run_id>
# Env: DB_PATH (default /var/lib/numerix/numerix.db), SEED_DIR (default seed), SEED_BIN (default ./bin/seed),
#      APP_ENV=production refuses --no-backup. The demo file seed/questions/_examples.json is never loaded here.
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PATH:/usr/local/go/bin:/c/Program Files/Go/bin"

DB="${DB_PATH:-/var/lib/numerix/numerix.db}"
SEED_DIR="${SEED_DIR:-seed}"
MODE="--dry-run"
EXTRA=()
while [ $# -gt 0 ]; do
  case "$1" in
    --apply) MODE="" ;;
    --rollback) MODE="--rollback"; shift; EXTRA+=("$1") ;;
    --allow-retire|--strict) EXTRA+=("$1") ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
  shift
done

if [ -x "${SEED_BIN:-./bin/seed}" ]; then
  SEED=("${SEED_BIN:-./bin/seed}")
else
  SEED=(go run ./cmd/seed)
fi

echo "== validate"
"${SEED[@]}" --db "$DB" --seed-dir "$SEED_DIR" --exclude _examples --validate-only
echo "== run (${MODE:-apply})"
"${SEED[@]}" --db "$DB" --seed-dir "$SEED_DIR" --exclude _examples ${MODE:+$MODE} "${EXTRA[@]}"

# Discover feed: the curated items (seed/feed/items.json) are a separate table loaded by feedseed; the media files are
# copied to MEDIA_DIR by hand once per release. Skipped on --rollback, which only undoes a content run.
FEED_BIN="${FEED_BIN:-./bin/feedseed}"
if [ "$MODE" != "--rollback" ] && [ -x "$FEED_BIN" ] && [ -f "$SEED_DIR/feed/items.json" ]; then
  export MEDIA_DIR="${MEDIA_DIR:-/var/lib/numerix/feed-media}"
  echo "== feed (validate)"
  "$FEED_BIN" --items "$SEED_DIR/feed/items.json" --media "$MEDIA_DIR" --validate-only
  if [ -z "$MODE" ]; then
    echo "== feed (apply)"
    "$FEED_BIN" --db "$DB" --items "$SEED_DIR/feed/items.json" --media "$MEDIA_DIR"
  fi
fi
if [ -z "$MODE" ]; then
  echo "== done. Check: curl -fsS http://127.0.0.1:8081/v1/health ; the API refreshes its view of the bank within 30 s."
fi
