#!/usr/bin/env bash
# Simple secret scan of every tracked file and of the whole git history. Exit 1 when something looks like a real secret.
# It is a safety net, not a guarantee: it knows the shapes of the secrets this project uses (private keys, Atlas
# connection strings with a password, Anthropic/GitHub/AWS-style keys, long hex or base64 values assigned to *_SECRET,
# *_TOKEN, *_PASSWORD or *_KEY). Placeholders such as "dev-admin-token", empty values and <angle-bracket> values pass.
#   bash scripts/secret-scan.sh            scan
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
PATTERNS=(
  '-----BEGIN [A-Z ]*PRIVATE KEY-----'
  'mongodb(\+srv)?://[^:/@[:space:]<>]+:[^@[:space:]<>]{6,}@'
  'sk-ant-[A-Za-z0-9_-]{20,}'
  'gh[pousr]_[A-Za-z0-9]{30,}'
  'github_pat_[A-Za-z0-9_]{30,}'
  'AKIA[0-9A-Z]{16}'
  '(SECRET|TOKEN|PASSWORD|API_KEY|ACCESS_KEY)[A-Z_]*[=:][[:space:]]*["'"'"']?[A-Za-z0-9+/_-]{32,}["'"'"']?[[:space:]]*$'
)
rx="$(IFS='|'; echo "${PATTERNS[*]}")"
fail=0
# 1. current files (skip lock files, generated code, test fixtures that use obvious fake values)
hits="$(git grep -nIE -e "$rx" -- . ':(exclude)*.lock' ':(exclude)pnpm-lock.yaml' ':(exclude)package-lock.json' ':(exclude)go.sum' ':(exclude)api/*.gen.go' || true)"
hits="$(printf '%s\n' "$hits" | grep -vE 'example|placeholder|dummy|changeme|xxxx|\.test\.|_test\.go|fake|0123456789|dev-only' || true)"
if [ -n "$(printf '%s' "$hits" | tr -d '[:space:]')" ]; then echo "possible secrets in tracked files:"; printf '%s\n' "$hits" | cut -c1-160; fail=1; fi
# 2. history (added lines only)
hist="$(git log -p --all -G"$rx" --pretty=format:'commit %h' 2>/dev/null | grep -E "^\+" | grep -E -e "$rx" | grep -vE 'example|placeholder|dummy|changeme|xxxx|_test|fake|0123456789|dev-only' | head -5 || true)"
if [ -n "$(printf '%s' "$hist" | tr -d '[:space:]')" ]; then echo "possible secrets in git history (rotate them, then rewrite or accept):"; printf '%s\n' "$hist" | cut -c1-120; fail=1; fi
[ "$fail" = 0 ] && echo "secret scan: nothing suspicious found"
exit "$fail"
