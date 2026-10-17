#!/usr/bin/env bash
# Run ONCE on the Oracle VM (Ubuntu, ARM or x86) as a sudo user: sudo bash setup.sh api.example.com
# It installs Caddy (HTTPS) and Litestream (SQLite backup), creates the service user and folders,
# and installs the systemd units. It does not start the API: deploy.sh copies the binary first.
set -euo pipefail

DOMAIN="${1:?usage: sudo bash setup.sh <domain>}"
ARCH="$(dpkg --print-architecture)" # arm64 or amd64

# 1. Service user without a shell, data and config folders.
id numerix >/dev/null 2>&1 || useradd --system --home /var/lib/numerix --shell /usr/sbin/nologin numerix
install -d -o numerix -g numerix -m 750 /var/lib/numerix /opt/numerix
install -d -m 750 -g numerix /etc/numerix

# 2. Caddy from its official apt repository (automatic HTTPS certificates).
apt-get update -y
apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' >/etc/apt/sources.list.d/caddy-stable.list
apt-get update -y && apt-get install -y caddy

# 3. Litestream: latest .deb for this CPU from GitHub releases.
URL="$(curl -s https://api.github.com/repos/benbjohnson/litestream/releases/latest | grep browser_download_url | grep -E "linux-${ARCH}.*\.deb|linux-${ARCH}.*\.deb" | head -1 | cut -d '"' -f 4)"
[ -n "$URL" ] || { echo "could not find the Litestream .deb for $ARCH, install it by hand"; exit 1; }
curl -sL "$URL" -o /tmp/litestream.deb && dpkg -i /tmp/litestream.deb

# 4. Config files from this folder (next to this script).
HERE="$(cd "$(dirname "$0")" && pwd)"
sed "s/__DOMAIN__/${DOMAIN}/g" "$HERE/Caddyfile" >/etc/caddy/Caddyfile
install -m 644 "$HERE/numerix-api.service" /etc/systemd/system/numerix-api.service
install -m 640 -g numerix "$HERE/litestream.yml" /etc/litestream.yml
[ -f /etc/numerix/api.env ] || install -m 640 -g numerix "$HERE/env.example" /etc/numerix/api.env

# 5. Firewall: Oracle images ship iptables rules that block 80 and 443 even after the cloud rule opens them.
iptables -I INPUT 6 -m state --state NEW -p tcp --dport 80 -j ACCEPT
iptables -I INPUT 6 -m state --state NEW -p tcp --dport 443 -j ACCEPT
command -v netfilter-persistent >/dev/null && netfilter-persistent save || true

systemctl daemon-reload
systemctl enable numerix-api caddy litestream
echo "setup done. Edit /etc/numerix/api.env, then run deploy.sh from your PC."
