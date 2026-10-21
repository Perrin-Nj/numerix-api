# Handoff: pipeline, env files, act tests (agent stopped, tokens ended)

## Task and owner goal
Everything needed for production lives in the repos so the owner only: buys the domain, creates OVH/Oracle accounts and VMs, sets GitHub secrets, presses Approve.
Path: OVH VPS-1 Beauharnois (prod, amd64), Oracle Always Free (staging, arm64), Porkbun .ca, backups Oracle bucket + Backblaze B2.
Rules: rtk prefix, no AI attribution, no real secrets, do NOT push, never taskkill /IM, own ports 8360-8369 / 3360-3369.

## Worktrees
Branch prod/go-live. API: C:\Users\Wandj\AppData\Local\Temp\claude\C--Users-Wandj-OneDrive-Documents-Numerix\5d717d80-429a-4e4f-95c3-84b74db517fd\scratchpad\gl-api. Frontend: same folder, gl-fe.
Review agents (pass 2, pass 3) also edit these branches: re-read before editing, small commits, never revert their work.

## DONE
- Both repos, commit "ci(deploy): skip cleanly when no server is configured yet" (API 2ab277e, FE b3bb1ba):
  - deploy-staging.yml: new `gate` job (environment: staging, reads vars.DEPLOY_HOST). Empty -> notice + summary, build and deploy skipped (green). CI (`checks`) still runs.
  - deploy-prod.yml: new `gate` job reads REPOSITORY variable PROD_APPROVAL_CONFIRMED. Empty -> skipped green. Other value than `yes` -> prepare errors. (Production environment variables cannot be read without triggering the approval, hence a repo variable.) Prod only runs on a v*.*.* tag push anyway, never on merge to main.
- Verified facts: act 0.2.83 installed (WinGet). Docker Desktop installed; engine was stopped, I started it and `docker info` now answers (Server 28.3.2). WSL has Ubuntu (stopped). shellcheck exists.
- Env inventory gathered: API reads PORT LISTEN_HOST APP_ENV DB_PATH JWT_SECRET ADMIN_API_TOKEN FEATURE_WELCOME_GIFT TRUST_PROXY ACCESS_TTL_SECONDS CHILD_TTL_SECONDS REFRESH_TTL_SECONDS REQUEST_TIMEOUT_SECONDS MAX_CONCURRENT_REQUESTS RATE_LIMIT_IP_PER_MIN PPROF LLM_PROVIDER LLM_API_KEY LLM_MODEL LLM_MONTHLY_CAP MONGODB_URI MONGODB_DB MONGO_PSEUDO_SECRET ASSET_BASE_URL MEDIA_DIR MEDIA_BASE_URL ALLOWED_ORIGINS ADMIN_ORIGIN WEBAUTHN_RP_ID WEBAUTHN_RP_NAME WEBAUTHN_ORIGINS ANTHROPIC_API_KEY ANTHROPIC_BASE_URL ANTHROPIC_DRAFT_MODEL NARRATOR_MODEL (config.go + internal/drafter). Web: API_URL, IMAGE_HOSTS, CONNECT_ORIGINS, VERCEL_REGIONS, NEXT_OUTPUT, NX_DIST, NEXT_PUBLIC_* (12, see src/config/env.ts). Admin: ADMIN_PASSWORD(min 8) ADMIN_SESSION_SECRET(min 32) PLATFORM_API_URL ADMIN_API_TOKEN.
- Existing: FE config/env/{development,preview,production}.env (committed); API deploy/oracle/{env,admin.env,web.env}.example (single set).

## NOT DONE
1. Frontend config/env/staging.env. Replace invalid placeholder API_URL in production.env with YOURDOMAIN form.
2. .env.example in API root, FE root, FE admin/ (gitignore has !.env.example in API; check FE `.env*` rule at line 20 needs `!.env.example`).
3. deploy/oracle/{api,web,admin}.{staging,production}.env.example, every variable above commented (purpose, generate command, required/optional), token YOURDOMAIN.
4. scripts/render-env.sh (--domain example.ca --env staging|production; fills hosts and passkey lines, secrets blank, prints openssl commands; output dir gitignored; shellcheck clean; tested).
5. Test that every variable in config.go, drafter and the Next/admin schemas appears in the examples (Go test in API; vitest in FE).
6. act runs (both repos, CI and deploy workflows in skip mode) and docs/ACT_TEST_RESULTS.md. No act run done yet.
7. Setup docs: OVH specifics (amd64, login user from order email), keep Oracle working.
8. Section "what happens on push to main" (workflows triggered, Vercel project risk, alerts, no missing secret) in PROD_GO_LIVE.md; update PROD_GO_LIVE.md and PROD_OWNER_CHECKLIST.md (checklist still says DEPLOY_HOST per environment: now also repo variable PROD_APPROVAL_CONFIRMED may be empty until prod is ready); copy finals to Numerix\docs and both repos' docs.
9. Uncommitted edits by other agents exist in gl-api (.gitattributes, ci.yml, deploy/oracle/*, passkeys_test.go, db-prod-deploy.sh, docs); I did NOT commit them. gl-fe has untracked dist/ (do not commit).

## Next steps in order
1. In each worktree run `git status`; do not discard others' changes.
2. Do items 1-5 above, commit per item.
3. act: Docker Desktop must be running (start "C:\Program Files\Docker\Docker\Docker Desktop.exe"). Then `act -l`, `act pull_request -W .github/workflows/ci.yml -P ubuntu-latest=catthehacker/ubuntu:act-latest --artifact-server-path <scratch> --secret-file .secrets` (untracked, dummy values; add to .gitignore). Heavy jobs (suite, playwright) may need skipping with `-j`. Run `act push -W .github/workflows/deploy-staging.yml -j gate` and prod gate to prove the skip. Record pass/fail per job.
4. Fix workflow bugs found, write ACT_TEST_RESULTS.md (also list what act cannot test: environments with reviewers, OIDC, real SSH, vars scoped to environments).
5. Docs (item 7, 8), copy files, commit. Do not push.

## Acceptance checklist
- [ ] gate jobs skip green with empty DEPLOY_HOST / PROD_APPROVAL_CONFIRMED (verify with act -j gate)
- [ ] actionlint and yamllint clean on workflows
- [ ] staging.env and 3 .env.example files exist, no secrets
- [ ] 6 deploy/oracle example files, all variables commented
- [ ] render-env.sh shellcheck clean, tested, output gitignored
- [ ] variable-coverage test passes in both repos
- [ ] act results per job in docs/ACT_TEST_RESULTS.md
- [ ] OVH notes in setup docs; push-to-main impact listed
- [ ] PROD_GO_LIVE.md and PROD_OWNER_CHECKLIST.md updated and copied to Numerix\docs and both repos

## Risks
- Gate for prod uses a repo variable, differs from the literal "DEPLOY_HOST" request.
- Docker Desktop was started by me and may keep running (memory use).
