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
apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl gpg sqlite3 xz-utils
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' >/etc/apt/sources.list.d/caddy-stable.list
apt-get update -y && apt-get install -y caddy

# 3. Litestream: latest .deb for this CPU from GitHub releases.
URL="$(curl -s https://api.github.com/repos/benbjohnson/litestream/releases/latest | grep browser_download_url | grep -E "linux-${ARCH}.*\.deb" | head -1 | cut -d '"' -f 4)"
[ -n "$URL" ] || { echo "could not find the Litestream .deb for $ARCH, install it by hand"; exit 1; }
curl -sL "$URL" -o /tmp/litestream.deb && dpkg -i /tmp/litestream.deb

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
install -d -o deploy -g deploy -m 700 /home/deploy/.ssh
printf 'restrict,command="/usr/local/bin/numerix-deploy" %s\n' "$(head -1 "$DEPLOY_KEY")" >/home/deploy/.ssh/authorized_keys
chown deploy:deploy /home/deploy/.ssh/authorized_keys && chmod 600 /home/deploy/.ssh/authorized_keys
cat >/etc/sudoers.d/numerix-deploy <<'SUDO'
deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart numerix-api, /usr/bin/systemctl restart numerix-web, /usr/bin/systemctl restart numerix-admin, /usr/bin/systemctl stop numerix-api, /usr/bin/systemctl start numerix-api
deploy ALL=(numerix) NOPASSWD: /usr/local/bin/numerix-deploy _backup, /usr/local/bin/numerix-deploy _restore *, /usr/local/bin/numerix-deploy _seed *
SUDO
chmod 440 /etc/sudoers.d/numerix-deploy && visudo -cf /etc/sudoers.d/numerix-deploy

# 6. Config files from this folder.
sed -e "s/__APP_HOST__/${APP_HOST}/g" -e "s/__ADMIN_HOST__/${ADMIN_HOST}/g" -e "s/__API_HOST__/${API_HOST}/g" "$HERE/Caddyfile" >/etc/caddy/Caddyfile
for u in numerix-api numerix-web numerix-admin; do install -m 644 "$HERE/$u.service" "/etc/systemd/system/$u.service"; done
install -m 640 -g numerix "$HERE/litestream.yml" /etc/litestream.yml
install -m 755 "$HERE/verify-backup.sh" /usr/local/bin/numerix-verify-backup
install -m 644 "$HERE/numerix-verify-backup.service" "$HERE/numerix-verify-backup.timer" /etc/systemd/system/
for f in api web admin; do [ -f "/etc/numerix/$f.env" ] || install -m 640 -g numerix "$HERE/$f.env.example" "/etc/numerix/$f.env"; done

# Backup credentials live in ONE file the owner fills by hand (never in the repo, never in chat).
[ -f /etc/litestream.env ] || { install -m 640 -o root -g numerix /dev/null /etc/litestream.env; printf 'LITESTREAM_ACCESS_KEY_ID=
LITESTREAM_SECRET_ACCESS_KEY=
' >/etc/litestream.env; }
install -d /etc/systemd/system/litestream.service.d
printf '[Service]
EnvironmentFile=/etc/litestream.env
' >/etc/systemd/system/litestream.service.d/override.conf

# 7. Firewall: Oracle images ship iptables rules that block 80 and 443 even after the cloud rule opens them.
iptables -C INPUT -p tcp --dport 443 -j ACCEPT 2>/dev/null || {
  iptables -I INPUT 6 -m state --state NEW -p tcp --dport 80 -j ACCEPT
  iptables -I INPUT 6 -m state --state NEW -p tcp --dport 443 -j ACCEPT
  command -v netfilter-persistent >/dev/null && netfilter-persistent save || true
}

systemctl daemon-reload
systemctl enable numerix-api numerix-web numerix-admin caddy litestream numerix-verify-backup.timer
systemctl restart caddy
echo "setup done. Next: fill /etc/numerix/*.env and /etc/litestream.yml, then run the deploy workflow."
