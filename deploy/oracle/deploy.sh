#!/usr/bin/env bash
# Run on YOUR PC from Services/numerix-api: bash deploy/oracle/deploy.sh ubuntu@<vm-ip> [arm64|amd64]
# Builds a static Linux binary (no cgo), copies it to the VM, restarts the service, checks health.
set -euo pipefail

TARGET="${1:?usage: bash deploy/oracle/deploy.sh user@host [arm64|amd64]}"
ARCH="${2:-arm64}"
export PATH="$PATH:/c/Program Files/Go/bin"

# Static cross-compile: CGO off means one self-contained file, no C libraries on the server.
GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/api-linux ./cmd/api

scp bin/api-linux "$TARGET:/tmp/numerix-api"
ssh "$TARGET" 'sudo install -o numerix -g numerix -m 755 /tmp/numerix-api /opt/numerix/api && sudo systemctl restart numerix-api && sleep 2 && systemctl is-active numerix-api && curl -fsS http://127.0.0.1:8081/v1/health'
echo
echo "deployed. public check: curl https://<your-domain>/v1/health"
