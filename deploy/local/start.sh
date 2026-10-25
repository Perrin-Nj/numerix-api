#!/usr/bin/env bash
# Starts the local copy of production (docker compose). Safe to re-run.
#   bash deploy/local/start.sh            build and start
#   bash deploy/local/start.sh seed       also load the content like the db-content workflow (dry run, then apply)
#   bash deploy/local/start.sh down       stop (keeps the data)   |   down -v   stop and delete the local data
# Secrets are generated once into deploy/local/.secrets (git-ignored); they are NOT the production secrets.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
winpath() { cd "$1" && (pwd -W 2>/dev/null || pwd); }
export LOCAL_DIR="$(winpath "$HERE")"
export WEB_DIR="${WEB_DIR:-$(winpath "$ROOT/../frontend")}"
[ -f "$WEB_DIR/scripts/release.sh" ] || { echo "frontend repo not found at $WEB_DIR (set WEB_DIR)" >&2; exit 2; }
cd "$HERE"

case "${1:-up}" in
  down) shift; exec docker compose down "$@" ;;
esac

if [ ! -d .secrets ]; then
  umask 077; mkdir .secrets
  HOST=localhost; PORT=8443
  for c in api web admin; do
    sed -e "s/YOURDOMAIN/$HOST/g" -e "s#https://\(app\|admin\|api\)\.$HOST#https://\1.$HOST:$PORT#g" \
      "$ROOT/deploy/vps/$c.production.env.example" | tr -d '\r' >".secrets/$c.env"
  done
  tok="$(openssl rand -hex 32)"
  sed -i -e "s/^JWT_SECRET=$/JWT_SECRET=$(openssl rand -hex 32)/" -e "s/^ADMIN_API_TOKEN=$/ADMIN_API_TOKEN=$tok/" .secrets/api.env
  sed -i -e "s/^ADMIN_API_TOKEN=$/ADMIN_API_TOKEN=$tok/" -e "s/^ADMIN_PASSWORD=$/ADMIN_PASSWORD=local-admin-password/" \
    -e "s/^ADMIN_SESSION_SECRET=$/ADMIN_SESSION_SECRET=$(openssl rand -hex 32)/" .secrets/admin.env
  echo "generated local secrets in deploy/local/.secrets (admin password: local-admin-password)"
fi

docker compose up -d --build
echo "waiting for the API..."
for _ in $(seq 1 60); do
  [ "$(docker compose ps api --format '{{.Health}}' 2>/dev/null)" = healthy ] && break
  sleep 2
done

if [ "${1:-}" = seed ]; then
  docker compose run --rm seed
  docker compose run --rm seed --apply
fi

echo
echo "web   https://app.localhost:8443"
echo "admin https://admin.localhost:8443   (password: local-admin-password)"
echo "api   https://api.localhost:8443/v1/health"
echo "Chrome/Edge resolve *.localhost by themselves; accept the Caddy certificate warning once."
