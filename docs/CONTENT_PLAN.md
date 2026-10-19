# Numerix content programme: plan (Phase 1)

Status: design only, no production code. Date: 2026-10-18. Written for the agents of phases P2 to P6.
Rules for every phase: shell commands start with `rtk`; no AI attribution in files or commits; no em dashes in kid-facing text; no timers anywhere in the game; the LLM never does math and never writes a number (docs/NUMERIX_CONTEXT.md, section 2).

Product owner requirements this plan answers:
a question bank stored in the DB for every world and quest; no AI at runtime (AI only drafts in the admin, a human publishes); 20 worlds; more and more meaningful questions; reusable graphics chosen by criteria; real-world appealing imagery with clear free licences; better animations; question sync to MongoDB; local DB now, prod scripts later.

Source list (all fetched or searched on 2026-10-18) is in section 11. References look like [S3].

---

## 1. Current state

### 1.1 What exists (read from the code)

| Item | Today | Where |
|---|---|---|
| World ids | 8: `nature, espace, cuisine, sport, construction, medecine, science, echecs` | `internal/app/feed_engagement.go` (`feedWorlds`), contract `WorldId` enum (`../frontend/contracts/openapi.yaml` line 1886) |
| World names, hook, colour | fr and en, hard-coded in Go. `image_url` is always `""` | `feedWorlds`, `ListWorlds` in `internal/app/game_catalog.go` |
| Skills (the curriculum graph) | 75 = 51 arithmetic (`ARI-*`) + 24 other: GEO 8, MES 9, STA 4, PRO 3. Grades introduced: year 1 = 30, year 2 = 14, year 3 = 19, year 4 = 12. Nothing for years 5 and 6 | `internal/engine/data/pda-arithmetique.json`, `pda-autres-champs.json` (embedded) |
| Skills per world (a skill may sit in several worlds) | science 75, medecine 52, construction 44, echecs 43, nature 41, cuisine 39, espace 32, sport 29 | `univers` field of each skill |
| Typical-error codes | 99 distinct `erreurs_typiques[].code` found in the two JSON files (the brief says about 110: the Phase P2 test must count the real number and the plan uses the real list, not a number) | same files |
| Generators | 75 names in the curriculum, only 6 implemented: `gen_faits_addition`, `gen_calcul_mental_add_soustr`, `gen_terme_manquant_add_soustr`, `gen_faits_multiplication`, `gen_sens_multiplication_division`, `gen_mult_div_personnelle`. They serve 8 skills: `ARI-OPE-A02a, A02c, A03a, A05, A06a, A06c, A07a` and `ARI-SOP-A03a` | `internal/engine/registry.go`, `gen_addition.go`, `gen_multiplication.go` |
| "Chapter" | = one skill inside a world. The journey of a world is the list of its skills; friendly chapter titles exist for the 8 implemented skills only (`skillNames`), other skills show the curriculum label | `game_catalog.go` |
| "Quest" | does not exist as a data object | none |
| Chapter length | 3 beats (`chapterLength`) | `internal/app/game.go` |
| Question shape | one kind only: `Question{math_text, choices[4]}`, answer `AnswerValue` = int, number, string, or `[int,int]` (quotient, remainder) | contract schemas `Question`, `AnswerValue` |
| Images | no world art, no assets table. `public/reactions/*.svg` are OpenMoji (CC BY-SA 4.0), `public/mascot/rabbit-balloon.lottie` + a 1.2 MB dotlottie wasm | `../frontend/public`, `LICENSES.md` |
| Mongo | write-only outbox sync of 3 collections (`personal_bests`, `family_board`, `progress_stats`), no content | `docs/MONGO_SYNC.md`, migration `0007_outbox.sql` |
| Admin | `/admin/v1`: overview, parents, children, plans, config, notifications, audit. No content screens. `admin_audit` is append-only (SQL triggers) | `docs/ADMIN_API.md`, migration `0005_admin.sql` |

### 1.2 How a beat picks its problem today

1. `StartSession`: `engine.Choose(...)` (revision, consolidation, new, upkeep) picks a skill from the child's BKT profile, world and grade (`internal/engine/planner.go`). The skill is stored in the session JSON.
2. For each beat `pickQuestion(tx, childID, skill, avoidShape)` (`internal/app/game_session.go` line 437) runs in one write transaction:
   1. look in table `questions` for a row of this skill, another `shape` than the last beat, not in `child_questions` for this child (least served first, random);
   2. none: `Graph.GenerateFresh` calls the generator up to 60 times for an unseen statement, inserts it as a `questions` row (key `params_hash = sha256(skill|statement)`);
   3. generator exhausted: re-serve the child's oldest question of another shape;
   4. write `child_questions` (PK child + question) = the per-child no-repeat guarantee.
3. So the "bank" today is only a cache of generated problems. Skills without a generator (67 of 75) cannot be played at all (`StartSession` rejects them: "unknown or not available yet"). The story around the maths is made by the narrator (canned text or LLM), never the numbers.

### 1.3 What must change so a curated bank is primary

1. New authoritative tables (section 3): `worlds`, `quests`, `bank_questions` (+ versions). Hand-written seed JSON is loaded by `cmd/seed`.
2. `pickQuestion` step 1 reads published curated rows first, for `(world, skill)`, preferring the child's current quest; filters out served ones (per-child no-repeat kept through `child_questions`), avoids the previous beat's `type` (the old `shape`), and orders by distance to a target difficulty from the child's mastery, then by a deterministic hash (stable, no `RANDOM()`).
3. Curated rows are projected into the existing `questions` table the first time they are served (`params_hash = "bank:<slug>:v<version>"`, new columns `bank_id`, `bank_version`, `origin`). This keeps `beats.question_id`, `child_questions` and all current code valid, and a later edit (version 2) never changes a beat already served.
4. The generator becomes step 2 only: used when the bank has no unserved row for this skill (child exhausted it) or the skill has no curated rows yet. Step 3 (re-serve oldest) stays.
5. Skills without a generator become playable as soon as they have published bank rows: `StartSession` and `Choose` use `implemented = hasGenerator || bankCount(skill) >= 8`.
6. `Beat.question` and `AnswerValue` grow (types, media, ordering, matching): contract change, section 4.6.
7. Answer evaluation for curated rows is deterministic from the answer spec (section 3), a wrong choice with a `misconception` code returns `erreur_typique` and that code, anything else `erreur_aleatoire`. The BKT update is unchanged.
8. The scene text: a curated row carries its own `story` (fr + en). If present the beat has `scene.status = ready` at once, no LLM call (the narrator stays as fallback for generated rows). This is what makes "no AI at runtime" true for curated content.

Grades 5 and 6: the embedded curriculum stops at year 4 (cycle 2 for arithmetic). The product serves ages 6 to 12 but the data has no year 5 or 6 skills. Decision: this programme authors grades 1 to 4 against real skill ids and stretches difficulty 4 and 5 for older children; cycle 3 skills (`ARI-*` for years 5 and 6) are a separate curriculum-JSON task, listed in CONTENT_TODO.md as "out of scope, needs skill graph first". Schema allows `grade` 1 to 6 already.

---

## 2. The 20 worlds

### 2.1 Curriculum basis

- Quebec: the PFEQ primary mathematics progression (Progression des apprentissages) groups content in fields: arithmetic, geometry, measurement, statistics, probability [S1] [S2]. The repo's `meta.url` points to the ministry PDF. Every quest below names a real skill id from the embedded graph, so the PDA reference is automatic (`ref_pda`).
- Ontario equivalent (for the English-Canada side): the 2020 curriculum has strands B Number, C Algebra, D Data (includes probability), E Spatial Sense (geometry and measurement), F Financial Literacy [S3]. Mapping used in the doc and shown to parents:

| PDA field (id prefix) | Ontario strand |
|---|---|
| Arithmétique `ARI-SEN` (numbers) | B Number |
| Arithmétique `ARI-SOP`, `ARI-OPE` (operations) | B Number |
| Arithmétique regularities `ARI-OPE-A13/A14` | C Algebra (patterns) |
| Géométrie `GEO`, Mesure `MES` | E Spatial Sense |
| Statistique `STA`, Probabilité `PRO` | D Data |
| Money and decimals in a shop (`ARI-SEN-C03/C04`, `ARI-OPE-C02a/C03a`) | F Financial Literacy (plus B Number) |

### 2.2 Rules for all worlds

- 4 quests per world (5 for the 8 existing worlds), 10 to 15 questions per quest, so 48 to 60 questions per world, about 1000 in total. A quest = one skill in one world. Skill ids are real; the same skill may appear in several worlds with different stories (the planner already treats `univers` that way).
- Question types (the `type` field, section 3): `numeric` (numeric entry), `choice` (multiple choice), `truefalse`, `order`, `match`, `word` (word problem), `visual` (graph, chart, clock, number line, picture reading), `estimate`. Every world must contain all 8 and every quest at least 4 different ones. Minimum per world of 52: numeric 10, choice 10, word 8, truefalse 5, order 5, match 5, visual 5, estimate 4.
- Difficulty 1 to 5 inside a quest; spread 2/3/3/2/2 per 12.
- Existing world ids stay as they are. Their `name`/`tagline`/`hook`/colour strings stay and are copied into `worlds.json`.
- Counts in the tables: `n` = recommended questions for the quest.
- Games feel (decision already in memory: Duolingo outside, story inside): each question has a short story line in the world's setting, the maths decides what the hero does.

### 2.3 World table

Ids, names (fr-CA / en-CA), grades, main strands.

| # | id | fr-CA | en-CA | Theme (kid-appealing, real life) | Grades | PDA field / Ontario strand |
|---|---|---|---|---|---|---|
| 1 | `nature` (exists) | Nature | Nature | Forest trail, camp, wildlife spotting | 1-4 | Arithmetic, Statistics / B, C, D |
| 2 | `espace` (exists) | Espace | Space | Rocket mission, planets, moon base | 2-4 | Arithmetic, Measure / B, E |
| 3 | `cuisine` (exists) | Cuisine | Cooking | Family bakery, recipes, market snacks | 1-4 | Arithmetic, fractions, Measure / B, E |
| 4 | `sport` (exists) | Sport | Sport | Soccer, swim meet, basketball scores | 1-4 | Statistics, Arithmetic / B, D |
| 5 | `construction` (exists) | Construction | Building | Treehouse, bridge, tiny house | 2-4 | Geometry, Measure / E |
| 6 | `medecine` (exists) | Médecine | Medicine | Animal clinic, first aid, school nurse | 2-4 | Measure, Arithmetic / B, E |
| 7 | `science` (exists) | Science | Science | Lab, experiments, inventions | 2-4 | Statistics, Probability, Measure / D, E |
| 8 | `echecs` (exists) | Échecs | Chess | Tournament, board squares, openings | 1-4 | Geometry (positions), patterns / B, C, E |
| 9 | `ocean` | Océan | Ocean | Coral reef, submarine dive, whales of the St. Lawrence | 1-4 | Arithmetic, Measure / B, E |
| 10 | `dinosaures` | Dinosaures | Dinosaurs | Museum dig, fossil lab, giant bones | 1-4 | Numbers, Measure / B, E |
| 11 | `animaux` | Animaux | Animals | Farm and zoo, feeding time, baby animals | 1-3 | Arithmetic, groups / B |
| 12 | `musique` | Musique | Music | Band practice, beats and rhythm, concert | 2-4 | Fractions, patterns / B, C |
| 13 | `transport` | Transports | Vehicles | Trains, planes, bikes, school bus | 1-4 | Time, Arithmetic / B, E |
| 14 | `pirates` | Pirates | Pirates | Treasure map, ship, island grid | 1-4 | Geometry (locating), Arithmetic / B, E |
| 15 | `marche` | Marché | Market | Shop, coins, change, saving | 1-4 | Decimals, Arithmetic / B, F |
| 16 | `meteo` | Météo | Weather | Forecast, rain gauge, temperature chart | 2-4 | Statistics, Probability / D |
| 17 | `robots` | Robots | Robots | Programming a robot on a grid | 1-4 | Patterns, Geometry / C, E |
| 18 | `arts` | Arts | Art studio | Painting, symmetry, friezes, mosaics | 1-4 | Geometry / E |
| 19 | `hiver` | Hiver | Winter | Sugar shack (cabane à sucre), skating rink, snow fort | 1-4 | Arithmetic, Measure, Time / B, E |
| 20 | `jeux` | Jeux | Game night | Board games, dice, arcade tokens | 1-4 | Probability, Arithmetic / B, D |

