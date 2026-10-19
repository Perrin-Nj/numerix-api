# Content programme: task list

Source of truth for scope: docs/CONTENT_PLAN.md (section numbers below refer to it). Shell commands start with `rtk`. No AI attribution in files or commits. No em dashes in kid-facing text. No timers.

## P1 research and design (done)
- [x] Read current engine, catalog, picker, contract, admin and Mongo docs
- [x] Verify licences (Pexels, Unsplash, Pixabay, Openverse, Wikimedia, Fluent Emoji, OpenMoji, Noto, LottieFiles, Rive)
- [x] Write docs/CONTENT_PLAN.md and this list

## P2 backend (owner: one backend agent)
Gate A (do first, unlocks P3 and P4):
- [ ] Extract JSON Schema from plan 3.3 to `seed/schema/question.schema.json`; add `denylist.txt`, `names.txt`
- [ ] `internal/content`: types, canonical JSON + sha256, `calc` evaluator with `big.Rat` (3.4)
- [ ] `internal/content`: validator with every hard rule and soft warning of 3.5, one pass and one fail fixture per rule
- [ ] Reuse the narrator leak checker for answer leaks in digits and number words, fr and en
- [ ] `seed/worlds.json` (20 worlds, fr+en names/taglines/hooks, accent colours, grades) and `seed/quests.json` (89 quests of 2.4)
- [ ] Two example questions per type in `seed/questions/_examples.json`
- [ ] `go test ./internal/content -run TestSeed` walks `seed/questions/*.json`
Gate B (API ready for the UIs):
- [ ] Migration `0008_content.sql` (worlds, quests, bank_questions, question_tags, question_media, question_versions + triggers, misconceptions, seed_runs, `questions.bank_id/bank_version/origin`)
- [ ] Migration `0009_assets.sql` (assets, asset_tags, asset_vocab, asset_variants)
- [ ] Load the curriculum misconception codes into `misconceptions` (count them in a test, do not hard-code 99 or 110)
- [ ] `pickQuestion`: curated first, rank by target difficulty then deterministic hash, avoid previous type, generator fallback, re-serve oldest (1.3)
- [ ] Project curated rows into `questions` with `params_hash = bank:<id>:v<n>`
- [ ] `implemented` = generator exists OR at least 8 published bank rows; update `StartSession`, `Choose`, `ListSkills`
- [ ] Answer evaluator for numeric, choice, truefalse, order, match, range; misconception code gives `erreur_typique`
- [ ] Beat view builds `question` with new fields and `media[]`; scene `ready` from curated `story`; never leaks answer, explain, calc, common_errors (BE-67 test)
- [ ] `AnswerResult` returns localised `explain` and misconception code (BE-69)
- [ ] Hint endpoint or field (BE-70)
- [ ] Worlds and journey read from tables (names, quests, hero); keep the 8 old ids and strings
- [ ] `internal/assetstore` + media resolver (criteria score, rendezvous hash, fallback chain, 4.4)
- [ ] HTTP: `GET /v1/assets/{id}.webp`, `/{id}-{w}.webp`, `GET /v1/credits`, ETag, immutable, 304
- [ ] Admin API endpoints of section 8 (questions CRUD, transition, versions, validate, validate-all, export/import, worlds/quests, assets, coverage) with audit in same transaction
- [ ] AI draft endpoint: Anthropic structured output, 503 without key, always draft, validator + calc run, no self-publish
- [ ] Mongo kinds `question_bank`, `question_audit`, `question_stats` (+ `content_worlds`, `content_quests`), k-anonymity 5 for stats, resolvers, unique indexes
- [ ] Update docs/ADMIN_API.md and docs/MONGO_SYNC.md
- [ ] Contract: update `openapi.yaml` (CR-11..CR-18), `openapi.admin.yaml`, `BACKEND_REQUIREMENTS.md` rows (BE-67..BE-72); regenerate Go (`scripts/gen.sh`)
Deploy:
- [ ] `cmd/seed`: validate, migrate, backup (`VACUUM INTO` + Litestream check), diff by hash, dry-run, apply per kind, post checks, `seed_runs`
- [ ] `cmd/seed --rollback <run_id>` using `question_versions`
- [ ] `scripts/prod/deploy-content.sh` and `.ps1` (dry-run by default, `--apply`)
- [ ] `docs/CONTENT_DEPLOY.md` runbook (backup, apply, rollback, `litestream restore`)
Acceptance: plan section 10, P2 checks 1 to 9.

## P3 asset acquisition (owner: one agent, parallel after Gate A)
- [ ] User supplies `PEXELS_API_KEY` (free); optional `PIXABAY_API_KEY`
- [ ] `seed/assets/manifest.json`: per world 1 hero + 5 scenes queries, orientation, topics, objects, mood; Fluent sprite list per world
- [ ] `seed/assets/vocab.json`: facets world, topic (about 120), object (about 150), mood, setting, with fr/en labels
- [ ] `scripts/assets/fetch.mjs`: Pexels search, Openverse fallback (cc0, pdm, by), Commons metadata with bot User-Agent, rate limit and backoff, people and brand filters
- [ ] sharp pipeline: hero 480/960/1440, scenes 480/960, sprites 256, WebP q80, LQIP 24 px, dominant colour, content hash id
- [ ] Fluent Emoji 3D sprite download from `microsoft/fluentui-emoji` (MIT)
- [ ] `seed/assets/assets.json` with licence, creator, attribution_text, alt fr+en, tags, review status
- [ ] Contact sheet HTML; human review and approve; list of rejected items
- [ ] Update `frontend/public/LICENSES.md` sources table
- [ ] Idempotent re-run proof; run log with and without Pexels key
- [ ] Size check 5 to 20 MB; coverage table per world and quest topic
Acceptance: plan section 10, P3 checks 1 to 6.

