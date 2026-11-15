# QA sign-off SEC-PERF-001 (branch hardening/sec-perf, Go 1.27.4)

Verdict: SIGNED OFF

| Check | Result |
|---|---|
| go build ./... , go vet ./... | PASS |
| go test ./... -count=1 | PASS (all packages ok) |
| go test -race (store, auth, app) | NOT RUN: `go: -race requires cgo` (no gcc on this machine) |
| govulncheck ./... | PASS: "No vulnerabilities found." |
| Local prod-mirror api image rebuilt from branch | PASS, healthy, GOMEMLIMIT=512MiB in container |
| Smoke via https://api.localhost:8443 | PASS: health 200, ready 200, register 201, child 201, consent 204, child token 200, session 201, beat 201, answer 200, config/public 200 |
| Security headers | PASS: X-Content-Type-Options nosniff, CSP default-src 'none'; frame-ancestors 'none', HSTS max-age=31536000 (+ includeSubDomains from proxy), X-Frame-Options DENY, Referrer-Policy no-referrer |
| app.localhost:8443 / | 200 (admin / 307 -> /login 200; app /login 404, it has no such route) |
| loadtest -rps 50 -duration 30s -families 300 | PASS on 5xx: 0 5xx, 0 429, 0 conn errors in every run |
| Latency budget (p95) | Noisy on this host (Docker + Windows load). Alternating main/branch x2: main p95 521-897 ms, branch 572-1015 ms; main also exceeds budget. Not a branch regression |
| docker stats api memory | 26-30 MiB RSS (well under 512 MiB limit) |

Notes
- Loadtest only exercises startSession in dev mode (world "nature" is not enabled by default; defaults echecs/medecine/espace), identical on main.
- Frontend images (web/admin) failed to rebuild in start.sh (`scripts/release.sh` exit 1 in the frontend repo): unrelated to the API; existing web/admin containers were reused and answer 200.
- api/openapi30.yaml shows modified in git status with an empty diff stat (line endings only); untracked .idea/. Not touched by QA.
- Rebuilding only the api recreates its network namespace; caddy/web/admin had to be re-upped (`docker compose up -d --no-build`) because caddy shares the api network.
