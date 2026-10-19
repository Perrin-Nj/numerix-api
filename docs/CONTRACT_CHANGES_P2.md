# Contract changes of phase P2 (for the frontend agent)

Source: `../frontend/contracts/openapi.yaml` (regenerated Go: `bash scripts/gen.sh`). Every existing call still works: generated questions are unchanged and every new field is optional. Rows CR-11..CR-18 and BE-67..BE-72 are in `BACKEND_REQUIREMENTS.md`.

| Id | Change |
|---|---|
| CR-11 | `WorldId` has 20 values (added ocean, dinosaures, animaux, musique, transport, pirates, marche, meteo, robots, arts, hiver, jeux). Treat an unknown id as a generic world. New children get all 20 in `enabled_worlds`; existing children keep their stored list (a parent enables the new worlds in settings). |
| CR-12 | `World` gains `grade_min`, `grade_max`, `hero` (MediaRef), `scenes[]`; `image_url` is the widest hero URL ("" until art is approved: draw a gradient from `accent_color`). `GET /worlds` and `home.worlds` read the database. Legacy names, hooks and colours are unchanged. |
| CR-13 | `Question` gains `kind` (numeric, choice, truefalse, order, match, word, visual, estimate), `prompt`, `story`, `items{cards|left,right}`, `visual{kind,data}`, `media[{role,image}]`, `hint_available`. `choices` may hold 0 to 5 values (generated questions still send 4). `math_text` stays required (curated questions without one send the prompt). Generated questions omit all new fields. |
| CR-14 | `AnswerValue` also accepts boolean (truefalse), list of ids (order), list of `[left,right]` pairs (match). A choice answer is the value of the chosen card exactly as sent. |
| CR-15 | A curated beat has `scene.status = ready` with `text` = the question story and `illustration_url` = widest scene image; the narration stream replays the same text (denouement uses the question's own feedback or a neutral line; no model is called). |
| CR-16 | New schemas `MediaRef`, `MediaCredit`, `QuestionMedia`, `CreditItem`. URLs are `<ASSET_BASE_URL>/<id>-<w>.webp` (default prefix `/assets`: add the `next.config.ts` rewrite `/assets/:path*` to `<API>/v1/assets/:path*`). |
| CR-17 | `GET /v1/assets/{file}` (public): `a_<20 hex>.webp` widest, `a_<20 hex>-<w>.webp` a variant; `ETag`, `Cache-Control: public, max-age=31536000, immutable`, 304 on `If-None-Match`, 404 for unknown or unapproved. `GET /v1/credits` (public) lists attributions. |
| CR-18 | `JourneyNode` gains `quest{id,name,goal}` and `media`; the label is the quest name. A node exists for each quest whose skill is playable in the world. A world with no playable quest returns an empty journey (200) and `startSession` answers 422. |
| BE-69 | `AnswerResult` gains `explain`, `misconception` (code, for analytics) and `feedback` for curated questions. |
| BE-70 | New `POST /sessions/{id}/beats/{beatId}/hint` -> `{hint}` (404 when the question has none, 409 after the answer). |
| other | `Skill.implemented` and the `implemented` filter now mean a generator OR at least 8 published bank questions. `GET /skills?world=` includes the skills of that world's quests. |

Question selection order: published bank question for (world, skill) the child has not seen (grade <= child grade, nearest target difficulty, type different from the previous beat), then the generator, then the oldest question re-served.