Taglines and hooks for the 12 new worlds are authored by P2 in `seed/worlds.json` (fr and en, one short line each, no em dashes), accent colours chosen to keep contrast 4.5:1 with white text.

### 2.4 Quests

Format: quest id = `<world>.<slug>`; skill id (real); grade; learning goal (short); n.
Existing worlds (5 quests each). Skills marked `G` have a generator today and stay available as fallback.

**nature**
| quest | skill | gr | goal | n |
|---|---|---|---|---|
| nature.feuilles | ARI-SEN-A02c | 1 | Count a collection by grouping (leaves, pine cones) | 12 |
| nature.sentier | ARI-OPE-A13b | 1-2 | Continue a number pattern along the trail | 12 |
| nature.campement | ARI-OPE-A02a (G) | 1-2 | Add facts to 20 with camp supplies | 12 |
| nature.observer | STA-03a | 1-2 | Read a pictograph of birds seen | 10 |
| nature.rivieres | MES-A05a | 3 | Relate m, dm, cm for stream lengths | 12 |

**espace**
| quest | skill | gr | goal | n |
|---|---|---|---|---|
| espace.decompte | ARI-SEN-A08 | 2 | Order numbers on the launch sheet | 12 |
| espace.fusee | ARI-OPE-A03a (G) | 2-3 | Add and subtract in a mental way (fuel, crew) | 12 |
| espace.orbites | ARI-OPE-A06a (G) | 3 | Multiplication facts (rows of solar panels) | 15 |
| espace.horloge | MES-G01 | 2 | Read the time on mission control clock | 10 |
| espace.distances | ARI-OPE-A01a | 3-4 | Estimate sums and differences of big numbers | 12 |

**cuisine**
| quest | skill | gr | goal | n |
|---|---|---|---|---|
| cuisine.fournee | ARI-OPE-A02c (G) | 2 | All addition facts with trays of cookies | 15 |
| cuisine.recette | ARI-SEN-B02 | 3 | Fractions of a whole (half, quarter of a pizza) | 12 |
| cuisine.mesures | MES-E03 | 3 | Capacity: mL and L in a recipe | 12 |
| cuisine.partage | ARI-SOP-A03a (G) | 3 | Share and group (division and multiplication situations) | 12 |
| cuisine.tablee | ARI-OPE-A07a (G) | 4 | Division with remainder, seating at the table | 12 |

**sport**
| quest | skill | gr | goal | n |
|---|---|---|---|---|
| sport.points | ARI-OPE-A04a | 1-2 | Add scores of two teams | 12 |
| sport.classement | STA-04a | 2 | Build a bar chart of match results | 10 |
| sport.relais | ARI-OPE-A06c (G) | 4 | Fluent multiplication facts (relay laps) | 15 |
| sport.chrono | MES-G02 | 2-3 | Hours and minutes (training length, no timer in play) | 10 |
| sport.tournoi | PRO-05a | 1-2 | Certain, possible, impossible in a draw | 10 |

**construction**
| quest | skill | gr | goal | n |
|---|---|---|---|---|
| construction.formes | GEO-C02 | 1-2 | Name plane figures on blueprints | 12 |
| construction.solides | GEO-B03 | 1-2 | Name solids (cube, cone, cylinder) in bricks | 12 |
| construction.perimetre | MES-A06 | 3 | Perimeter of the treehouse floor | 12 |
| construction.surface | MES-B01b | 4 | Area of a wall (counting unit squares) | 12 |
| construction.regles | MES-A04a | 2 | Measure with ruler cm/dm | 10 |

**medecine**
| quest | skill | gr | goal | n |
|---|---|---|---|---|
| medecine.doses | ARI-OPE-A05 (G) | 2 | Missing term: dose needed to reach the total | 12 |
| medecine.poids | MES-F03 | 3 | Mass: g and kg of pets | 12 |
| medecine.horaire | MES-G01 | 1-2 | Reading times of the check-ups | 10 |
| medecine.dossier | STA-03a | 1-2 | Read a chart of visits | 10 |
| medecine.pansements | ARI-OPE-A03b | 3 | Products (boxes of bandages) | 12 |

**science**
| quest | skill | gr | goal | n |
|---|---|---|---|---|
| science.mesurer | MES-A03 | 1-2 | Estimate and measure lengths in the lab | 12 |
| science.hasard | PRO-05b | 3 | Which event is more likely | 12 |
| science.resultats | STA-03b | 4 | Interpret data tables and charts | 12 |
| science.suites | ARI-OPE-A14 | 1-2 | Extend a sequence of measurements | 10 |
| science.melanges | ARI-SEN-C03 | 3 | Read decimals (1.5 L) on beakers | 12 |

**echecs**
| quest | skill | gr | goal | n |
|---|---|---|---|---|
| echecs.cases | GEO-A01 | 1-2 | Locate squares and objects on a grid | 12 |
| echecs.parcours | GEO-A02 | 1-2 | Describe and follow a path | 12 |
| echecs.valeurs | ARI-OPE-A04a | 1-2 | Add piece values (pawn 1, knight 3, ...) | 12 |
| echecs.motifs | ARI-OPE-A13a | 1 | Non-numeric patterns (colours of squares) | 10 |
| echecs.partie | ARI-OPE-A06a (G) | 3 | Facts of multiplication (rows of 8) | 12 |

New worlds (4 quests each).

**ocean** : ocean.recif ARI-SEN-A02c (1, count fish) 12; ocean.profondeur ARI-SEN-A10 (1-2, number line of depth) 12; ocean.baleines ARI-OPE-A04b (3, add two 3-digit numbers) 12; ocean.sous-marin MES-A05a (3, metres and centimetres) 10.

**dinosaures** : dinosaures.fossiles ARI-SEN-A07 (1, compare numbers) 12; dinosaures.os ARI-SEN-A05 (1, compose/decompose) 12; dinosaures.geants MES-A04a (2, measure and compare lengths) 12; dinosaures.oeufs ARI-OPE-A03b (3, groups of equal eggs) 12.

**animaux** : animaux.ferme ARI-SOP-A02a (1, situation of addition) 12; animaux.pattes ARI-SEN-A01c (1, skip counting 2, 5, 10) 12; animaux.zoo ARI-SOP-A03a (G, 3) 12; animaux.pairs ARI-SEN-A11a (2, even and odd) 10.

**musique** : musique.rythme ARI-OPE-A13b (1-2, number patterns) 12; musique.mesures ARI-SEN-B01 (2, halves and quarters of a bar) 12; musique.concert ARI-OPE-A06a (G, 3) 12; musique.instruments STA-02 (1, collect and sort data) 10.

**transport** : transport.gare MES-G01 (1, read time) 12; transport.autobus ARI-OPE-A04c (3, subtraction of 3-digit numbers) 12; transport.pistes MES-A06 (3, perimeter of a track) 10; transport.voyage ARI-OPE-A03a (G, 2) 12.

**pirates** : pirates.carte GEO-A01 (1, locate on a map) 12; pirates.tresor ARI-SEN-A03 (1, read and write numbers) 12; pirates.butin ARI-SOP-A05a (1, equivalences) 12; pirates.navire ARI-OPE-A08 (4, missing factor) 12.

**marche** : marche.pieces ARI-SEN-A05 (1, decompose amounts) 12; marche.prix ARI-SEN-C03 (3, read prices with decimals) 12; marche.monnaie ARI-OPE-C02a (4, add/subtract decimals) 12; marche.panier ARI-OPE-A01a (2, estimate the basket) 12; marche.economies ARI-SEN-C08 (4, compare decimals) 10 (5 quests: this is the financial-literacy world).

**meteo** : meteo.pluie STA-04a (2, bar chart of rain) 12; meteo.temperature STA-03b (4, line graph) 12; meteo.prevision PRO-05b (3, more likely) 12; meteo.saisons ARI-SEN-A08 (2, order numbers) 10.

**robots** : robots.programme ARI-OPE-A14 (1, continue a sequence) 12; robots.grille GEO-A02 (1, moves on a grid) 12; robots.miroir GEO-D01 (2, congruent figures) 10; robots.boucles ARI-OPE-A13b (1, repeat patterns) 12.

**arts** : arts.formes GEO-C02 (1, plane figures) 12; arts.frises GEO-D03a (2, friezes) 12; arts.mosaique ARI-SEN-B03 (3, fraction of a mosaic) 12; arts.symetrie GEO-C05 (4, identify and build quadrilaterals) 10.

**hiver** : hiver.erabliere ARI-OPE-A06a (G, 3, buckets of sap) 12; hiver.patinoire MES-A06 (3, perimeter of rink) 12; hiver.neige STA-03a (1, read a chart) 12; hiver.horaire MES-G02 (2, time relations) 10.

**jeux** : jeux.des PRO-08a (4, count outcomes of two dice) 12; jeux.cartes ARI-SEN-A08 (1, order numbers) 12; jeux.points ARI-OPE-A02c (G, 2) 12; jeux.chance PRO-05a (1, certain/possible/impossible) 10.

Totals: 8 worlds x 5 quests (40) + 11 worlds x 4 (44) + marche 5 = 89 quests, about 1050 questions (average 11.8).

### 2.5 Type quota check per world

P4 authors one JSON per world; the validator (3.5) refuses a `published` world file that misses a type or a quota. Every quest also needs a visual or media for at least 3 questions so the screen is not text only.

---

## 3. Question data model and seed schema

### 3.1 Tables (migration `0008_content.sql`, SQLite)

