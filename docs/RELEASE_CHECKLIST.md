# Release checklist

Tick every line before the frontend branch `chore/go-backend-cutover` is merged to `main` (a push to `main` deploys the web and admin apps by itself). Detailed runbooks: `DEPLOY_ORACLE.md`, `DB_PROD_DEPLOY.md`, `MONGO_SYNC.md`, `ASSETS.md`, `LOAD_TEST.md`.

## 1. API environment (VM file `/etc/numerix/api.env`, template `deploy/oracle/env.example`)

| Variable | Required | Value |
|---|---|---|
| `APP_ENV` | yes | `production` (refuses `--no-backup` in the seed tool and `PPROF`; requires `ADMIN_API_TOKEN` of 16+ chars when set) |
| `PORT`, `DB_PATH` | yes | `8081`, `/var/lib/numerix/numerix.db` |
| `JWT_SECRET` | yes | `openssl rand -hex 32` |
| `ADMIN_API_TOKEN` | yes | 16+ random chars; the same value as on the Vercel admin project. Without it the admin API answers 503 `admin_disabled` |
| `ALLOWED_ORIGINS` | yes | the exact web origin(s), comma separated (CORS and cookies) |
| `ADMIN_ORIGIN` | yes | the admin origin |
| `WEBAUTHN_RP_ID`, `WEBAUTHN_ORIGINS`, `WEBAUTHN_RP_NAME` | yes | the real domain (no scheme) and the web origin; passkeys fail on a wrong value |
| `TRUST_PROXY` | yes | `true` behind Caddy so rate limits see the client IP |
| `FEATURE_WELCOME_GIFT` | no | `false` until the gift is wanted |
| `ASSET_BASE_URL` | no | empty: the web app serves `/assets/*` through its rewrite |
| `MEDIA_DIR`, `MEDIA_BASE_URL` | yes for Discover | folder with the 200 Discover files (default `./seed/feed-media`) and the URL prefix the web app rewrites (`/feed-media`); back the folder up with the release (docs/FEED_MEDIA.md) |
| `LLM_PROVIDER`, `LLM_API_KEY`, `LLM_MODEL`, `LLM_MONTHLY_CAP` | no | empty keeps canned stories; `ANTHROPIC_API_KEY` and `NARRATOR_MODEL` switch on the admin AI drafts |
| `MONGODB_URI`, `MONGODB_DB`, `MONGO_PSEUDO_SECRET` | no | see section 5 |

No secret is ever committed or logged. Check: `sudo systemctl status numerix-api`, then `curl https://<api-host>/v1/health`.

## 2. Content seed on the production database

1. On the VM, in the API folder: `bash scripts/db-prod-deploy.sh` (validate, dry run). Expect `seed files valid: 20 worlds, 89 quests, 1229 questions, 193 assets` and only world contrast warnings.
2. `bash scripts/db-prod-deploy.sh --apply` (takes a `VACUUM INTO` backup first).
3. Verify: `curl -s https://<api-host>/v1/worlds -H "Authorization: Bearer <token>"` returns 20 worlds, each with a `hero`; the admin app shows coverage per quest.
4. Rollback if needed: `bash scripts/db-prod-deploy.sh --rollback <run_id>`.

## 3. Pexels key upgrade path (pictures)

The bundled 193 assets come from Openverse and Wikimedia (public domain, CC BY with attribution) and are final for launch. To raise quality later: get a free key at pexels.com/api, then on a dev machine `PEXELS_API_KEY=... node tools/assets/fetch.mjs`, review the contact sheets, commit the new `seed/assets/*.json` and image files, and run step 2 again. Pexels photos need a visible link to Pexels and `creator_url` (the script sets it); the credits page lists them. The key is never needed at runtime and never goes in the repo.

## 4. Backups: Litestream and the restore drill

1. `/etc/litestream.yml` has the real namespace and region; the access key pair is in `/etc/systemd/system/litestream.service.d/override.conf`.
2. `sudo systemctl status litestream` is active and `litestream snapshots -config /etc/litestream.yml /var/lib/numerix/numerix.db` lists a recent snapshot.
3. Restore drill, once before launch: `litestream restore -config /etc/litestream.yml -o /tmp/restored.db /var/lib/numerix/numerix.db`, then `sqlite3 /tmp/restored.db "PRAGMA integrity_check; SELECT COUNT(*) FROM bank_questions;"` returns `ok` and 1229 published plus retired rows. Write the date of the drill in `PLAN.md`.

## 5. MongoDB Atlas (optional)

Off by default (SQLite serves everything). To enable: M0 cluster in a Canadian region, Network Access limited to the VM public IP (not 0.0.0.0/0), a user with `readWrite` on `numerix` only, then `MONGODB_URI`, `MONGODB_DB=numerix` and a `MONGO_PSEUDO_SECRET` that is never changed. No names, emails or free text reach Mongo; ids are pseudonymised. Check `GET /admin/v1/health`: `mongo_sync.enabled` true, `pending` 0, `last_error` empty. Atlas M0 has no backups: schedule `mongodump` if the data matters.

## 6. Vercel

| Project | Variable | Value |
|---|---|---|
| `numerix-web` | `API_URL` | `https://<api-host>/v1` (also the target of the `/assets/*` rewrite) |
| `numerix-admin` | `PLATFORM_API_URL` | `https://<api-host>` (no `/v1`) |
| `numerix-admin` | `ADMIN_API_TOKEN` | same as the API |
| `numerix-admin` | `ADMIN_PASSWORD`, `ADMIN_SESSION_SECRET` | long random values; the token stays on the admin server and never reaches a browser |

`pnpm vercel:env diff production` shows drift, `pnpm vercel:env push production` fixes it. `config/env/production.env` must not contain the `numerix-api.example.invalid` placeholder.

## 7. Must be true before merging the frontend to `main`

- [ ] The API is deployed with this release and `GET /v1/health` is green on the public host.
- [ ] Content seeded (section 2) and `locked_reason` present in journeys (this release).
- [ ] `API_URL` and `PLATFORM_API_URL` set, no placeholder host left.
- [ ] Litestream snapshot exists and the restore drill passed.
- [ ] Discover media: `MEDIA_DIR` copied and backed up, `feedseed --validate-only` passes, `GET /v1/feed-media/<file>` answers 200, 206 with Range, and 304 with If-None-Match.
- [ ] Frontend, from `Services/frontend`: `pnpm check`, `bash scripts/test.sh` suites `contract flows ui errors mobile play state sounds arch` all green, `pnpm build`, `(cd admin && npm test && npm run build)`.
- [ ] API, from `Services/numerix-api`: `gofmt -l .` empty, `go vet ./...`, `staticcheck ./...`, `go test ./... -count=1`, `govulncheck ./...` (the only finding allowed is the `golang.org/x/crypto` openpgp advisory, which has no fixed version and is not imported), `GOOS=linux GOARCH=arm64 go build ./cmd/api ./cmd/seed`.
- [ ] Load smoke on the VM (not a laptop with host stalls): `bin/loadtest -rps 50 -duration 20s`, zero 5xx, answer p95 under 400 ms.
- [ ] Secrets only in the VM env file and in Vercel; `git log -p` shows none; commits carry no AI attribution.
- [ ] After the merge: `bash scripts/test.sh live` against the production URL, then open the admin app and sign in.
