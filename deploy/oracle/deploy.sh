#!/usr/bin/env bash
# MANUAL fallback (the normal path is the GitHub workflow). Run on YOUR PC from the API repo root:
#   bash deploy/oracle/deploy.sh <ssh-target> [arm64|amd64] [release-id]
# <ssh-target> is a login that may run "sudo -u deploy" (for example ubuntu@host). Uses the same numerix-deploy gate as CI,
# so a manual deploy takes a backup first and can be rolled back with the same verbs.
# shellcheck disable=SC2029 # the arguments are meant to expand on this PC; the VM runs the gate
set -euo pipefail

TARGET="${1:?usage: bash deploy/oracle/deploy.sh user@host [arm64|amd64] [release-id]}"
ARCH="${2:-arm64}"
ID="${3:-$(git rev-parse --short=12 HEAD)}"
export PATH="$PATH:/c/Program Files/Go/bin"
GATE="sudo -u deploy /usr/local/bin/numerix-deploy"

GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X github.com/Perrin-Nj/numerix-api/internal/app.Version=$ID" -o bin/api-linux ./cmd/api
SUM="$(sha256sum bin/api-linux | cut -d' ' -f1)"

ssh "$TARGET" "$GATE backup"
ssh "$TARGET" "$GATE put api $ID $SUM" <bin/api-linux
ssh "$TARGET" "$GATE activate api $ID"
ssh "$TARGET" "$GATE verify api"
echo "deployed $ID. public check: curl https://<api-host>/v1/health   rollback: ssh $TARGET '$GATE rollback api'"