```sql
CREATE TABLE worlds (
  id TEXT PRIMARY KEY, ord INTEGER NOT NULL,
  name_fr TEXT NOT NULL, name_en TEXT NOT NULL,
  tagline_fr TEXT NOT NULL, tagline_en TEXT NOT NULL,
  hook_fr TEXT NOT NULL, hook_en TEXT NOT NULL,
  accent_color TEXT NOT NULL CHECK (accent_color GLOB '#[0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f]'),
  grade_min INTEGER NOT NULL, grade_max INTEGER NOT NULL,
  strand TEXT NOT NULL,                       -- PDA field(s), ex "arithmetique,statistique"
  hero_asset_id TEXT REFERENCES assets(id),   -- section 4
  status TEXT NOT NULL DEFAULT 'published' CHECK (status IN ('draft','published','retired')),
  content_hash TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE quests (
  id TEXT PRIMARY KEY,                        -- "cuisine.fournee"
  world_id TEXT NOT NULL REFERENCES worlds(id), ord INTEGER NOT NULL,
  skill_id TEXT NOT NULL,                     -- must exist in the curriculum graph
  name_fr TEXT NOT NULL, name_en TEXT NOT NULL,
  goal_fr TEXT NOT NULL, goal_en TEXT NOT NULL,
  grade_min INTEGER NOT NULL, grade_max INTEGER NOT NULL,
  target_count INTEGER NOT NULL,              -- the "n" of section 2
  icon_asset_id TEXT REFERENCES assets(id),
  status TEXT NOT NULL DEFAULT 'published', content_hash TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX idx_quests_world ON quests(world_id, ord);

CREATE TABLE bank_questions (
  id TEXT PRIMARY KEY,                        -- slug, ex "cuisine.fournee.007"
  world_id TEXT NOT NULL REFERENCES worlds(id),
  quest_id TEXT NOT NULL REFERENCES quests(id),
  skill_id TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('numeric','choice','truefalse','order','match','word','visual','estimate')),
  answer_kind TEXT NOT NULL CHECK (answer_kind IN ('numeric','choice','truefalse','order','match','range')),
  grade INTEGER NOT NULL CHECK (grade BETWEEN 1 AND 6),
  difficulty INTEGER NOT NULL CHECK (difficulty BETWEEN 1 AND 5),
  status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','review','published','retired')),
  version INTEGER NOT NULL DEFAULT 1,
  content_json TEXT NOT NULL,                 -- the whole seed object (3.3), canonical form
  content_hash TEXT NOT NULL,                 -- sha256 of canonical JSON
  author TEXT NOT NULL, source TEXT NOT NULL, -- source: seed:<file> | admin | ai-draft
  ai_drafted INTEGER NOT NULL DEFAULT 0,
  reviewed_by TEXT, reviewed_at TEXT, published_at TEXT, retired_at TEXT,
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
  CHECK (ai_drafted = 0 OR status = 'draft' OR reviewed_by IS NOT NULL)   -- a human must review AI drafts
);
CREATE INDEX idx_bank_pick ON bank_questions(skill_id, world_id, status, difficulty);
CREATE INDEX idx_bank_quest ON bank_questions(quest_id, status);

CREATE TABLE question_tags  (question_id TEXT NOT NULL REFERENCES bank_questions(id) ON DELETE CASCADE, tag TEXT NOT NULL, PRIMARY KEY (question_id, tag));
CREATE TABLE question_media (question_id TEXT NOT NULL REFERENCES bank_questions(id) ON DELETE CASCADE,
  role TEXT NOT NULL,                         -- scene | object | choice:<id> | visual
  ord INTEGER NOT NULL DEFAULT 0, asset_id TEXT REFERENCES assets(id), criteria_json TEXT,  -- explicit id OR criteria (4.4)
  PRIMARY KEY (question_id, role, ord));

CREATE TABLE question_versions (               -- append-only snapshot of every saved version
  question_id TEXT NOT NULL, version INTEGER NOT NULL,
  status TEXT NOT NULL, content_json TEXT NOT NULL, content_hash TEXT NOT NULL,
  actor TEXT NOT NULL, action TEXT NOT NULL,   -- create | edit | submit | publish | retire | seed | rollback
  at TEXT NOT NULL, PRIMARY KEY (question_id, version));
-- same two triggers as admin_audit: RAISE(ABORT) on UPDATE and DELETE

CREATE TABLE misconceptions (code TEXT PRIMARY KEY, skill_id TEXT, desc_fr TEXT, desc_en TEXT);  -- loaded from the 99 curriculum codes + new "mis_*" codes
CREATE TABLE seed_runs (id TEXT PRIMARY KEY, at TEXT NOT NULL, actor TEXT NOT NULL, dry_run INTEGER NOT NULL,
  git_rev TEXT, files_hash TEXT, added INTEGER, changed INTEGER, retired INTEGER, unchanged INTEGER, backup_path TEXT, status TEXT);

ALTER TABLE questions ADD COLUMN bank_id TEXT;       -- served-instance table gains the link
ALTER TABLE questions ADD COLUMN bank_version INTEGER;
ALTER TABLE questions ADD COLUMN origin TEXT NOT NULL DEFAULT 'generated';   -- generated | curated
```

Audit trail decision: reuse `admin_audit` (already append-only, same-transaction rule from `ADMIN_API.md`) with `target_type` in `question|world|quest|asset|seed_run` and `detail` = JSON diff summary. Only `question_versions` is new, because audit rows hold no snapshot. This avoids a second audit table; Mongo `question_audit` is derived from `admin_audit` + `question_versions` (3.6).

Why one `bank_questions` row holds the whole object as JSON plus a few indexed columns: the admin edits whole documents, the seed files are whole documents, Go validates the whole document, and the picker only filters on the indexed columns. This is the same pattern the repo uses for `sessions.json` and `questions.json`.

### 3.2 Types versus answer kinds

| `type` (what the child sees) | allowed `answer.kind` | Child input |
|---|---|---|
| `numeric` | `numeric` | number keypad (also remainder pair) |
| `choice` | `choice` | 2 to 5 cards |
| `truefalse` | `truefalse` | two big buttons |
| `order` | `order` | drag or tap cards into order |
| `match` | `match` | tap left then right, pairs |
| `word` | `numeric` or `choice` | same as numeric/choice, longer story |
| `visual` | `numeric` or `choice` | question reads a `visual` block (bar chart, pictograph, clock, number line, grid) |
| `estimate` | `range` or `choice` | pick the best estimate; `range` accepts a closed interval |

### 3.3 Seed JSON schema (JSON Schema 2020-12, to be saved by P2 as `seed/schema/question.schema.json`)

A seed file `seed/questions/<world>.json` is `{ "world": "cuisine", "schema": 1, "questions": [ Question, ... ] }`.

```jsonc
{
  "id": "cuisine.fournee.007",              // ^[a-z0-9]+(\.[a-z0-9-]+)*\.\d{3}$ ; world + quest prefix must match
  "quest": "cuisine.fournee",
  "skill": "ARI-OPE-A02c",
  "type": "word",
  "grade": 2,
  "difficulty": 3,
  "story":  { "fr": "Léa sort deux plaques de biscuits du four.", "en": "Lea takes two trays of cookies out of the oven." },
  "prompt": { "fr": "Il y a 8 biscuits sur la première plaque et 7 sur la deuxième. Combien de biscuits en tout ?",
              "en": "There are 8 cookies on the first tray and 7 on the second. How many cookies in all?" },
  "math_text": "8 + 7 = ?",                 // optional, shown under the prompt, language neutral
  "visual": null,                           // or { "kind": "bars|pictograph|clock|numberline|grid|fraction|dice", "data": {...} }
  "choices": null,                          // for choice/truefalse/estimate-choice (below)
  "items": null,                            // for order/match
  "answer": { "kind": "numeric", "value": 15, "unit": null, "calc": "8+7" },
  "common_errors": [                        // typical wrong answers for numeric entry
    { "value": 14, "misconception": "ARI_ADD_OFF_BY_ONE", "feedback": { "fr": "Presque ! Compte encore une fois les deux plaques.", "en": "Close! Count both trays once more." } },
    { "value": 1,  "misconception": "ARI_ADD_SUBTRACTS", "feedback": { "fr": "...", "en": "..." } }
  ],
  "hint":   { "fr": "Pars de 8 et avance de 7 pas.", "en": "Start at 8 and hop 7 steps." },
  "explain": { "fr": "8 + 7 = 8 + 2 + 5 = 10 + 5 = 15.", "en": "8 + 7 = 8 + 2 + 5 = 10 + 5 = 15." },   // shown only AFTER the answer
  "tags": ["cookies", "baking", "add-within-20", "make-ten"],
  "media": [
    { "role": "scene",  "criteria": { "world": "cuisine", "topic": "baking", "object": "cookie", "kind": "photo" } },
    { "role": "object", "asset": "fluent:cookie" }            // explicit asset (id or seed alias, resolved by the loader)
  ],
  "status": "draft",                       // draft | review | published (seed files may carry published)
  "author": "p4-agent-a",
  "version": 1,
  "source": "seed:cuisine.json"
}
```
(`ARI_ADD_OFF_BY_ONE` style codes above are illustrative: P4 must only use codes present in `misconceptions`; the exact list is loaded from the curriculum JSON.)

`choice` example fragment:
```jsonc
"type": "choice",
"choices": [ { "id": "a", "text": {"fr":"12","en":"12"}, "misconception": "..." },
             { "id": "b", "text": {"fr":"15","en":"15"} }, { "id": "c", ... }, { "id": "d", ... } ],
"answer": { "kind": "choice", "correct": "b", "calc": "8+7", "calc_choice": { "b": "8+7" } }
```
`truefalse`: `"answer": { "kind": "truefalse", "value": false, "misconception": "..." }` with `statement` in `prompt`.
`order`: `"items": [{ "id":"a","text":{...},"key":"3/4" }, ...]`, `"answer": { "kind":"order", "sequence":["c","a","b"], "by":"asc", "calc": "sort asc of keys" }`.
`match`: `"items": { "left":[{id,text}], "right":[{id,text}] }`, `"answer": { "kind":"match", "pairs":[["l1","r2"],["l2","r1"]] }`.
`estimate`: `"answer": { "kind":"range", "min": 180, "max": 220, "exact_for_check": 198 }` or a `choice` with the best estimate.
Remainder division: `"answer": { "kind":"numeric", "value":[3,2], "calc":"divmod(23,7)" }` (matches the contract tuple).

Language rule: both `fr` and `en` are objects, never single strings. Language tags `fr-CA` / `en-CA`; the key names are `fr` and `en`.

### 3.4 `calc` mini-language

`calc` is an optional expression for the Go test to recompute the numeric answer. Allowed: integers and decimals, `+ - * / ( )`, `divmod(a,b)`, `min`, `max`, `abs`, `round`, `gcd`, `pow(a,2)`, `sum(a,b,..)`. No identifiers, no loops. Evaluated with exact rational arithmetic (`math/big.Rat`) so 0.1 + 0.2 is exact. Required for every `numeric` and `range` answer with an arithmetic derivation; for `choice` use `calc_choice` for the correct id; optional elsewhere. This follows the project's golden rule: numbers are verified by deterministic code, never by a model.

