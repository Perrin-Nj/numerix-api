#!/usr/bin/env bash
# Run ONCE per VM (Ubuntu 24.04, ARM or x86) as a sudo user. One VM = one environment (staging OR production).
#   sudo bash setup.sh --app app.example.ca --admin admin.example.ca --api api.example.ca --deploy-key /tmp/deploy_key.pub
# It installs Caddy (HTTPS), Litestream (SQLite backup), sqlite3 and Node.js, creates the service user (numerix) and the
# restricted deploy user (deploy), installs the systemd units and the numerix-deploy gate. It does not start anything:
# the first release is pushed by the GitHub deploy workflow (or by hand, see docs/PROD_GO_LIVE.md).
set -euo pipefail

NODE_VERSION="v22.23.3" # checked to exist on nodejs.org/dist (2026-10-21); bump on purpose, never "latest"
APP_HOST="" ADMIN_HOST="" API_HOST="" DEPLOY_KEY=""
while [ $# -gt 0 ]; do
  case "$1" in
    --app) APP_HOST="$2"; shift 2 ;;
    --admin) ADMIN_HOST="$2"; shift 2 ;;
    --api) API_HOST="$2"; shift 2 ;;
    --deploy-key) DEPLOY_KEY="$2"; shift 2 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done
# shellcheck disable=SC2015
[ -n "$APP_HOST" ] && [ -n "$ADMIN_HOST" ] && [ -n "$API_HOST" ] && [ -f "$DEPLOY_KEY" ] || {
  echo "usage: sudo bash setup.sh --app HOST --admin HOST --api HOST --deploy-key /path/to/public_key.pub" >&2; exit 2; }
grep -qE '^ssh-(ed25519|rsa) ' "$DEPLOY_KEY" || { echo "$DEPLOY_KEY is not a public SSH key (ssh-ed25519 ...)" >&2; exit 2; }
ARCH="$(dpkg --print-architecture)" # arm64 or amd64
HERE="$(cd "$(dirname "$0")" && pwd)"

# 1. Users and folders. numerix runs the services; deploy only installs releases (see numerix-deploy).
id numerix >/dev/null 2>&1 || useradd --system --home /var/lib/numerix --shell /usr/sbin/nologin numerix
id deploy >/dev/null 2>&1 || useradd --create-home --shell /bin/bash deploy
passwd -l deploy >/dev/null
install -d -o numerix -g numerix -m 750 /var/lib/numerix /var/lib/numerix/backups
for c in api web admin seed; do install -d -o deploy -g numerix -m 755 "/opt/numerix/$c" "/opt/numerix/$c/releases"; done
install -d -m 750 -g numerix /etc/numerix

# 2. Packages: Caddy from its official apt repository, sqlite3 for backups.
apt-get update -y
apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl gpg python3 sqlite3 xz-utils
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' >/etc/apt/sources.list.d/caddy-stable.list
apt-get update -y && apt-get install -y caddy

# 3. Litestream: pin the reviewed release and verify the architecture-specific package before installing it.
case "$ARCH" in
  arm64) RELEASE_ARCH="arm64"; SHA256="7d34ed3356844fe3dd127462b9f4ea7e79ef14679eadf2fed6c737dcb797358e" ;;
  amd64) RELEASE_ARCH="x86_64"; SHA256="a191a0928884d1820fab1f866ede1d0d5811c323d0587bf863a43481a82a7668" ;;
  *) echo "unsupported Litestream architecture: $ARCH" >&2; exit 1 ;;
esac
VERSION="0.5.17"
DEB="$(mktemp /tmp/litestream.XXXXXX.deb)"
trap 'rm -f "$DEB"' EXIT
URL="https://github.com/benbjohnson/litestream/releases/download/v${VERSION}/litestream-${VERSION}-linux-${RELEASE_ARCH}.deb"
curl --fail --silent --show-error --location "$URL" -o "$DEB"
echo "$SHA256  $DEB" | sha256sum --check --status || { echo "Litestream checksum verification failed" >&2; exit 1; }
dpkg -i "$DEB"

# 4. Node.js from nodejs.org, checksum verified (Ubuntu's own package is too old for Next.js 16).
NARCH=$([ "$ARCH" = arm64 ] && echo arm64 || echo x64)
if [ ! -x /opt/numerix/node/bin/node ]; then
  cd /tmp
  curl -fsSLO "https://nodejs.org/dist/${NODE_VERSION}/node-${NODE_VERSION}-linux-${NARCH}.tar.xz"
  curl -fsSL "https://nodejs.org/dist/${NODE_VERSION}/SHASUMS256.txt" | grep "node-${NODE_VERSION}-linux-${NARCH}.tar.xz" | sha256sum -c -
  rm -rf /opt/numerix/node && mkdir -p /opt/numerix/node
  tar -xJf "node-${NODE_VERSION}-linux-${NARCH}.tar.xz" -C /opt/numerix/node --strip-components=1
