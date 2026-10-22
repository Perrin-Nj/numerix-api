# Numerix API: Go backend plan

Status: plan, awaiting two decisions (section 2). No code yet. Go is not installed on this machine (section 9).

Source of truth for behaviour: `../frontend/contracts/openapi.yaml` (spec first), `../frontend/contracts/BACKEND_REQUIREMENTS.md` (BE-1 to BE-63), `../frontend/docs/BACKEND_GO.md` (engine, bank, visual design, LLM roles). This plan adds storage, sync, hosting, mascots and the build order. The Python engine in `../numerix-` is the reference to port.


## 1. What you asked, and the answer

| Ask | Answer |
|---|---|
| Go, very expressive, every line explained, Spring background | Skill `numerix-go-expert` (global, in `~/.claude/skills`) is mandatory for all Go work. Each file gets a "Go for Spring people" note and a comment per line, with the Spring equivalent. |
| SQLite, backups, sync, only important data to hosted MongoDB | See section 2: the hosting fact changes the order. |
| Fast data access | Hot paths read one indexed row (question bank by `params_hash`, child state by id). In-process LRU cache in front of the store. Leaderboard reads come from a precomputed Mongo collection, not computed per request. |
| LLM clean, reusable components | LLM writes only story scene and ending, from typed payloads, results stored once per (question, world, language, prompt version, variant) and reused by all children. It never produces numbers, answers or pictures. |
| FE library for human-looking mascots and graphics | Lottie with downloaded packs (your choice), plus the existing `MethodVisual` for maths pictures. See section 6. |
| Backend reads the contracts | `oapi-codegen` generates server interface and models from `openapi.yaml`. CI fails on drift (BE-49). A contract test replays the WireMock mappings against the Go server. |

## 2. Decisions needed (my recommendation first)