### 3.5 Validation spec (Go test, package `internal/content`, runs on every seed file and in `cmd/seed --validate`)

Hard errors (block seed and publish):
1. Structure: valid against the JSON Schema; `id` unique across all files; id prefix = quest id = world file; `quest` exists in `quests.json` and its `skill` equals the question's `skill`; `skill` exists in the curriculum graph; `grade` within quest grade range and `grade >= skill.annee_intro - 0` (a grade 1 question may not use a year 3 skill).
2. Languages: every user-visible text object (`story`, `prompt`, `hint`, `explain`, each choice, item, feedback, alt text) has non-empty `fr` and `en`; `fr` and `en` are not identical unless the text is digits/symbols only.
3. Answer is in the options: `choice` correct id is in `choices`; `truefalse` has a bool; `order.sequence` is a permutation of `items` ids; `match.pairs` covers every left id once and each right id at most once; numeric answer in `common_errors` is never equal to `answer.value`.
4. Distractors: choice texts are pairwise distinct per language; no two choices with the same numeric value; 3 to 5 choices (`choice`), exactly 2 for `truefalse`; each distractor carries `misconception` OR is listed as `random` (allowed at most 1 per question); every misconception code exists in table `misconceptions`.
5. Numeric truth: if `calc` present, evaluated result equals `answer.value` exactly; for `calc_choice` equals the correct choice value; every `common_errors[].value` differs from the truth; for `range`, `min <= exact_for_check <= max` and `max - min` is at most 20 percent of the value.
6. No answer leak before answering: the answer text (number, remainder pair parts only if not an operand, fraction, choice text) does not appear in `story`, `prompt`, `hint`, `media` alt text or `visual.data` labels, in digits or in words (fr and en number words up to 1 000 000, reuse the narrator leak checker, BE-05). Operands that equal the answer are allowed only if flagged `"allow_operand_equals_answer": true` with a reason in `source`. `explain` is exempt but is never part of the Beat payload (test: serialise a Beat, assert no `explain`, `answer`, `calc`, `common_errors` keys).
7. No timers and no pressure: no key named `timer`, `time_limit`, `seconds`; regex ban (fr and en, case-insensitive, accents folded) on `vite`, `dépêche`, `compte à rebours`, `chrono` (except in a skill of type time-reading, where a clock visual is allowed), `hurry`, `quick!`, `countdown`, `seconds left`, `time is running out`.
8. Kid text rules: no em dash (U+2014) or en dash used as a pause (U+2013), no emoji inside text (art is media, not text), no profanity list hit, no brand names list hit (a small deny list in `seed/schema/denylist.txt`), no real children's names list is needed but first names must come from an allow list in `seed/schema/names.txt` (mixed Quebec names).
9. Length and reading level (limits from the age band; word problems are shorter than the texts classic readability formulas expect, so plain limits are used instead of a Flesch score [S12] [S13]):

| Field | Grade 1-2 | Grade 3-4 | Grade 5-6 |
|---|---|---|---|
| `story` | at most 25 words | at most 35 | at most 45 |
| `prompt` | at most 20 words, 2 sentences | 30 words, 3 sentences | 40 words, 3 sentences |
| any sentence | at most 14 words | 18 | 22 |
| `hint` | at most 14 words | 18 | 22 |
| `explain` | at most 30 words | 40 | 50 |
| choice / item text | at most 8 words | 10 | 12 |

   Also: no word longer than 14 letters; at most 1 subordinate number-word per sentence is not enforced (kept as review checklist).
10. Media: every `asset` id/alias resolves; every `criteria` object uses only vocabulary values from `asset_vocab`; at most 3 media per question.
11. Status rules: a seed file may carry `draft` or `published`; `ai_drafted=true` cannot be `published` without `reviewed_by`; `retired` is never in a seed file (retire = remove from seed, loader marks `retired`).

Soft warnings (printed, do not block): fewer than 4 distinct types in a quest, quest below target_count minus 2, difficulty spread skewed, two prompts with Levenshtein distance under 25 percent in a quest, tags below 2, `hint` identical to `explain`.

World file quotas (hard error only when `status=published` for the whole world): the 8 types present, quota table of section 2.2, each quest at least `target_count - 2` published.

### 3.6 Mongo collections and sync

Reuse the existing outbox and worker (`docs/MONGO_SYNC.md`): same-transaction `outbox` rows, 5 s worker, unordered bulk upsert, unique index on `key`, at-least-once and idempotent. Content is not personal data, so no pseudonymisation for the first two collections; the stats collection follows the k-anonymity rule already in the doc.

| Collection | `key` | Document | Kind in `outbox` |
|---|---|---|---|
| `question_bank` | question id | full current object (`content_json` parsed), `world`, `quest`, `skill`, `type`, `grade`, `difficulty`, `status`, `version`, `content_hash`, `author`, `source`, `ai_drafted`, `reviewed_by`, `updated_at`, `published_at` | `question_bank` |
| `question_audit` | `admin_audit.id` | `at`, `actor` (admin name or `seed:<run>`), `action`, `question_id`, `from_version`, `to_version`, `content_hash`, `detail` | `question_audit` |
| `question_stats` | question id | `world, quest, skill, attempts, correct_rate, typical_error_rate, top_misconceptions[{code,count}], median_response_ms`, `updated_at`. Written only when at least 5 distinct children contributed (below that the document is deleted), no child id | `question_stats` |

Also: `worlds` and `quests` documents go into `question_bank` collection with `doc_type`, or two tiny collections `content_worlds`, `content_quests` (decision: two tiny collections, so a query by type needs no filter).

How rows are enqueued: every transaction that inserts or changes a `bank_questions`, `worlds` or `quests` row (admin API, seed loader, import) inserts outbox rows for it in the same transaction; every `admin_audit` insert for a content target inserts a `question_audit` row; the answer path already writes a `progress_stats` outbox row, it additionally writes `question_stats` for `questions.bank_id` when not null. Resolver functions (`internal/mongosync`) read the current state from SQLite, same as today. Seed run of 1000 questions = 1000 outbox rows, collapsed by key, 10 bulk batches of 100.

Failure modes, erasure and rebuild: same as the existing doc. Mongo can always be rebuilt from SQLite (`mongosync --resync content`).

---

## 4. Media and asset system

### 4.1 What production systems do (and what we take)

- Tag with a controlled vocabulary, attach a term only when a searcher would expect that image for it: stock-library taxonomy practice (Corbis taxonomists) [S9]. We take: fixed facets (`world, topic, object, mood, setting, grade_band`) with a vocabulary table, no free tags, curated by humans.
- Rights and descriptive metadata in IPTC style fields (creator, credit, licence, keywords) [S10]. We take: `creator, licence, licence_url, attribution_text, source_url` as columns on every asset.
- Faceted narrow-and-widen retrieval [S9]: we take facet filters with weights and a relax-then-fallback chain (4.4).
- Content-addressed, immutable URLs: a file whose name contains its hash never changes, so it can be cached for a year with `Cache-Control: public, max-age=31536000, immutable`; HTML stays `no-cache` [S11].
- Modern formats: WebP and AVIF are supported by all modern browsers and beat JPEG for photos [S14]. Responsive sets with `srcset`. Decision: WebP only (sharp `quality 80`, effort 4 [S15]) to keep storage small; AVIF can be added as extra `asset_variants` rows later without a schema change.
- Placeholders: a tiny real image beats BlurHash in a browser (no JavaScript decode, no main-thread work) [S16]. Decision: store a 24 px wide WebP (about 150 to 400 bytes) as a `data:` URI in column `lqip` plus the `dominant_color`; no BlurHash.

### 4.2 Table design (migration `0009_assets.sql`)

```sql
CREATE TABLE assets (
  id TEXT PRIMARY KEY,                 -- "a_" + first 20 hex of sha256(normalised master bytes) : content addressed, dedupes
  kind TEXT NOT NULL CHECK (kind IN ('photo','illustration','sprite','lottie')),
  world_id TEXT REFERENCES worlds(id), -- primary world, NULL = shared
  grade_band TEXT NOT NULL DEFAULT 'all' CHECK (grade_band IN ('all','1-2','3-4','5-6')),
  mood TEXT,                           -- calm | joyful | adventurous | curious | cozy | epic
  dominant_color TEXT NOT NULL,        -- "#RRGGBB"
  lqip TEXT NOT NULL,                  -- data:image/webp;base64,... (24 px wide)
  width INTEGER NOT NULL, height INTEGER NOT NULL,   -- of the master
  alt_fr TEXT NOT NULL, alt_en TEXT NOT NULL,        -- describes the content, never contains the answer
  source_name TEXT NOT NULL,           -- pexels | openverse | wikimedia | fluent-emoji | own
  source_url TEXT NOT NULL, creator TEXT NOT NULL, creator_url TEXT,
  licence TEXT NOT NULL CHECK (licence IN ('CC0','PDM','CC-BY-4.0','CC-BY-3.0','CC-BY-2.0','MIT','PEXELS','OWNED')),
  licence_url TEXT NOT NULL,
  attribution_text TEXT NOT NULL,      -- ready to print, ex "Photo by Jane Doe on Pexels"
  attribution_required INTEGER NOT NULL DEFAULT 0,
  people_free INTEGER NOT NULL DEFAULT 1,             -- 1 = no identifiable person (kid safety gate)
  brand_free INTEGER NOT NULL DEFAULT 1,
  review_status TEXT NOT NULL DEFAULT 'pending' CHECK (review_status IN ('pending','approved','rejected')),
  reviewed_by TEXT, reviewed_at TEXT, created_at TEXT NOT NULL
);
CREATE TABLE asset_tags (asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  facet TEXT NOT NULL CHECK (facet IN ('world','topic','object','mood','setting')),
  value TEXT NOT NULL, PRIMARY KEY (asset_id, facet, value));
CREATE INDEX idx_asset_tags_lookup ON asset_tags(facet, value);
CREATE TABLE asset_vocab (facet TEXT NOT NULL, value TEXT NOT NULL, label_fr TEXT, label_en TEXT, parent TEXT, PRIMARY KEY (facet, value));
CREATE TABLE asset_variants (asset_id TEXT NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
  w INTEGER NOT NULL, format TEXT NOT NULL DEFAULT 'webp', bytes BLOB NOT NULL, size INTEGER NOT NULL,
  PRIMARY KEY (asset_id, w, format)) WITHOUT ROWID;
```
Rule: a public asset must have `review_status='approved'`, `people_free=1`, `brand_free=1`. The loader refuses to approve a photo whose `licence='PEXELS'` without `creator_url`.

### 4.3 Blob in SQLite versus file store: decision

Decision: **blobs in SQLite, in a separate table `asset_variants`** (WITHOUT ROWID so the big BLOB sits in overflow pages), not in a file directory.

