#!/usr/bin/env bash
# Regenerates everything derived from the contract. Run from the repo root: bash scripts/gen.sh
set -euo pipefail
cd "$(dirname "$0")/.."
export PATH="$PATH:/c/Program Files/Go/bin:$HOME/go/bin"
node scripts/openapi30.mjs                          # OpenAPI 3.1 -> 3.0.3 copy
oapi-codegen -config api/oapi.cfg.yaml api/openapi30.yaml   # models + strict server interface
node scripts/genmeta.mjs                            # op metadata + 501 stubs
gofmt -w internal/httpapi/opmeta_gen.go internal/app/unimplemented_gen.go