fi

# 5. The deploy gate and the restricted deploy key (cannot open a shell, cannot forward ports).
install -m 755 -o root -g root "$HERE/numerix-deploy" /usr/local/bin/numerix-deploy
install -d -o root -g deploy -m 750 /home/deploy/.ssh
printf 'restrict,command="/usr/local/bin/numerix-deploy" %s\n' "$(head -1 "$DEPLOY_KEY")" >/home/deploy/.ssh/authorized_keys
chown root:deploy /home/deploy/.ssh/authorized_keys && chmod 640 /home/deploy/.ssh/authorized_keys
cat >/etc/sudoers.d/numerix-deploy <<'SUDO'
deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart numerix-api, /usr/bin/systemctl restart numerix-web, /usr/bin/systemctl restart numerix-admin, /usr/bin/systemctl stop numerix-api, /usr/bin/systemctl start numerix-api, /usr/bin/systemctl stop litestream, /usr/bin/systemctl start litestream
deploy ALL=(numerix) NOPASSWD: /usr/local/bin/numerix-deploy _backup, /usr/local/bin/numerix-deploy _restore *, /usr/local/bin/numerix-deploy _seed *
SUDO
chmod 440 /etc/sudoers.d/numerix-deploy && visudo -cf /etc/sudoers.d/numerix-deploy

# 6. Config files from this folder.
sed -e "s/__APP_HOST__/${APP_HOST}/g" -e "s/__ADMIN_HOST__/${ADMIN_HOST}/g" -e "s/__API_HOST__/${API_HOST}/g" "$HERE/Caddyfile" >/etc/caddy/Caddyfile
for u in numerix-api numerix-web numerix-admin; do install -m 644 "$HERE/$u.service" "/etc/systemd/system/$u.service"; done
install -m 640 -g numerix "$HERE/litestream.yml" /etc/litestream.yml
install -m 755 "$HERE/verify-backup.sh" /usr/local/bin/numerix-verify-backup
install -m 644 "$HERE/numerix-verify-backup.service" "$HERE/numerix-verify-backup.timer" /etc/systemd/system/
for f in api web admin; do
  tpl="$HERE/$f.env.example"; [ "$f" = api ] && tpl="$HERE/env.example"
  [ -f "/etc/numerix/$f.env" ] || install -m 640 -g numerix "$tpl" "/etc/numerix/$f.env"
done

# Backup credentials live in ONE file the owner fills by hand (never in the repo, never in chat).
[ -f /etc/litestream.env ] || { install -m 640 -o root -g numerix /dev/null /etc/litestream.env; printf 'LITESTREAM_ACCESS_KEY_ID=
LITESTREAM_SECRET_ACCESS_KEY=
' >/etc/litestream.env; }
install -d /etc/systemd/system/litestream.service.d
# runs as numerix like the API: files it creates next to the database must be writable by the API
printf '[Service]
User=numerix
Group=numerix
EnvironmentFile=/etc/litestream.env
' >/etc/systemd/system/litestream.service.d/override.conf

# Automatic security updates of the operating system
apt-get install -y unattended-upgrades

# 7. Firewall: Oracle images ship iptables rules that block 80 and 443 even after the cloud rule opens them.
# Other hosts (OVH, Hetzner...) usually have no such rule, so this only acts when iptables exists and rejects by default.
if command -v iptables >/dev/null && iptables -S INPUT 2>/dev/null | grep -q REJECT; then
iptables -C INPUT -p tcp --dport 443 -j ACCEPT 2>/dev/null || {
  iptables -I INPUT 6 -m state --state NEW -p tcp --dport 80 -j ACCEPT
  iptables -I INPUT 6 -m state --state NEW -p tcp --dport 443 -j ACCEPT
  # shellcheck disable=SC2015
  command -v netfilter-persistent >/dev/null && netfilter-persistent save || true
}
fi

systemctl daemon-reload
systemctl enable numerix-api numerix-web numerix-admin caddy litestream numerix-verify-backup.timer
systemctl restart caddy
echo "setup done. Next: fill /etc/numerix/*.env and /etc/litestream.yml, then run the deploy workflow."