Why:
- Size: 20 worlds x (1 hero x 3 widths + 4 scenes x 2 widths) + about 250 sprites at 256 px. Budget: hero 480/960/1440 about 20+70+140 KB = 230 KB x 20 = 4.6 MB; scenes 480/960 about 20+70 = 90 KB x 80 = 7.2 MB; sprites 8 KB x 250 = 2 MB. Total about 14 MB, inside the 5 to 20 MB range.
- SQLite measured about 35 percent faster than separate files for ~10 KB blobs, files padded to block size use 20 percent more disk; files win for large blobs but the cross-over is far above our sprite sizes and our hero images are served with immutable caching so each is read once per edge [S17].
- Litestream takes over checkpointing and ships WAL pages in LTX files, plus a full snapshot each 24 h by default; snapshot cost scales with DB size [S18]. A 14 MB growth is a one-time import (written once, in few transactions so WAL bursts are small), after which assets never change (content addressed), so replication cost stays near zero. One file also means one backup, one restore, atomic with the rows that reference it, and no extra volume mount on the Oracle VM.
- Deploy: the same `cmd/seed` loads assets and questions in one run; no file sync step to forget.
- Escape hatch: if assets ever exceed about 100 MB or we add video, the `asset_variants.bytes` read in one function (`assetstore.Read`) is swapped for an object store (S3 compatible) without changing URLs, since URLs contain the hash.

Rejected: files on disk (needs a second backup path and a volume), object store now (extra service for 14 MB).

### 4.4 Selection rules (deterministic, stable per child and question)

A question's media entry is either an explicit `asset` (always wins) or `criteria`:
`{ "world", "topic", "object", "mood", "kind", "setting" }` (all optional except `kind` and one of world/topic/object).

Algorithm `ResolveMedia(criteria, childID, questionID, gradeBand)`:
1. Candidates = approved, `people_free`, `brand_free` assets of `kind`, and `grade_band IN ('all', band)`.
2. Score each: `object` match 5, `topic` 4, `setting` 2, `world` 3 (primary or tag), `mood` 2, exact `grade_band` 1.
3. Keep the candidates whose score is at least `maxScore - 2`.
4. Pick with rendezvous hashing: highest `sha256(childID | questionID | assetID)`. Same child and question always get the same image, different children spread over the set, an asset added later only steals the questions where it wins. Admin preview uses `childID=""`.
5. Fallbacks, in order: relax `mood`; relax `setting`; relax `object`; world hero (`worlds.hero_asset_id`); world sprite set (Fluent emoji of the world); no image and the UI draws a CSS gradient from `accent_color` with the world icon (never a broken image).
6. Resolution happens at beat build time and returns `MediaRef` objects (4.6). Never at publish time, so adding assets improves old questions; the pick is deterministic so it does not flicker.

Facet vocabulary (`asset_vocab`) is seeded by P3 from the world table plus about 120 `topic` values (for example `baking, tools, planets, coral, ...`) and 150 `object` values; admin can add values but not free text.

### 4.5 HTTP API

| Method and path | Purpose | Auth | Notes |
|---|---|---|---|
| `GET /v1/assets/{id}.webp` | largest variant | none | `ETag: "<id>-<w>"`, `Cache-Control: public, max-age=31536000, immutable`, 304 on `If-None-Match`, `Content-Type: image/webp`, `Content-Length` |
| `GET /v1/assets/{id}-{w}.webp` | the variant of width `w` (480, 960, 1440, 256) | none | 404 if absent; same headers |
| `GET /v1/credits` | list of required attributions (asset id, thumbnail url, attribution_text, source_url, licence) | none | used by the footer credits page |
| `GET /v1/worlds` (exists) | add `hero` (MediaRef) and `scenes[]`; fill the existing `image_url` with the hero URL | as today | no breaking change |
| `GET /children/{id}/journey?world=` (exists) | nodes get optional `quest` {id, name, goal} and `media` | as today | optional fields |
| `GET /children/{id}/sessions/{id}` beat (exists) | `question.media[]` MediaRef objects, `scene.illustration_url` filled | as today | no answer data |
| Admin: `GET /admin/v1/assets?facet=...&q=&status=`, `GET /admin/v1/assets/{id}`, `PATCH /admin/v1/assets/{id}` (tags, alt, review_status), `POST /admin/v1/assets` (upload master, server makes variants), `GET /admin/v1/assets/resolve?criteria...` (preview what the selector returns) | admin token | audited in `admin_audit` |

`MediaRef` = `{ id, alt, width, height, color, lqip, srcset: [{w, url}], credit? }`. Language comes from `Accept-Language` like the rest of the contract.

Next.js: the frontend should not use `next/image` optimisation for these (already resized, immutable); use a plain `<img srcset sizes loading width height style={{background: color}}>` with the `lqip` as background, and a rewrite `/assets/:path* -> API/v1/assets/:path*` in `next.config.ts` so the browser and the Vercel CDN see the immutable headers; acceptance check: `curl -I` twice shows a cache hit header (P6).

### 4.6 What changes in the OpenAPI contract (`../frontend/contracts/openapi.yaml`)

Every row needs a line in `BACKEND_REQUIREMENTS.md` (next free ids are BE-67 and CR-11 at the time of writing; P2 renumbers if taken). Proposed rows, status `proposed`, owner backend (P2), priority P0 unless noted:

| ID | Change | Schema impact |
|---|---|---|
| CR-11 | `WorldId` enum grows from 8 to 20 values | breaking for exhaustive client switches: frontend must treat unknown ids as generic |
| CR-12 | `World` gets optional `hero` (MediaRef), `scenes[]`, `grade_min`, `grade_max`; `image_url` is now filled | additive |
| CR-13 | `Question` generalised: `kind` (type), `prompt`, `story`, optional `choices` (min 2, max 5, were exactly 4), `items` (order/match), `visual`, `media[]`, `hint_available` | `choices` constraint relaxed; additive otherwise |
| CR-14 | `AnswerValue` extended: `array of string` (order), `array of [string,string]` (match pairs), `boolean` (true/false) | additive `oneOf` |
| CR-15 | `Beat.scene` may be `ready` at once with curated text, `illustration_url` filled | behaviour |
| CR-16 | New `MediaRef` schema | additive |
| CR-17 | `GET /assets/{id}.webp`, `GET /assets/{id}-{w}.webp`, `GET /credits` (public) | new paths, `x-roles: []` |
| CR-18 | Journey node optional `quest` and `media` | additive |
| BE-67 | A `Beat` never contains `answer`, `explain`, `calc`, `common_errors`, `misconception` (extends BE-01 to the new fields; test serialises a Beat) | rule |
| BE-68 | Curated bank is primary; generator only when the child exhausted the bank; per-child no-repeat via `child_questions` | rule |
| BE-69 | `AnswerResult` for curated rows returns `explain` (localised) after answering and the matched `misconception` code | additive field |
| BE-70 | `hint` endpoint or field: `POST .../beats/{id}/hint` returns the hint text (counts as a hint in BKT as today if one exists, else new) | proposed, P1 |
| BE-71 | Asset responses: ETag, immutable, 304 | rule |
| BE-72 | Mongo content sync (3.6) | rule |

Admin endpoints are not in `openapi.yaml`; they go into `docs/ADMIN_API.md` and `../frontend/contracts/openapi.admin.yaml` (same owner P2, consumer P5b).

---

## 5. Image sourcing

### 5.1 Verified licence summary (pages fetched)

| Source | Licence terms (from the page) | Script access | Attribution | Verdict |
|---|---|---|---|---|
| Pexels [S4] [S5] | Free to use, commercial OK, modification OK, attribution not required; do not sell unaltered copies, do not imply endorsement by people or brands, do not use as a trademark; identifiable people must not be shown in a bad light | REST API, free key in header `Authorization`; 200 requests/hour and 20 000/month; `size`, `orientation`, `locale`, `per_page` up to 80; fields `alt`, `avg_color`, `src.*`, `photographer`, `photographer_url` | API rule: a prominent link to Pexels where used; credit photographer when possible | **Primary photo source** |
| Unsplash [S6] [S7] | Commercial OK, no attribution required, cannot be sold unaltered, cannot be compiled to build a competing service | API demo 50 requests/hour, production 1000; API guidelines require hotlinking the returned URLs and calling a download endpoint, and require credit to photographer and Unsplash | required for API apps | Rejected for the script (hotlink rule conflicts with our self-hosted, hashed, immutable files); allowed for manual single picks |
| Pixabay [S8] [S19] | Commercial OK, no attribution required; cannot sell standalone; branded content not for commercial use; recognisable people restricted | API key, 100 requests/60 s, results must be cached 24 h, no permanent hotlinking (download is expected), no mass download | asked, not required | Optional third source for illustrations and vectors (key needed) |
| Openverse (CC0, PDM, CC BY only) [S20] | Aggregates Wikimedia Commons, Flickr, etc; per-item licence returned | **No key** for anonymous use: tested, headers show 20 requests/minute burst and 200/day sustained; `license_type=commercial`, `extension`, `category=photograph\|illustration`; returns `license`, `license_version`, `creator`, `creator_url`, `attribution`, `foreign_landing_url`, `url`, `thumbnail`, `tags`, `width`, `height` | CC BY needs creator + licence + link (use the returned `attribution`) | **No-key fallback**, filter `license in (cc0, pdm, by)` |
| Wikimedia Commons (via Openverse or `imageinfo` + `extmetadata`) [S21] [S22] | Only CC0, PD and CC BY accepted; ShareAlike would force our modified files under the same licence; NC/ND refused | Action API, no key, mandatory descriptive `User-Agent` with contact and the word `bot` [S23]; fields `LicenseShortName`, `Artist`, `AttributionRequired`, `Attribution`; multi-licensed files can give unreliable metadata, so those are skipped | author + licence + link | Same fallback, use only through the script's `User-Agent` |
| Microsoft Fluent Emoji 3D [S24] | MIT licence, copyright Microsoft Corporation (LICENSE file fetched); repo path `assets/<Name>/3D/<name>_3d.png` (verified for `Owl`); styles 3D, Color, Flat, High Contrast | `raw.githubusercontent.com`, no key | MIT notice kept in the credits page and `public/LICENSES.md` | **Selected for object sprites** (glossy 3D, kid-friendly, 256 px PNG to WebP) |
| OpenMoji [S25] | CC BY-SA 4.0 (ShareAlike) | GitHub | credit required | Not for new content. The 12 `public/reactions/*.svg` are already OpenMoji: ShareAlike risk, replace with Fluent sprites in P5a |
| Noto emoji | font software is OFL 1.1; the page fetched does not state the licence of the image files [S26] | n/a | n/a | Rejected (cannot confirm; same conclusion as `LICENSES.md`) |
| LottieFiles [S27] | "Simple License": commercial OK, modify OK, but any redistribution must carry the same licence terms; no compiling competing service | download per file | encouraged | Not used for the mascot (4.7 decision, section 7) |

### 5.2 Final choice: 3 sources

1. **Pexels** for real-world photos (needs a free key: `PEXELS_API_KEY`).
2. **Openverse** (which indexes Wikimedia Commons) as the no-key fallback and for illustrations, restricted to `cc0, pdm, by`. No key; if the user later wants more quota, register an Openverse OAuth app (optional).
3. **Microsoft Fluent Emoji 3D** (MIT) for object sprites and prop images; also source of owl-related props. No key.