**A. Primary store.** You asked if MongoDB can replace SQLite. Facts found: Atlas free tier is 512 MB, one free cluster per project, **no backups on the free tier**, and 500 connections ([Atlas free limits](https://mongodb.com/docs/atlas/reference/free-shared-limitations/)). SQLite is a file on disk, so it needs a host with a persistent disk. Vercel functions have no persistent disk, so SQLite cannot be the primary store there. Vercel does run Go (`net/http` handlers, [docs](https://vercel.com/docs/functions/runtimes/go)).

Recommendation: **MongoDB Atlas M0 is the primary store now** (free, works on Vercel, nothing to host). Code the store behind small interfaces (`QuestionStore`, `ChildStore`, `HistoryStore`, `LeaderboardStore`) so a **SQLite adapter** can be added later, with Litestream backup, the day you rent a VM with a disk. SQLite is also used now for local dev and tests (fast, no network). Skip the Mongo sync layer until SQLite becomes primary, because with Mongo as primary there is nothing to sync.

**Update, you asked to use the embedded DB to the maximum, free.** SQLite is the embedded DB and the code is free (`modernc.org/sqlite`, pure Go, no cgo). What is not free is a persistent disk to run it on: Fly.io has no free tier for new accounts (trial only, volumes $0.15/GB/month, [source](https://temps.sh/blog/fly-io-pricing-2026)); Railway free may allow a small volume but the limit is unverified; Oracle Cloud Always Free, Koyeb and Render were not checked. So the plan becomes: **SQLite is the primary store everywhere it has a disk (your PC, tests, any VM) and the only store the engine talks to**; Mongo Atlas M0 stays free for leaderboard and anonymised stats; the host is the one open question (free VM to verify, or Vercel with Mongo as primary until then). Turso (libSQL, hosted SQLite, free 9 GB per the same search) is a fourth route: SQLite semantics with no disk to manage; its Go embedded replicas need cgo, so it is not pure Go.

**Hosting check (done, sources dated 2026, third-party where noted).**

| Option | Cost | Disk | CLI | Verdict |
|---|---|---|---|---|
| Oracle Always Free ARM VM | $0 | yes, real VM | `oci` | Allowance reported cut to 2 OCPU / 12 GB, idle VMs reclaimed (<20% CPU, network, memory over 7 days) ([Oracle docs](https://docs.oracle.com/en-us/iaas/Content/FreeTier/freetier_topic-Always_Free_Resources.htm), [report](https://news.ycombinator.com/item?id=49183750)). Needs a card, sign-up and capacity are fiddly, you run the OS. Cheapest, most work. |
| Railway Hobby | $5/month, includes $5 usage credit; volume up to 5 GB, $0.15/GB/month ([pricing summary](https://temps.sh/blog/railway-pricing-2026)) | yes, volume | `railway` | Real embedded SQLite, no server to patch, deploys from GitHub. Free plan only 0.5 GB volume and sources conflict on whether a permanent free tier exists. Best balance. |
| Turso Free | $0: 5 GB, 500M rows read, 10M rows written per month ([official](https://turso.tech/pricing.md)) | none to manage | `turso` | Hosted, so every query is a network call: not embedded and slower. Go client is remote-only, the upstream pure-Go repo is deprecated, a community fork exists ([Turso Go docs](https://docs.turso.tech/sdk/go/reference), [fork](https://pkg.go.dev/github.com/WillowWorks-io/libsql-client-go)); the cgo one blocks static builds. Not recommended as primary. |
| Render free, Koyeb free | n/a | no persistent disk, or free tier closed ([report](https://snapdeploy.dev/state-of-free-hosting)) | yes | Rejected. |
| Northflank free | $0, 0.5 GB volume claimed | yes | yes | Sources conflict, card required. Backup option only. |
| Fly.io | volumes $0.15/GB/month, no free tier | yes | `fly` | Works, no free tier. |

Recommendation: **Railway Hobby (about $5 a month) with embedded SQLite on a volume, Litestream streaming to object storage, Mongo Atlas M0 for leaderboard and anonymised stats.** Fallback at $0: Oracle Always Free VM, same Go binary and Litestream, if you accept the ops work and reclaim risk. The Go code does not change between them, only the deploy target.

CLI and autonomy: `railway` and `oci` are both scriptable, but each needs one interactive login by you (browser). After that I can create the project, volume, env vars and deploys myself. I will not store tokens in the repo.

If you prefer SQLite primary anyway, the host must be Fly.io or a VPS, and Mongo then receives leaderboard and anonymised stats through an outbox.

**B. Backups on the free Mongo tier.** No built-in backups, so a scheduled GitHub Action runs `mongodump` weekly to an encrypted artifact or bucket. Needs your OK on where backups live. Child data is personal data (Law 25): choose the Atlas region carefully (Canada if offered on the free tier, not verified).

## 3. Architecture

```
Next.js app --HTTPS--> numerix-api (Go, net/http)
                         |- internal/http      generated handlers + adapters
                         |- internal/app       use cases (StartSession, NextBeat, Answer, Summary)
                         |- internal/engine    generators, grading, errors, leak check, BKT   (pure, no I/O)
                         |- internal/visual    MethodVisual builders                           (pure)
                         |- internal/store     interfaces + mongo/ and sqlite/ adapters
                         |- internal/narrator  LLM client, payloads, cache, canned fallback
                         |- internal/sync      outbox + leaderboard publisher (only if SQLite primary)
                         '- cmd/api            main = composition root (Spring: the @SpringBootApplication)
```

Data model and selection rules are in `../frontend/docs/BACKEND_GO.md` section 3, expressed for Mongo as collections: `questions` (unique index on `params_hash`), `question_visuals` (unique `cache_key`), `narrations`, `child_questions` (unique `child_id + question_id`), `leaderboard` (index `board_id, score desc`). "Never served to this child" uses an anti-join: pick candidates with `$lookup` or a per-child served-id set cached in memory for the session.

Fast access: indexes above, in-memory LRU for hot questions and visuals, `served_count` updated with `$inc`, leaderboard top-N kept as a small document refreshed on write.

## 4. LLM roles (answer to "what will the LLM do")

1. Scene text, before the answer, from payload `pour_le_llm_mise_en_scene` (no answer inside).
2. Ending text, after the answer, from `pour_le_llm_denouement` (verdict given).
3. Optional later: pick between pre-made variants. Nothing else. Output is leak-checked by Go, stored, reused. Provider failure or budget cap: canned bank per world and language (BE-54). Prompts are versioned files in `internal/narrator/prompts/`.

## 5. Reusable component rule

One generic pipeline `Generate(kind, key) -> cached artefact` used by questions, visuals and narrations: look up by deterministic key, build on miss, validate, store, return. Written once in `internal/bank`, three kinds plug in. Avoids three copies of the same cache code.

## 6. Mascots and in-play graphics

Choice: Lottie packs. Findings: Lottie is a playback format with a big marketplace and tiny runtime; Rive reacts better to app state ([comparison](https://www.pkgpulse.com/guides/lottie-vs-rive-vs-css-animations-web-animation-formats-2026)). To keep Lottie reactive, the mascot is a set of short clips (idle, happy, sad, thinking, cheer) switched by game events, with a static fallback for reduced motion and a text alternative.

Licensing: LottieFiles free animations fall under the Lottie Simple License (commercial use allowed, no standalone redistribution, creator terms can differ) ([help](https://help.lottiefiles.com/animation-licensing-basics-)). I cannot log in or accept licences for you, and downloads need your explicit OK per file. Process: you or I list candidate packs, you confirm, files go in `public/mascot/` with a `LICENSES.md` recording URL, creator and licence. Prefer one creator for a consistent style so it looks designed, not assembled.

Frontend work: a `Mascot` component in `src/game/` (Storybook first), `lottie-react` or `@lottiefiles/dotlottie-react` (`.lottie` files are smaller), mood driven by `story-machine` state, lazy loaded. Maths pictures stay `MethodVisual` (exact numbers from the engine).

## 7. Phases and todos

1. Tooling: install Go, create repo, `go mod init`, CI (fmt, vet, staticcheck, govulncheck, test -race). Exit: hello endpoint and `GET /health` pass in CI.
2. Contract: `oapi-codegen` config, generate, stub every operation returning 501, contract inventory test (all 312 operations present).
3. Engine port: generators, grading, error taxonomy, leak check, BKT. Port Python tests as table tests first.
4. Visual builder with golden tests against `../frontend/mock/lib/visual.mjs` output.
5. Store interfaces plus SQLite adapter (tests, local) plus Mongo adapter; bank pipeline; selection rules.
6. Sessions: start, next beat, answer (right and wrong give a different shape next), summary.
7. Narrator: provider interface, prompt files, leak check, cache, canned fallback, cost cap.
8. Platform: auth, children, parents, consent, export, delete (Law 25), subscription gate with `unpaid_policy`.
9. Leaderboard and anonymised stats in Mongo, hashed ids, opt-in display names.
10. Deploy to Vercel (Go runtime), env vars, backups job, point a staging web build at it, run `bash scripts/test.sh` against it.
11. Frontend: Mascot with Lottie packs, then swap `NEXT_PUBLIC_STATE_SOURCE=api`.

Each phase ends with the exit gate: tests green, contract drift check green, a record in this repo's `PHASE_LOG.md`.

## 8. Token saving (applies to every phase)

`rtk` on shell, one read per file, subagents for wide search and review, skills loaded once, generated code (oapi-codegen, sqlc) instead of hand-written boilerplate, and docs linked instead of repeated.

## 8b. Findings from the Numerix docs and the Python engine (read by a subagent, cited file:line)

Root has no .md files; docs live in `../../docs/` (`NUMERIX_CONTEXT.md`, `Numerix-Developer-Handover.md`, `README.md`). Must honour:
- LLM never does math; two payloads only; `fuite_detectee()` always empty, 720 checks per run (`NUMERIX_CONTEXT.md:24-57`).
- Three outcomes: CORRECT, ERREUR_TYPIQUE, ERREUR_ALEATOIRE; 110 error codes; no red X. Answers are int, str or a tuple (division with remainder) (`:142`, `:187-197`).
- **Product rule: no timers and no leaderboard against others, own past only** (`NUMERIX_CONTEXT.md:348-356`). This conflicts with the Mongo leaderboard idea. Decision needed (section 11). Until then the Mongo store holds anonymised stats only, no ranking of children against each other.
- Engine: 75 skills, 8 worlds, only 6 generators implemented (not 8 skills' worth of coverage). BKT constants (`bkt.py`): mastery 0.9, half-lives 18/72/36 h. Planner priority: revision, consolidation, new, upkeep. Port these first; golden tests use own fixtures because Python RNG will not match Go bit for bit.
- Kid PIN: 4 digits, argon2id, 5 wrong in 15 min gives 429; child JWT 8 h, no refresh; p95 budgets answer 400 ms, beats 800 ms; 200 concurrent kids; Law 25 items BE-36/38/39/41/51 (`BACKEND_INTEGRATION.md:55,113-118,1015-1030,1166-1183`).
- Stale: handover says FastAPI plus Supabase. Go and SQLite supersede it. `FOR UPDATE SKIP LOCKED` in `BACKEND_GO.md` is Postgres only: replaced below.

## 8c. Sign-in, devices, concurrency, speed (new)

**Who signs in.** Parents: passkey (WebAuthn) with email fallback. Library `go-webauthn` (storage and framework agnostic; you store the credential and the session data of each Begin call, and sign count, AAGUID and backup flags) ([tutorial](https://mojoauth.com/blog/add-passkeys-to-go-in-30-minutes)). Kids: no passkey; they use a 4 digit PIN today.

**"Do not log in every time on a device you already used": yes, with cookies, done safely.**
- After a parent signs in on a device, the server issues a *device token*: random 256-bit, stored hashed in SQLite, sent as `HttpOnly; Secure; SameSite=Lax; Path=/` cookie, 180 days, rotated on each use, revocable from the parent space ("Appareils connus"). Short access token (15 min) is minted from it. Kids on that device pick their profile and play with no PIN until the parent revokes or the PIN is required for sensitive screens only.
- This **changes the contract**: today the child JWT has no refresh and 8 h TTL (`INTEGRATION.md:55,118`). New rows go in `BACKEND_REQUIREMENTS.md` (BE-64 device token, BE-65 passkey endpoints, BE-66 device list/revoke) in the same change as the code. The Next.js proxy already forwards cookies.
- Why cookies and not localStorage: `HttpOnly` stops script theft; passkeys stop phishing.

**50 per second.** I read it as 50 requests per second sustained with bursts to 200 (docs say 200 concurrent kids, so the target is consistent). SQLite in WAL mode serialises writers but a write transaction takes about 0.1 to 1 ms on NVMe, so 50 writes per second is far inside the limit; no modernc benchmark was found, so we measure ([sources](https://oneuptime.com/blog/post/2026-09-08-fix-sqlite-database-is-locked-concurrent-writes/markdown)).
- Two pools: one write connection (`SetMaxOpenConns(1)`, `BEGIN IMMEDIATE`, `busy_timeout`), a read pool sized to CPUs. Pragmas set per connection through the DSN.
- Question selection without `SKIP LOCKED`: one write transaction does "pick unserved question, insert `child_question`" atomically, so two requests can never serve the same pair; the unique key `(child_id, question_id)` is the safety net.
- Requests carry deadlines (`context`), a server-wide limiter returns 429 with `Retry-After` instead of queueing forever, in-memory LRU for hot questions and visuals, rate limits and PIN lockout in an in-memory sharded counter (single node) persisted on lockout only.
- Load test with `k6` or `vegeta` at 50 rps and 200 rps as a phase exit gate; p95 answer under 400 ms.

**Beautiful game experience (backend share).** Fast first beat (pre-warmed bank per skill), a different shape after a wrong answer, instant canned narration when the LLM is slow, warm-up job at boot. Visual polish is frontend: Lottie mascot states, `MethodVisual`, motion tokens, sound. Candidates to be listed for your approval.

## 9. Risks

- Go is not installed. Install with `winget install GoLang.Go` after your OK, then restart the shell.
- Vercel Go runtime was labelled beta in one doc version: check current status before relying on it; fallback is a container host.
- Free Mongo has no backups, 512 MB cap and shared performance: fine for a prototype, not for launch.
- Cold starts on serverless add latency to the first request; mitigate with the existing keep-alive.
- Lottie licence terms differ per creator; track each file.
- 312 operations is large; phases 2 and 8 are the long ones.
- The LLM provider and monthly budget are not chosen yet.
