#!/usr/bin/env bash
# Daily proof that the backup is usable: restores the newest Litestream replica into /tmp and checks it.
# Also keeps CPU and network lightly busy, which helps against Oracle's idle-instance rule (it is not a guarantee).
# Fails (non-zero, shows in "systemctl status numerix-verify-backup") when the restore or the check fails.
set -euo pipefail
out="$(mktemp -d)"; trap 'rm -rf "$out"' EXIT
litestream restore -config /etc/litestream.yml -o "$out/restored.db" /var/lib/numerix/numerix.db
[ "$(sqlite3 "$out/restored.db" 'PRAGMA integrity_check;')" = ok ] || { echo "restored copy failed integrity_check" >&2; exit 1; }
echo "backup restore ok: $(sqlite3 "$out/restored.db" 'SELECT COUNT(*) FROM bank_questions;') questions, $(date -u +%FT%TZ)"