Pixabay stays optional (key `PIXABAY_API_KEY`, only used if set).

### 5.3 How the script fetches about 6 images per world

Script: `scripts/assets/fetch.mjs` (Node 20, `sharp` for resize and WebP [S15], plain `fetch`). Phase P3 owns it. Steps:

1. Input `seed/assets/manifest.json`: for each world 6 entries (1 hero, 5 scenes) = `{ world, role, queries:[...], orientation, mood, topics[], objects[], min_width: 1600 }` plus a list of Fluent sprites per world: `{ fluent: "Cookie" }`. Hand-written, versioned in git, so the picks are reproducible: after the first review, the manifest is rewritten with the pinned `source_id` of each chosen photo.
2. Search: Pexels `GET https://api.pexels.com/v1/search?query=<q>&orientation=landscape&size=large&per_page=20` with the key. Skip results when `alt` matches the people deny regex (`man|woman|boy|girl|child|kid|baby|people|person|portrait|family|crowd|face`), or the query list is marked `brand_risk`. Keep 6 candidates per entry for review. If `PEXELS_API_KEY` is empty, use Openverse: `GET https://api.openverse.org/v1/images/?q=<q>&license_type=commercial&category=photograph&extension=jpg&page_size=20`, then keep `license in {cc0, pdm, by}` and the same people regex on `title/tags`; for Commons items, re-query `imageinfo/extmetadata` with the `User-Agent` `NumerixAssetBot/1.0 (contact: wandjiperrin@gmail.com) node-fetch` and drop files where `AttributionRequired` is missing or the licence short name contains `SA`, `NC` or `ND`.
3. Rate: one request per 2 s (Pexels budget 200/h; the whole job needs under 150 requests), exponential backoff on 429, stop at 80 percent of `X-Ratelimit-Remaining`.
4. Download `src.original` (or `large2x`, whichever is at least 1600 px wide), compute `sha256` of the normalised master (sharp `rotate()`, strip metadata) and use `a_<20 hex>` as id; skip if already in `seed/assets/out/`.
5. Variants with sharp: hero 480, 960, 1440 wide (`cover`, 16:9 crop for hero, 4:3 for scenes), scenes 480 and 960; `webp({quality:80, effort:4})`. LQIP: resize to 24 px wide, `webp({quality:40})`, base64. `dominant_color` from `stats().dominant`, else Pexels `avg_color`. Sprites: Fluent `3D/<name>_3d.png` resized to 256 px, `webp({quality:90})` keeping alpha.
6. Write `seed/assets/out/<id>-<w>.webp` and `seed/assets/assets.json` (one object per asset with every column of 4.2 except bytes, plus `tags[]` and `variants[]` file names). `alt_fr` and `alt_en` are required fields: the script writes a draft from the source `alt` + world labels, flags `review_status: pending`, and P3 reviews all of them. Alt text describes what is visible and never hints at a maths answer.
7. Review gate: the script emits `seed/assets/contact-sheet.html` (thumbnails, licence, creator, alt, checkbox). A human flips `approved` in `assets.json` (or P3 does and the user confirms). Only `approved` assets are loaded by `cmd/seed`. Reject anything with an identifiable face, readable brand/logo, weapons, alcohol, or anything not suited to ages 6 to 12.
8. Attribution record: `attribution_text` = Pexels: `Photo by <photographer> on Pexels`; Openverse: the returned `attribution` string; Fluent: `Fluent Emoji by Microsoft, MIT`. `GET /v1/credits` and the parent-area credits page print them; `public/LICENSES.md` gets the sources table.
9. Idempotent: re-running changes nothing unless the manifest changed.

User-supplied key: **`PEXELS_API_KEY`** (free: create a Pexels account, then API page [S5]). Optional: `PIXABAY_API_KEY`. Not needed: Openverse (anonymous), Wikimedia, Fluent. Without the Pexels key the script still produces a full set from Openverse/Commons, with fewer perfect matches.

---

## 6. Animations

Principles (all from the cited docs): respect `prefers-reduced-motion` [S28]: remove or replace large scale, pan and parallax with opacity fades, keep only essential motion and make it subtle. Motion for React: `MotionConfig reducedMotion="user"` disables transform and layout animations but keeps opacity and colour; `useReducedMotion` for custom cases such as parallax set to `y: 0` [S29]. Use `startViewTransition` with a fallback for navigation between pages and disable its pseudo-element animations under reduced motion [S30]. Only animate `transform` and `opacity` (compositor friendly), never `width/height/top/left`, and keep every animation under 600 ms except the mascot idle loop. No timer is ever shown; animation never conveys time pressure.

Library decision: **Motion (package `motion`, import `motion/react`)** for components + plain CSS for tiny loops (blink, float). No Lottie in the question flow (drops the 1.2 MB dotlottie wasm after the mascot migration).

Specs (spring = `{type:"spring", stiffness, damping}`):

| Moment | Spec | Reduced motion |
|---|---|---|
| Question enter | card: `opacity 0 -> 1`, `y 16 -> 0`, spring 380/32 (about 350 ms); prompt, visual, options stagger 60 ms (`staggerChildren: 0.06`); the scene image fades in over its `lqip` background (`opacity` 250 ms) | fade only, 150 ms, no stagger |
| Option select (tap) | `whileTap scale 0.97`; selected card gets a 2 px accent ring and `scale 1.02` spring 500/30; others dim to 0.7 opacity | ring only, no scale |
| Correct | selected card turns green-tinted (colour 200 ms), small bounce `y: [0,-8,0]` 350 ms; 8 to 12 star/confetti particles (transform + opacity, 700 ms, `will-change` removed after) from the card; mascot `cheer`; sound optional (existing setting) | colour change + mascot pose swap with opacity, no particles |
| Wrong (gentle) | no red cross, no shake; card gets warm amber tint and a soft wobble `rotate: [0,-2,2,-1,0]` 300 ms; the correct option is NOT revealed until the hint/explain step; mascot `think` or `sad-soft` ("not yet" tone); the contract already forbids success/failure chrome | amber tint only |
| Explain reveal | panel slides `y 12 -> 0` + fade 250 ms, text word groups fade in 40 ms apart (max 8 groups) | fade only |
| World map transitions | world card to world page uses a shared element: `view-transition-name: world-<id>` on the card image so the hero grows into the page; fallback is a 200 ms cross fade; path nodes appear with 40 ms stagger and a `pathLength` draw (SVG stroke) 600 ms | instant, no draw |
| Hero parallax | hero image moves with `useScroll`/`useTransform` at 0.25 times scroll (max 24 px), a foreground sprite layer at 0.5; image has `loading=eager` + LQIP | `y: 0`, no scroll link |
| Mascot reactions | owl state machine (section 7): idle, wave, think, cheer, sad, sleepy; transitions are 200 ms cross fades of the pose parts plus a spring on wings; idle: blink every 3 to 5 s (random), breathe `scaleY 1 -> 1.02` over 3 s | static pose, blink only |
| Chapter done | stars fly to the counter along a path (`offset-path` or a 2-point animate), 600 ms; haptic if supported | stars appear in place |

Performance limits: at most 20 simultaneous animated nodes; particles are pooled; animations stop when the tab is hidden; the whole animation layer is lazy-loaded (`dynamic import` of the confetti module) so first load stays light.

---

## 7. Mascot decision: a hand-built layered SVG owl

Decision: **(b) a hand-built layered SVG owl, animated with Motion and CSS.** Name: "Numa" (works in fr and en).

Evaluation:
- (a) Licensed sets: Rive Community files are shared under CC BY, remixable [S31]. The only owl found, "Flying Owl" (antonin.jech, CC BY), is one flying/sitting animation, not a 5-pose set (idle, wave, think, cheer, sad), and Rive needs its runtime (about the same weight as dotlottie). LottieFiles Simple License lets us use and modify commercially but forces the same terms on every redistribution of the file and each animation may carry its own terms [S27]; no single owl pack with all five poses was verified. Stitching five unrelated owls would break character consistency, the main thing a mascot is for. Duolingo itself drives poses with a Rive state machine over one rigged character [S32], which is what a single owned rig gives us too.
- (b) Own SVG: we own the licence (no attribution or share-alike risk, consistent with the kids-app rule), one rig gives all poses and later ones (sleepy, point, read) for free, tiny size (about 15 to 25 KB), crisp at any size, themeable (accent colour per world), and animates with the same engine as the rest of the UI. Cost: some drawing time in P5a.

SVG build spec (`frontend/public/mascot/numa.svg` source and `frontend/src/components/mascot/Numa.tsx`):
- `viewBox="0 0 400 400"`, flat shapes with soft gradients, 3 to 4 brand colours (body warm brown `#8B5E3C`, belly cream `#F6E7C8`, wing darker brown `#6F4A2F`, beak and feet orange `#F29E38`, eyes white with dark pupils `#2B2118`), contrast checked against light and dark backgrounds.
- Separate groups with ids (each is a Motion `motion.g` with its own transform-origin): `body`, `belly`, `head` (contains `tuft-l`, `tuft-r`, `face-disc`), `eye-l`, `eye-r` (each: `eyeball`, `pupil`, `lid`), `brow-l`, `brow-r`, `beak`, `wing-l`, `wing-r`, `foot-l`, `foot-r`, `tail`, optional `glasses`, `shadow`.
- Pose states (variants): `idle` (breathe, blink), `wave` (`wing-r` rotate -50 deg and back x3, 1.2 s, head tilt 4 deg, big smile pupils), `think` (head rotate 8 deg, `wing-l` raised to the beak, pupils up-left, `brow-r` raised, three dots bubble fades in), `cheer` (jump `y -18` spring, both wings up 65 deg, eyes become happy arcs, tuft wiggle), `sad` (head rotate -6 deg and `y +4`, brows slanted, lids half closed, no tears, wings down; used for "not yet" with a warm tone), `sleepy` (lids closed, slow breathing; for the end-of-day screen).
- API: `<Numa pose="idle|wave|think|cheer|sad|sleepy" size={96} look={{x,y}} />`; the play page maps outcomes: question shown -> `idle`, hint opened -> `think`, correct -> `cheer`, `erreur_typique` or `erreur_aleatoire` -> `sad` (soft), chapter done -> `wave`.
- Files to remove after P5a: `public/mascot/rabbit-balloon.lottie`, `public/mascot/dotlottie-player.wasm` (if nothing else uses them), and the `@lottiefiles/dotlottie-*` dependency.
- Accessibility: `role="img"` with `aria-label` from the pose ("Numa the owl is cheering") in both languages, decorative when beside text.

---

## 8. Admin workflow

App: `../frontend/admin` (Next.js app dir, routes under `src/app/(app)/`). New screens (P5b) call new `/admin/v1` endpoints (P2) through the admin `/api/*` proxy; same auth (`ADMIN_API_TOKEN`), CORS, problem+json, pagination and audit rules as `docs/ADMIN_API.md`.

