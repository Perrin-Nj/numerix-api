# SEC-PERF-001: security, memory and CPU hardening

Goal: raise security and cut memory/CPU with no API or behavior change.
Branch: hardening/sec-perf. Never push main without QA sign-off (docs/QA_SIGNOFF.md).

## Requirements
- R1 Go toolchain at latest 1.27 patch (fixes GO-2026-6859 net/http).
- R2 Direct and indirect modules upgraded; govulncheck reports 0 findings (GO-2026-6865 klauspost/compress).
- R3 HTTP server has timeouts, body limits, security headers (verify before adding; keep existing behavior).
- R4 Memory: find top allocators via pprof under load (cmd/loadtest); fix with bounded caches/pools/limits; GOMEMLIMIT set in service unit.
- R5 CPU: fix top hot spots found by pprof only.
- R6 Docker/CI/Actions pinned and current.

## Acceptance (every change)
1. go build, go vet, go test ./... green.
2. API contract tests and loadtest unchanged results.
3. Local stack (deploy/local, mirrors prod) smoke OK.
4. QA agent signs off in docs/QA_SIGNOFF.md before any push.

## Out of scope
Frontend/admin changes, schema changes, new features.
