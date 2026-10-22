# Review pass 3: repo reality and security (handoff)

## Task
Verify every PROD_GO_LIVE.md claim against the repos; boot the real API under APP_ENV=production from deploy/vps/env.example; run web and admin standalone with forwarded headers; security-review the deploy design (deploy key, sudoers, Caddy, systemd, workflows, admin exposure, CSP); fix defects with small edits and tests; update the docs; copy docs to master folder and both repos.

## DONE
- API (gl-api, branch prod/go-live): built real binary, ran with a filled env.example on port 8350 with temp DB: /v1/health and /v1/ready 200, admin API 401 without token and 200 with, pprof 404.
- Production guards confirmed by running the binary: short or empty JWT_SECRET, APP_ENV=staging, PPROF=true, http origin, WEBAUTHN origin outside RP id, RP id without origins, short ADMIN_API_TOKEN, MONGODB_URI without secret, http/localhost ADMIN_ORIGIN are all rejected. Note: empty ADMIN_API_TOKEN is NOT rejected (admin API silently disabled).
- Added test TestPasskeysProductionShape (internal/app/passkeys_test.go) - passes; real-domain WebAuthn shape works. Committed.
- Admin (gl-fe/admin) built standalone and run on 3350 with Host/X-Forwarded-* headers: redirects are relative, nx_admin cookie is Secure/HttpOnly/SameSite=strict, login limiter blocks on 6th try (keyed by first X-Forwarded-For, which Caddy overwrites).
- Web (gl-fe/.next/standalone) run on 3351: CSP and security headers present, CSRF blocks foreign Origin (403), register through proxy sets Secure cookies (nx_parent, nx_refresh, nx_device). Env names in doc section 8 match config.go, admin/src/lib/env.ts and src/config.
- Another reviewer already fixed (HEAD 2ab277e): idempotent put, actions:read on prod deploy job, ci concurrency group includes github.workflow.

## NOT done (findings still open; none applied yet)
1. numerix-deploy put (web/admin/seed): tar check only blocks absolute and .. names. Add: reject hard links, and reject any entry whose path lies under a symlink entry of the same archive (extraction through an in-archive symlink). Do NOT ban ".." in symlink targets (pnpm standalone needs them). Also rm -rf "$dir.part" before mkdir. Add cases to deploy/vps/test-gate.sh.
2. setup.sh: make /home/deploy/.ssh and authorized_keys root-owned (root:deploy 750 / 640) so the deploy key cannot rewrite its own forced command.
3. deploy-staging.yml (API) failure step rolls back even when failure happened before activate (swaps to an OLDER release). Set ACTIVATED=1 in GITHUB_ENV after activate and use if: failure() && env.ACTIVATED == '1'. Same idea per component in the web repo staging workflow.
4. Web repo keepalive.yml: no permissions block; ${{ vars.KEEPALIVE_URLS }} interpolated into run (move to env). Cron every 5 min likely exceeds the free private-repo Actions minutes (UNVERIFIED): say so in doc section 4.
5. Admin app has no CSP (admin/next.config.ts). Safe to add default-src 'self'; script-src/style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'; object-src 'none' (no external URLs found in admin/src). Test in a browser.
6. Caddyfile: add commented IP-allowlist snippet for admin host (@blocked not remote_ip CIDR / respond 403), HSTS includeSubDomains decision; systemd: add ProtectKernelTunables, ProtectControlGroups, RestrictSuidSgid, LockPersonality, RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX, MemoryMax on api; confirm web ProtectSystem=strict does not break Next cache writes (not testable here).
7. GitHub: production environment should restrict deployment branches/tags (workflow_dispatch can run a workflow file from any branch); pin pnpm/action-setup by SHA (third-party). Add both to doc G2/section 16.
8. Doc: add undocumented optional env vars ANTHROPIC_DRAFT_MODEL, ANTHROPIC_BASE_URL (section 8.1); note ADMIN_API_TOKEN empty = admin API off, and admin app requires it non-empty; note that the seed/web/admin tarball holder gets code execution as numerix (reads api.env, DB) but not root: sudoers entries are fixed-argument.
9. Copy final PROD_GO_LIVE.md and PROD_OWNER_CHECKLIST.md to master docs folder and both repos' docs/; commit.

## Resume
- Worktrees: C:\Users\Wandj\AppData\Local\Temp\claude\C--Users-Wandj-OneDrive-Documents-Numerix\5d717d80-429a-4e4f-95c3-84b74db517fd\scratchpad\gl-api and \gl-fe (branch prod/go-live). Re-read files first: another reviewer commits to them. Many files show "M" only from CRLF noise (git diff is empty).
- Go: PATH needs /c/Program Files/Go/bin. Run gate test: bash deploy/vps/test-gate.sh (needs bash/WSL Ubuntu). Local run kit in scratchpad\gl-run (api.exe, api.env, ports 8350 API, 3350 admin, 3351 web; all stopped). Admin build: cd gl-fe/admin; npm ci; NEXT_OUTPUT=standalone npm run build. Web standalone from the Windows tarball is broken (pnpm symlinks); run gl-fe/.next/standalone directly or build on Linux/WSL.

## Residual risks
- Stolen deploy key = code execution as numerix (by design), not root.
- Workflows never run on GitHub; systemd units and Caddy not run (no systemd here); Litestream, WSL gate test not rerun by me.
- Admin on a public hostname is password-only with in-memory rate limit.

## Acceptance criteria
- [ ] put rejects hard links and writes through in-archive symlinks; test-gate.sh covers both and passes
- [ ] authorized_keys root-owned in setup.sh
- [ ] staging failure step only rolls back after activate (API and web repos)
- [ ] keepalive.yml has permissions and no expression in run
- [ ] admin CSP added and admin pages load in a browser
- [ ] Caddy admin allowlist snippet and systemd extra hardening added
- [ ] doc sections 4, 8.1, 15, 16, G2 updated; docs copied to master folder and both repos
- [ ] go test ./... and shellcheck pass; commits on prod/go-live in both repos; nothing pushed