| Screen | Route | What |
|---|---|---|
| Question list | `/questions` | table with filters world, quest, skill, type, grade, difficulty, status, author, `ai_drafted`, tag, text search (fr/en); counts per quest against `target_count`; bulk select (validate, submit for review, publish, retire) |
| Question editor | `/questions/[id]` and `/questions/new` | bilingual side-by-side fields, type switch that changes the answer form, choices with misconception picker (codes filtered by skill), `calc` field with a live "verified: yes/no" badge, validation panel (the exact Go rules, shown live through `POST /questions/validate`), live preview of the child's screen in fr or en with the real components, media picker, version history with diff and restore |
| Media picker | modal | criteria form (world, topic, object, mood, kind) calling `/assets/resolve`, grid of matching assets with licence badge, pick explicit or "use criteria" |
| Review queue | `/review` | items in `review`, one-click approve/publish by someone other than the author when `ai_drafted`; reject with a comment |
| Quests and worlds | `/worlds`, `/worlds/[id]` | edit names, taglines, quest list and `target_count`, pick hero asset; coverage dashboard (types present vs quota) |
| Import / export | `/import` | export a world or the whole bank as seed JSON; import a JSON file: dry run shows added/changed/invalid, then apply (writes `draft` unless the user chose publish and has the role) |
| Bulk validate | button | validates every question or a filter, downloads a report |
| Assets | `/media` | list/filter by facets, edit tags and alt, approve or reject, upload a master (server makes the variants) |

Endpoints (all under `/admin/v1`, all mutations audited in the same transaction; each content mutation also enqueues the Mongo outbox rows of 3.6):

```
GET    /questions?world=&quest=&skill=&type=&grade=&difficulty=&status=&q=&ai=1&page=&page_size=
GET    /questions/{id}                      -> current + version list
POST   /questions                           -> create (status draft)
PUT    /questions/{id}                      -> save new version (draft or review), If-Match: <version>
POST   /questions/{id}/transition           -> {to: "review"|"published"|"retired"|"draft", comment}
GET    /questions/{id}/versions[/{n}]       POST /questions/{id}/restore {version}
POST   /questions/validate                  -> body = question object, returns errors+warnings (same code as the test)
POST   /questions/validate-all              -> report
POST   /questions/ai-draft                  -> see below
GET    /questions/export?world=&status=     POST /questions/import?dry_run=1
GET    /quests, /worlds, PUT /worlds/{id}, PUT /quests/{id}
GET    /assets, GET /assets/{id}, PATCH /assets/{id}, POST /assets, GET /assets/resolve
GET    /content/coverage                    -> per world/quest counts vs targets and type quotas
```

