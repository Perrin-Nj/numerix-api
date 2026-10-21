#!/usr/bin/env bash
# Offline test of the numerix-deploy gate: runs a patched copy against a temp folder with a fake sudo/systemctl.
# Checks put (sha256, immutability, unsafe archive), activate, rollback, bad input, and the forced-command path.
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
T="$(mktemp -d)"
trap 'rm -rf "${T:?}"' EXIT
mkdir -p "$T/bin" "$T/base"
: >"$T/systemctl.log"
sed -e "s#^BASE=.*#BASE=$T/base#" -e "s#/usr/bin/systemctl#systemctl#g" "$HERE/numerix-deploy" >"$T/bin/numerix-deploy"
chmod +x "$T/bin/numerix-deploy"
# fake sudo: drop -n and -u user, log systemctl calls instead of running them
cat >"$T/bin/sudo" <<SUDO
#!/usr/bin/env bash
while [ "\$1" = -n ] || [ "\$1" = -u ]; do [ "\$1" = -u ] && shift; shift; done
if [ "\$1" = systemctl ]; then echo "\$*" >>"$T/systemctl.log"; exit 0; fi
exec "\$@"
SUDO
chmod +x "$T/bin/sudo"
export PATH="$T/bin:$PATH"
for c in api web admin seed; do mkdir -p "$T/base/$c/releases"; done
g() { numerix-deploy "$@"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

printf 'binary-one' >"$T/a1"; printf 'binary-two' >"$T/a2"
s1="$(sha256sum "$T/a1" | cut -d' ' -f1)"
g put api r1 "$s1" <"$T/a1" >/dev/null
g put api r1 <"$T/a1" >/dev/null || fail "re-put of the same bytes (workflow re-run) must succeed"
g put api r1 <"$T/a2" 2>/dev/null && fail "different bytes under an existing release id must be refused"
g put api r9 "$(printf '0%.0s' $(seq 64))" <"$T/a1" 2>/dev/null && fail "wrong sha256 must be refused"
[ ! -e "$T/base/api/releases/r9" ] || fail "failed put left a release behind"
g put api r2 <"$T/a2" >/dev/null
g activate api r1 >/dev/null
[ "$(readlink "$T/base/api/current")" = releases/r1 ] || fail "current should be r1"
g activate api r2 >/dev/null
[ "$(readlink "$T/base/api/previous")" = releases/r1 ] || fail "previous should be r1"
g rollback api >/dev/null
[ "$(readlink "$T/base/api/current")" = releases/r1 ] || fail "rollback should put r1 back"
[ "$(cat "$T/base/api/current/api")" = binary-one ] || fail "current binary content"
[ "$(grep -c 'restart numerix-api' "$T/systemctl.log")" = 3 ] || fail "expected 3 restarts"

mkdir -p "$T/web/x" && echo "console.log(1)" >"$T/web/x/server.js"
tar -czf "$T/web.tgz" -C "$T/web/x" .
g put web w1 <"$T/web.tgz" >/dev/null
[ -f "$T/base/web/releases/w1/server.js" ] || fail "web archive not extracted"
tar -czf "$T/evil.tgz" -C "$T/web/x" --transform 's#^server.js#../server.js#' server.js
g put web evil <"$T/evil.tgz" 2>/dev/null && fail "unsafe archive must be refused"
g put database x <"$T/a1" 2>/dev/null && fail "unknown component must be refused"
g put api '../x' <"$T/a1" 2>/dev/null && fail "bad release id must be refused"
g activate api nope 2>/dev/null && fail "activate of a missing release must fail"
g bogus 2>/dev/null && fail "unknown verb must fail"
SSH_ORIGINAL_COMMAND="status" g | grep '^api ' >/dev/null || fail "forced-command status"
SSH_ORIGINAL_COMMAND="put api r3" g <"$T/a2" >/dev/null || fail "forced-command put"
[ -x "$T/base/api/releases/r3/api" ] || fail "forced-command put result"
echo "numerix-deploy gate: all checks passed"