## P4 question authoring (four agents in parallel after Gate A)
Common: read plan sections 2, 3, 5; use only skill ids, quests and misconception codes from the seed files; write fr-CA first then en-CA; status `review`; run `rtk go test ./internal/content -run TestSeed -count=1` before commit.
- [ ] Agent A: nature, espace, cuisine, sport, construction (5 files)
- [ ] Agent B: medecine, science, echecs, ocean, dinosaures (5 files)
- [ ] Agent C: animaux, musique, transport, pirates, marche (5 files)
- [ ] Agent D: meteo, robots, arts, hiver, jeux (5 files)
For each world file:
- [ ] 48 to 60 questions, per-quest count within target minus 2 and plus 3
- [ ] All 8 types and type quotas (numeric 10, choice 10, word 8, truefalse 5, order 5, match 5, visual 5, estimate 4)
- [ ] Difficulty spread 1 to 5, story gives the number a purpose
- [ ] `calc` on every numeric answer; common errors with real misconception codes
- [ ] Hint guides without leaking; explain teaches the strategy
- [ ] Media criteria on at least 3 questions per quest
- [ ] Self-review of 10 questions per quest (tone, names, brands, length)
Acceptance: plan section 10, P4 checks 1 to 5.

## P5a frontend play pages (after Gate B)
- [ ] `Picture` component (srcset, sizes, width/height, LQIP, dominant colour, alt, credit)
- [ ] Question components: numeric keypad, choice cards, true/false, ordering (tap + drag + keyboard), matching, visual blocks (bars, pictograph, clock, number line, grid, fraction, dice), estimate
- [ ] Animation layer from plan section 6 (Motion, `MotionConfig reducedMotion="user"`, lazy confetti)
- [ ] Gentle wrong state (amber tint, soft wobble, no red cross), explain reveal after answering
- [ ] World pages: hero, parallax with reduced-motion fallback, shared-element transition with view transitions
- [ ] 20 world tiles with hero images from the API; fallback gradient when no asset
- [ ] Numa owl SVG rig and `<Numa pose>` with six poses; map outcomes to poses
- [ ] Replace OpenMoji reaction faces with Fluent sprites; delete rabbit Lottie and dotlottie wasm and dependency
- [ ] `next.config.ts` rewrite for `/assets/*`; confirm immutable headers pass through
- [ ] Credits page in the parent area using `/v1/credits`
- [ ] fr and en strings; keyboard and screen reader paths for each type
- [ ] Lint, build, component tests, Lighthouse and reduced-motion checks
Acceptance: plan section 10, P5a checks 1 to 6.

## P5b admin UI (after Gate B, parallel with P5a)
- [ ] Question list with filters, counts vs targets, bulk actions
- [ ] Question editor: bilingual fields, type-specific answer forms, misconception picker, live validation, live child preview, versions with diff and restore
- [ ] Media picker by criteria with licence badges
- [ ] Review queue and publish with author/reviewer rule
- [ ] AI draft button (disabled without key), "AI draft" badge
- [ ] Worlds and quests editor, coverage dashboard
- [ ] Import (dry run then apply) and export; bulk validate report
- [ ] Assets screen: facets, tags, alt, approve/reject, upload
- [ ] Admin strings fr/en, tests against mock admin API
Acceptance: plan section 10, P5b checks 1 to 5.

## P6 integration, review, test, merge
- [ ] Seed fresh DB (`cmd/seed --dry-run`, then `--apply`); sample 5 percent of each world and publish
- [ ] Play script: one chapter per world in fr and en, no repeats, curated text and media, generator unused while bank remains
- [ ] Mongo check with a local `mongod`: three content collections filled, re-seed writes nothing
- [ ] Prod rehearsal on a DB copy: backup, apply, rollback, `litestream restore`
- [ ] Licence review: `/v1/credits` vs `assets.json` vs `LICENSES.md`
- [ ] Security review: admin endpoints auth, AI draft limits, asset endpoints rate limit, no answer leaks in any payload
- [ ] Accessibility and reduced-motion pass on play pages
- [ ] Update docs (PLAN.md, SCHEMA.md, README) and check CONTENT_PLAN decisions still hold
- [ ] Merge; confirm no AI attribution lines in history

## Out of scope, needs separate decision
- [ ] Skill graph for cycle 3 (years 5 and 6): new skill ids needed before grade 5 and 6 content
- [ ] Generators for the 67 skills without one (bank covers them for now)
- [ ] AVIF variants if storage budget allows
- [ ] Runtime AI question generation (explicitly future)