AI draft button. `POST /admin/v1/questions/ai-draft` body `{world, quest, type, grade, difficulty, topic_hint?, language_primary}`. Behaviour:
- Disabled (`503 ai_disabled`) unless `ANTHROPIC_API_KEY` is set. It reuses the existing client in `internal/narrator/anthropic.go` and the configured model env var (new `ANTHROPIC_DRAFT_MODEL`, default the same as the narrator's).
- Request asks for the question JSON of 3.3 using the Messages API structured output (`output_config.format` with the question JSON Schema, every object `additionalProperties:false`) [S33]. The model proposes text, distractors and misconception codes from the supplied list. It is never trusted for numbers: the server recomputes the `calc`, runs the full validator (3.5), and rejects or marks `validation_errors` on the draft if anything fails.
- The row is always created with `status=draft`, `ai_drafted=1`, `author="ai:<model>"`, `source="ai-draft"`. Publishing requires an admin who is not the author and sets `reviewed_by`; the SQL CHECK in 3.1 enforces it, the API enforces it again.
- The AI is never called by the child-facing API. Cost control: per-admin limit 30 drafts/hour, prompt logged in the audit `detail` without the key.

Roles: first version has one admin role (as today); the review-by-another-person rule uses the admin `actor` name carried by the token mapping that exists in `admin.go` (P2 checks what identity is available; if only one token exists, the rule degrades to "reviewer comment required").

---

## 9. Production DB deploy scripts

Goals: idempotent, re-run updates only changed content, dry-run, backup first, rollback, no downtime for children reading.

Layout (P2): `cmd/seed/main.go`, `internal/content/{schema,validate,load,calc,pick}.go`, `scripts/prod/deploy-content.sh` (Linux, Oracle VM) and `scripts/prod/deploy-content.ps1` (Windows dev), `docs/CONTENT_DEPLOY.md` (short runbook, written in P2).

`cmd/seed` flags: `--db <path>`, `--seed-dir seed`, `--dry-run`, `--validate-only`, `--only worlds|quests|questions|assets`, `--world <id>`, `--backup-dir`, `--no-backup` (refused when `APP_ENV=production`), `--rollback <run_id>`, `--actor`, `--allow-retire` (without it, removals are only reported).

Flow:
1. Validate: load all seed JSON, run the validator (3.5) and curriculum cross checks; any hard error aborts before the DB is touched.
2. Migrations: `store.Migrate` applies new numbered SQL files (existing runner, `internal/store/migrate.go`); never edit old files.
3. Backup: (a) check Litestream is healthy (`litestream ltx` or the replica `generation` is less than 60 s old; skip with a warning on dev); (b) `VACUUM INTO '<backup-dir>/pre-seed-<ts>.db'` on the live DB (consistent copy, works while the API runs); (c) write `seed_runs` row `status=started` with `backup_path`. Restore reference: `litestream restore -timestamp <ts>` or copy the file back [S18].
4. Diff: for every object compute `sha256` of canonical JSON (sorted keys, normalised whitespace, `version`/`status`-free fields excluded as defined) and compare with `content_hash` in the table: `added`, `changed`, `unchanged`, `missing_in_seed` (candidates for `retired`).
5. Dry run prints the counts and the first 20 changes per kind and exits 0 without writing (also writes nothing to `seed_runs`).
6. Apply in ONE write transaction per kind (worlds, quests, assets, then questions in batches of 200): upsert changed rows, `version = version + 1`, insert `question_versions` (`action='seed'`), `admin_audit` rows (`actor='seed:<run_id>'`), outbox rows for Mongo. Unchanged rows are not touched (no `updated_at` churn, no Mongo traffic). Assets are inserted only if `id` (hash) is new.
7. Post checks: re-run the DB-side validator on what was written, assert counts (worlds = 20, every quest has at least `target_count - 2` published), `PRAGMA integrity_check`, `PRAGMA foreign_key_check`. On failure the transaction of the current kind is rolled back and the script stops with the backup path printed.
8. Finish: `seed_runs.status=ok`, print summary; `docs/MONGO_SYNC.md` health shows pending outbox rows draining.

Rollback: `--rollback <run_id>` restores, for every question changed in that run, the previous `question_versions` snapshot as a new version (`action='rollback'`) and sets rows added by the run to `retired`; for a catastrophic problem restore the pre-seed backup file or `litestream restore`. Retiring never deletes rows (served history and `child_questions` keep working).

Safety rules: published content that a child already saw never changes under the same `questions` row (the `bank_version` rule in 1.3). `--dry-run` is the default in the `.sh`/`.ps1` wrappers; the wrapper needs `--apply`.

CI: `go test ./internal/content -run TestSeed` validates all seed files on every PR (P4 agents run it before committing).

---

## 10. Phases, file ownership, acceptance checks

Order: P2 first (its first deliverable is the validator, schema and seed layout, so P3 and P4 can start); P3 and P4 run in parallel after "P2 gate A"; P5a and P5b start after "P2 gate B" (API running with fixtures); P6 last.

Directory ownership (no two phases edit the same path):

| Phase | Owns (creates and edits) | Must not touch |
|---|---|---|
| P2 backend | `numerix-api/internal/store/migrations/0008_content.sql`, `0009_assets.sql`; `internal/content/**`; `internal/assetstore/**`; `internal/app/game_session.go`, `game_answer.go`, `game_catalog.go` (picker, evaluator, worlds/journey); `internal/adminapi/{questions,assets,worlds,content}*.go`; `internal/mongosync/**` (new kinds); `internal/httpapi/**` (asset handler); `cmd/seed/**`; `scripts/prod/**`; `seed/schema/**`, `seed/worlds.json`, `seed/quests.json`; `docs/ADMIN_API.md`, `docs/MONGO_SYNC.md`, `docs/CONTENT_DEPLOY.md`; `../frontend/contracts/{openapi.yaml,openapi.admin.yaml,BACKEND_REQUIREMENTS.md}`; regenerated `api/` | `seed/questions/**`, `seed/assets/**`, any `frontend/src`, `frontend/admin` |
| P3 assets | `numerix-api/scripts/assets/**` (Node), `seed/assets/**` (`manifest.json`, `assets.json`, `out/*.webp`, `contact-sheet.html`), `seed/assets/vocab.json`, `../frontend/public/LICENSES.md` (sources table only) | Go code, DB migrations |
| P4 authoring (4 agents) | one file each: `seed/questions/<world>.json` for its 5 worlds. Agent A: `nature, espace, cuisine, sport, construction`; B: `medecine, science, echecs, ocean, dinosaures`; C: `animaux, musique, transport, pirates, marche`; D: `meteo, robots, arts, hiver, jeux` | everything else, including `quests.json` (ask P2 owner via the list of problems in the final report) |
| P5a play UI | `../frontend/src/app/play/**`, `../frontend/src/app/worlds/**`, `../frontend/src/app/globals.css` (animation tokens only), `../frontend/src/components/**` (new: `mascot/Numa.tsx`, `media/Picture.tsx`, `play/*`), `../frontend/public/mascot/**`, `../frontend/next.config.ts` (asset rewrite), `../frontend/messages/*` strings | `frontend/admin/**`, contracts, Go |
| P5b admin UI | `../frontend/admin/src/**` (new routes `/questions`, `/review`, `/worlds`, `/import`, `/media`), `../frontend/admin/src/messages/*` | `frontend/src/**`, contracts |
| P6 integration | `docs/CONTENT_TODO.md` ticks, e2e/test files `numerix-api/internal/app/content_e2e_test.go`, `../frontend/e2e/**`, fixes by agreement, merge | new features |

### P2 backend. Deliverables and acceptance
Deliverables: migrations 0008/0009; content package (schema, validator, calc, loader, picker, evaluator); `pickQuestion` bank-first with generator fallback; `StartSession`/`Choose` accept skills with bank rows; asset store + HTTP handler; admin API (section 8); Mongo kinds; `cmd/seed`; prod scripts; contract and `BACKEND_REQUIREMENTS` rows; 20 worlds in `worlds.json`, 89 quests in `quests.json`; example question files (2 per type) used by tests.
Acceptance checks:
1. `rtk go test ./... -count=1` green; `rtk go vet ./...` clean.
2. Validator tests: one passing and one failing fixture per rule of 3.5 (answer missing from choices, duplicate distractors, missing `en`, bad `calc`, answer leak in `story` in digits and in words, timer word, sentence too long, em dash, unknown misconception code).
3. Picker tests: curated served first; a child never gets the same bank id twice until exhausted; after exhaustion the generator serves; with no generator the oldest bank row is re-served; same inputs give the same pick (determinism); type differs from the previous beat when possible.
4. Beat serialisation test: no `answer, explain, calc, common_errors, misconception` keys (BE-67).
5. `cmd/seed --dry-run` twice shows `added=N` then `changed=0 unchanged=N`; editing one question changes exactly one row and adds one `question_versions` row; `--rollback` restores it; backup file exists and opens.
6. Assets: `GET /v1/assets/{id}-960.webp` returns 200 with `ETag` and `Cache-Control: public, max-age=31536000, immutable`; second request with `If-None-Match` returns 304; bad id 404.
7. Mongo: with the fake sink, one edit produces `question_bank` and `question_audit` documents; `question_stats` absent below 5 children; PII scan test passes.
8. Contract: `openapi.yaml` lints, generated Go matches (`scripts/gen.sh`), every new row exists in `BACKEND_REQUIREMENTS.md`.
9. Admin API: all endpoints have tests with the dev token; AI draft returns 503 without key and, with a stub provider, creates `status=draft, ai_drafted=1`; publishing an AI draft without a reviewer is refused (API and SQL).

### P3 assets. Deliverables and acceptance
Deliverables: `fetch.mjs`, manifest for 20 worlds, vocabulary, about 120 photos (6 per world) in 2 or 3 widths, about 250 sprites, `assets.json`, contact sheet, credits data, `LICENSES.md` table.
Acceptance:
1. `node scripts/assets/fetch.mjs --dry-run` lists the planned requests and respects the rate cap; re-run produces no diff.
2. Total `seed/assets/out` size between 5 and 20 MB.
3. 100 percent of assets have `licence` in the allowed list, `attribution_text`, `alt_fr`, `alt_en`, `creator`, `source_url`; none is SA/NC/ND; none flagged `people_free=0` is approved.
4. Every world has 1 hero and at least 4 scenes approved; every quest topic of section 2.4 has at least 2 matching assets or a Fluent sprite (script prints a coverage table).
5. A human or the user signs the contact sheet; a reviewer lists rejected items and why.
6. Works with and without `PEXELS_API_KEY` (second mode proven by a run log).

### P4 question authoring (4 agents). Deliverables and acceptance
Deliverables: `seed/questions/<world>.json` x 20, `status: "review"` (not published; a human or P6 publishes after review), maths verified by `calc`, French written first as Quebec French (fr-CA words: "dépanneur", "bonbon", "cartable" fit), English as Canadian English (metres, litres, "mom" not "mum"), each question with a short story that gives the number a purpose (a decision the hero makes), not a bare sum.
Acceptance:
1. `rtk go test ./internal/content -run TestSeed -count=1` green for the agent's files (hard errors zero).
2. Per world: 48 to 60 questions, all 8 types, quotas of 2.2, difficulty spread, every quest within `target_count` minus 2 and plus 3.
3. At least 70 percent of questions have `common_errors` or choice `misconception` codes from the real list; every `hint` guides without giving the answer; every `explain` teaches the strategy.
4. A self-review pass: 10 randomly picked questions per quest re-read for tone (kind, no pressure), cultural spread of first names, no brands, no scary themes.
5. No duplicate or near duplicate prompts inside a world (warning count zero or justified).

### P5a play pages. Deliverables and acceptance
Deliverables: all question type components (numeric keypad, choice cards, true/false, ordering, matching, visual blocks, estimate), `Picture` component (srcset, LQIP, colour), world pages with hero and parallax, map transitions, animation layer of section 6, Numa SVG mascot, reaction faces replaced by Fluent sprites, removal of rabbit/Lottie.
Acceptance:
1. `rtk npm run lint && rtk npm run build` pass; unit tests for each question component with fr and en.
2. Reduced motion: with `prefers-reduced-motion: reduce` emulated, no transform animation runs (test checks computed styles / `useReducedMotion` paths).
3. Lighthouse mobile (Moto G class): Performance at least 85 on a play page, LCP image has width/height and preloaded, CLS below 0.05; animation JS lazy chunk under 40 KB gzip.
4. No timer, no red cross; wrong answer shows the amber state and Numa `sad`; manual screenshot set for the 8 types in both languages.
5. Every media element has the `alt` from the API; keyboard and screen reader can answer each type (ordering/matching have keyboard alternatives).
6. Bundle: after removing dotlottie, `public/` shrinks by at least 1.2 MB.

### P5b admin UI. Deliverables and acceptance
Deliverables: screens of section 8 with types, hooks, fr/en strings (the admin already has `en.ts`, `fr.ts`).
Acceptance:
1. Build and lint pass; each screen has a happy-path test against a mock server of the admin API.
2. Editor shows the same validation errors as `POST /questions/validate` (golden fixtures).
3. AI draft button hidden/disabled when `/admin/v1/health` reports `ai: false`; created drafts show a visible "AI draft, needs review" badge and cannot be published by their author.
4. Import dry run on a seed file shows added/changed/invalid before apply; export then import is a no-op.
5. Media picker returns the same asset as `GET /v1/...` for the same criteria.

### P6 integration. Acceptance
1. Fresh DB: `cmd/seed --apply` loads 20 worlds, 89 quests, at least 900 questions in `review`; P6 publishes after sampling 5 percent of each world.
2. End-to-end: a test child plays one chapter in each of the 20 worlds in fr and en (script), never sees a repeated question, sees curated text and media, the generator is not called while bank rows remain.
3. Mongo: with a real or local `mongod` (`MONGODB_URI`), collections `question_bank`, `question_audit`, `question_stats` receive the documents; re-seed produces zero new writes.
4. Prod rehearsal: run the `.sh` wrapper against a copy of a production-like DB: backup created, dry run, apply, rollback, `litestream restore` of the backup.
5. Licence review: `/v1/credits` lists every attribution-required asset; `LICENSES.md` matches `assets.json`.
6. All CONTENT_TODO items checked or explicitly moved; Git history has no AI attribution lines.

---

## 11. Sources

- [S1] Quebec PFEQ mathematics progression PDF (the repo's `meta.url`): https://cdn-contenu.quebec.ca/cdn-contenu/education/pfeq/primaire/progressions-apprentissages/PFEQ-progression-apprentissages-mathematique-primaire.pdf (binary, fields confirmed through the repo JSON and S2)
- [S2] Quebec.ca PFEQ primary mathematics page (arithmetic by cycle, probability, operations structures): https://www.quebec.ca/education/prescolaire-primaire-et-secondaire/programmes-formations-evaluation/programme-formation-ecole-quebecoise/primaire/mathematique
- [S3] Ontario elementary mathematics curriculum, strands: https://www.dcp.edu.gov.on.ca/en/curriculum/elementary-mathematics/context/the-strands-in-the-mathematics-curriculum (strands B Number, C Algebra, D Data, E Spatial Sense, F Financial Literacy confirmed by search result summary)
- [S4] Pexels licence: https://www.pexels.com/license/
- [S5] Pexels API documentation: https://www.pexels.com/api/documentation/
- [S6] Unsplash licence: https://unsplash.com/license
- [S7] Unsplash API documentation: https://unsplash.com/documentation
- [S8] Pixabay content licence summary: https://pixabay.com/service/license-summary/
- [S9] Pool and Horan, controlled vocabularies for digital images (Corbis taxonomists): https://henrystewartpublications.com/journal/journal-of-digital-media-management/using-controlled-vocabularies-to-organise-digital-images-for-improved-search-results/
- [S10] IPTC metadata (creator, rights, keywords): https://cloudinary.com/glossary/iptc-metadata
- [S11] MDN Cache-Control, `immutable` and fingerprinted URLs: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Cache-Control
- [S12] Carnegie/EDM 2025 note that word problems are shorter than readability formulas assume: https://educationaldatamining.org/edm2025/proceedings/2025.EDM.short-papers.189/2025.EDM.short-papers.189.pdf
- [S13] EDUMATH, lower reading levels preferred for word problems: https://arxiv.org/pdf/2510.06965
- [S14] web.dev choose the right image format: https://web.dev/articles/choose-the-right-image-format
- [S15] sharp output options (webp, avif): https://sharp.pixelplumbing.com/api-output
- [S16] Mux, tiny real image placeholders vs BlurHash: https://www.mux.com/blog/blurry-image-placeholders-on-the-web
- [S17] SQLite, 35 percent faster than the filesystem: https://www.sqlite.org/fasterthanfs.html
- [S18] Litestream how it works: https://litestream.io/how-it-works/
- [S19] Pixabay API docs (key, 100 per 60 s, cache 24 h, no hotlink): https://pixabay.com/api/docs/
- [S20] Openverse API (tested anonymous call, field names, limits 20/min and 200/day): https://api.openverse.org/v1/images/ ; docs https://docs.openverse.org/
- [S21] Wikimedia Commons reuse and attribution: https://commons.wikimedia.org/wiki/Commons:Reusing_content_outside_Wikimedia
- [S22] CommonsMetadata extmetadata fields: https://www.mediawiki.org/wiki/Extension:CommonsMetadata/en
- [S23] Wikimedia User-Agent policy: https://foundation.wikimedia.org/wiki/Policy:Wikimedia_Foundation_User-Agent_Policy
- [S24] Microsoft Fluent Emoji (MIT licence file; `assets/Owl/3D/owl_3d.png`): https://github.com/microsoft/fluentui-emoji ; https://raw.githubusercontent.com/microsoft/fluentui-emoji/main/LICENSE
- [S25] OpenMoji (CC BY-SA 4.0): https://github.com/hfg-gmuend/openmoji
- [S26] Noto emoji licence file (font OFL, image licence not stated there): https://raw.githubusercontent.com/googlefonts/noto-emoji/main/LICENSE
- [S27] LottieFiles Simple License: https://lottiefiles.com/page/license
- [S28] MDN prefers-reduced-motion: https://developer.mozilla.org/en-US/docs/Web/CSS/@media/prefers-reduced-motion
- [S29] Motion for React accessibility (`MotionConfig`, `useReducedMotion`): https://motion.dev/docs/react-accessibility
- [S30] MDN View Transition API: https://developer.mozilla.org/en-US/docs/Web/API/View_Transition_API
- [S31] Rive community marketplace (CC BY) and the Flying Owl file: https://www.rive.app/docs/community/marketplace-overview ; https://rive.app/community/4329-8915-flying-owl
- [S32] Duolingo engineering, Rive state machine with visemes for characters: https://blog.duolingo.com/world-character-visemes
- [S33] Anthropic structured outputs (GA `output_config.format`, `additionalProperties:false`), as summarised by the Vercel AI Gateway docs: https://vercel.com/docs/ai-gateway/sdks-and-apis/anthropic-messages-api/structured-outputs

Things not verified (be honest in later phases): Unsplash API terms beyond the first 100 000 characters of the page; Noto emoji image licence; the PFEQ PDF text (binary, relied on the repo's already-extracted JSON); the Ontario page body (strands confirmed from the search summary); the exact count of misconception codes (99 in JSON versus about 110 in the brief); that the Vercel CDN honours `immutable` through a rewrite (acceptance check in P2/P6).
