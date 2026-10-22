# Production readiness validation

Checked 2026-10-21 in the local `prod/go-live` worktrees. No secrets, commits, pushes, or production hosts were used.

## Passed

| Check | Result |
|---|---|
| API Go tests | `go test ./... -count=1`: 547 tests passed across 23 packages |
| API static checks | `go vet ./...`: passed |
| API dependency scan | `govulncheck ./...`: no vulnerable call found |
| Deployment archive gate | `bash deploy/vps/test-gate.sh`: all checks passed |
| Deployment shell lint | ShellCheck passed for setup, deploy, test-gate, and env-render scripts |
| Workflow lint | `actionlint` and repository-configured `yamllint` passed for API and frontend deploy workflows |
| Deployment gate simulations | `act` 0.2.89 passed all four API/web staging and production gate simulations with deploy settings empty; runs were sequential and did not enable deployment |
| Frontend checks | `pnpm check` passed; ESLint reported 8 warnings in unmodified QA scripts, no errors |
| Frontend production build | `pnpm build` passed; Next emitted a font fallback warning |
| QA candidate frontend build | `pnpm exec next build` passed in the round-4 frontend worktree; Next emitted a font fallback warning |
| Admin checks/build | Typecheck, lint, and isolated production build passed; lint reported 3 warnings in unmodified navigation files; Next emitted a font fallback warning |
| Local runtime smoke | Production-mode API `/v1/health` and `/v1/ready` returned 200; `/admin/v1/overview` returned 401 without a token and 200 with a dummy token; listener bound to `127.0.0.1`; web `/` and admin `/login` returned 200 |
| Admin security headers | `/login` returned the configured Content-Security-Policy; other headers were not asserted in the smoke output |

The API build reported no reachable dependency vulnerability. `golang.org/x/crypto` remains required for Argon2; `govulncheck -show verbose` reported GO-2026-5932 for its unused `openpgp` package, with no fixed version listed. Treat this as a module-level advisory to monitor, not a clean bill for all dependencies.

## Blocked / not run

The first parallel `act` gate attempt collided on Docker container names; rerunning the four simulations sequentially passed. These were safe gate simulations only, not full workflows or live deployments.

Focused round-4 checks now pass on candidate heads frontend `578ea5d` and API `7f92545`: first-run, journey flows, UI/voice behavior, feed, and admin propagation. The UI/flow reruns used an unseeded API; an earlier attempt used curated questions, so those failures were invalid. The feed duplicate assertion passed in both suite runs. A memory threshold failed narrowly once (`17.4` to `39.3 MB`) and passed on the repeat. The candidate frontend `pnpm check` and production build passed; the build reports the existing font fallback warning.

The initial stack attempt used default ports because PowerShell variables did not cross into the WSL Bash invoked by `rtk`. The corrected run used Git Bash with variables exported inside Bash and verified API/web health before QA. Production hardening is not yet reconciled with this candidate build. Full-suite, load, physics-frame, and owner acceptance gates remain open.

Local checks did not validate GitHub reviewer protections, environment variables/secrets, actual SSH deployment, Caddy/systemd behavior, public TLS/firewall configuration, Litestream backups or restore, external monitoring, legal readiness, or release/r3 QA. QA round 4 remains NOT SIGNED OFF for the exact candidate. Do not push `main`, tag, or deploy before QA sign-off and owner approval.

The local smoke processes were stopped after checks. Temporary SQLite files were removed.
