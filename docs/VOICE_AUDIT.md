# Voice audit and human voice design

Date: 2026-10-19. Scope: `Services/frontend` (src, public, scripts), `Services/numerix-api` (internal/narrator, seed), product docs. Read-only audit; no app code changed. Recording script: `seed/voice/script.json` (machine) and `docs/VOICE_SCRIPT.md` (printable).

## 1. How voice works today

Short answer: all spoken voice is the browser's built-in text to speech (Web Speech API, `speechSynthesis`). No human recordings, no cloud TTS (no ElevenLabs, Azure, Polly, Google TTS; grep found none in frontend or API). The API only produces text. All `.mp3/.webm` files in `frontend/public/sounds` are CC0 sound effects (Kenney plus synthesized), not voice.

### Inventory (frontend paths relative to `Services/frontend/src`)

| # | Where (file:line) | What it says | Voice and lang picked | AI/TTS? |
|---|---|---|---|---|
| 1 | `platform/speech/useSpeaker.ts:103-134` | Engine: `new SpeechSynthesisUtterance(spokenText(text, l))`, `speechSynthesis.speak` | `pickVoice(all, lang, prefs.voiceName)`; `u.voice=v; u.lang=v.lang`; rate `prefs.rate` (0.95), pitch 1.05 | Yes, browser TTS |
| 2 | `platform/speech/voices.ts:11-23` | Ranking only | Matches primary subtag only (`fr`, `en`); +100 base, +16 "natural/neural/google/microsoft", +40 female name list (France names: Hortense, Julie, Amelie...), +4 exact locale | Yes |
| 3 | `platform/speech/spoken.ts:1-20` | Rewrites `+ - x / =` to words ("plus", "moins", "fois", "divisé par", "égale", "combien") | per language | Pre-processing for TTS |
| 4 | `platform/speech/index.ts:16-30` `useReadAloud` | Wrapper; reports "no voice" once | `useLocale()` | Yes |
| 5 | `features/play/StoryScreen.tsx:105-115, 205-217` | Scene narration text (LLM or canned), spoken once complete (`sceneStatus ready/fallback`) | locale voice | Text is LLM/canned; voice is TTS |
| 6 | `features/play/StoryScreen.tsx:268-276` | Denouement text (outcome story), spoken once complete | same | same |
| 7 | `features/play/StoryScreen.tsx:493` | Replay button, `speak(s.sceneText)` | same | same |
| 8 | `features/play/DoneScreen.tsx:42-45, 78` | Chapter appreciation (`lines.appreciation.pass.0-5`, `.miss.0-3`) via `useSpokenLine` | `lang` | Yes. Caption is hidden when voice works (`{mute ? appreciation : ' '}`) |
| 9 | `features/play/HomeFeed.tsx:83, 124` | First-visit greeting (`lines.mascot.welcome.*`, includes `{name}` variants) | `lang` | Yes |
| 10 | `features/play/MeScreen.tsx:85, 113` | "Test voice" sample: "Bonjour ! Je suis ta voix pour les histoires. On y va ?" | locale | Yes |
| 11 | `features/play/MeScreen.tsx:59`, `data/session/clear.ts:22` | `speechSynthesis.cancel()` on language switch and sign-out | n/a | Cleanup |
| 12 | `platform/prefs/store.ts:22` | Defaults `voice: true`, `rate: 0.95`, `voiceName: null`, `hintDelaySec: 30`, `hintVolume: 0.5` | n/a | Config (voice ON by default) |
| 13 | `StoryScreen.tsx:105` | Gate: `settings.read_aloud ?? true` AND `appConfig.features.readAloud`; parent switch `screens.parent.settings.readAloud` | n/a | Config (default ON) |
| 14 | `platform/audio/web-audio-player.ts`, `sound-map.ts`, `provider.tsx` | 30 sound names, WebM/Opus first, MP3 fallback, Web Audio buffers, per-sound volume | n/a | Not voice (SFX) |
| 15 | `game/narration-text/NarrationText.tsx:22` | `tick` SFX when text arrives; captions "L'histoire arrive", "L'histoire prend un moment", fallback notice | n/a | Not voice |
| 16 | `game/mascot-bubble/MascotBubble.tsx:22` + `lines.mascot.{think,cheer,curious,amazed,encourage,exit}` | Numa bubble text (about 98 `lines.*` strings per language in `i18n/generated/*.json`) | Bubble is silent text plus SFX | Could be voiced; today not spoken |
| 17 | `game/outcome-beat/OutcomeBeat.tsx`, `lines.outcome.{correct,detour,surprise}` | Outcome titles | Silent text plus SFX `beat_*` | Could be voiced; not today |
| 18 | `game/outcome-sim/OutcomeSim.tsx`, `game.outcomeSim.caption.<world>.{good,bad}` | Animation captions | Silent text | Could be voiced; not today |
| 19 | Question screens (`game/question-input/*`), prompts, hints, `common_errors[].feedback`, `explain` | Question text, hint, feedback | Not spoken today (only `sceneText`/`denouementText` are passed to `speak`) | Gap for pre-readers |
| 20 | `scripts/play-all.mjs:26`, `scripts/play-checks.mjs:12` | Stub `SpeechSynthesisUtterance` and `speechSynthesis` for QA runs | n/a | Test harness; must be updated if the engine changes |

### Backend: narrator

`numerix-api/internal/narrator` (`narrator.go`, `canned.go`, `anthropic.go`). Provider facade: LLM (`anthropic.go`) else canned (`canned.go`); falls back when LLM is off, fails, is slow, over budget, or leaks the answer. Phases: `scene` (before the answer) and `denouement` (after; outcome `correct`, `erreur_typique`, `erreur_aleatoire`). SSE read by `data/api/narration.ts:15-45`: events `scene|denouement` (deltas), `fallback`, `done`. Status `ready|fallback`, source `canned|anthropic|cache`. Canned bank: `internal/narrator/data/kit.json`, 8 worlds only (each 2 heroes, 3 titles, 3 openings, 3 ok, 3 typical, 3 surprise lines, with `{h}` hero, `{o}` object slots); other worlds resolve to `nature` (`canned.go:40`). Narration text is therefore generated or templated at runtime and cannot be pre-recorded as-is.

### Product docs

`docs/CLAUDE_CODE_PRODUCT_PROMPT.md:64-67, 304, 336-337, 367, 405`: sound feedback cues only (success, milestone, warning, error), packaged locally, subtle. No requirement for voice, narration, TTS or read aloud. `docs/NUMERIX_CONTEXT.md:338`: narrative engine, two LLM calls per question. Voice behaviour is an implementation decision, not a documented requirement; this audit proposes the requirement.

## 2. Problems

1. Robotic and inconsistent. Quality depends on the device (Windows "natural" voices good, many Android/Linux/Chrome builds poor or absent). Same line sounds different per child. Pitch hack 1.05 does not fix it.
2. Locale mismatch. `voices.ts:12` compares primary subtag only, so `fr-CA` can resolve to a France or Belgian voice (France names in `FEMALE` regex); `en-CA` to en-US/en-GB. Exact locale is only a +4 tie-break. No Quebec voice guarantee, no `en-CA` guarantee.
3. Default ON (`store.ts:22`, `StoryScreen.tsx:105`). Opposite of the target: computer voice must be opt-in by a parent.
4. Captions not guaranteed. `DoneScreen.tsx:78` hides the appreciation caption when voice works. `useSpokenLine` for greeting is fine, but the rule "captions always shown" is not enforced anywhere.
5. Latency and fragility. Speech starts only after the whole LLM scene is complete (`StoryScreen.tsx:205-217`), voices load late (race handled in `useSpeaker.ts:96-101`), autoplay lock queue (`:44-62`), Chrome GC bug workaround (`keep` ref). Cold start = silence.
6. No human fallback and no manifest: if no voice, the child hears nothing and gets a one-time toast (`noVoice`).
7. Coverage gap: question prompts, hints, feedback and mascot lines are never voiced; a Grade 1 pre-reader cannot play without an adult.
8. Number and unit reading is left to the engine (`spoken.ts` only handles operators). Risks: "2,5" and "1 000" reading, "cm", "m2", "$" and "kg", French agreement ("égale"), "70/80/90" depending on voice (fr-FR voices say soixante-dix, but Belgian/Swiss voices may say septante/octante/nonante).
9. Gender and tone in existing strings. Numa is "la chouette" (feminine): `lines.mascot.curious.7` "Je suis curieux", `welcome.2` "Prêt pour..." (also assumes child gender), `encourage.4` "plus fort". Recorded script uses feminine Numa and gender-neutral address to the child.
10. Pressure risk: `hintDelaySec: 30` (`store.ts:22`) is a time trigger. Keep it as an offer only (button highlight), never an auto-spoken hint. Product rule: no timers or pressure.
11. Narrator mismatch: 20 worlds exist (`seed/worlds.json`), canned kit covers 8; the rest fall back to nature story text, so a recorded story-beat set per world would not match the text for 12 worlds unless the kit is extended or the recorded line is world-level and text-independent (proposed below).
12. Tests stub the Web Speech API (`scripts/play-all.mjs`, `play-checks.mjs`); they need a clip-player stub.

## 3. Design: human voice system

### 3.1 Manifest (voice pack)

One JSON per locale, plus a content hash for cache busting. Key = line id (stem shared by fr-CA and en-CA, see `seed/voice/script.json`) + locale.

```json
{
  "schema": 1, "pack": "numa-v1", "locale": "fr-CA", "speaker": "numa",
  "base": "/voice/fr-CA/",
  "lines": {
    "b.correct.03": { "file": "b.correct.03", "ext": ["webm","mp3"], "dur": 1.4, "sha": "ab12", "text": "Tout juste !" },
    "c.num.021":    { "file": "c.num.021", "takes": { "cont": 0.9, "fin": 1.1 }, "dur": 1.1, "sha": "..." }
  }
}
```

- Files: mono, WebM/Opus 32 kbps first, MP3 64 kbps fallback (same pipeline as `public/sounds`, same `canPlayType` check in `web-audio-player.ts:55`). Loudness normalised (target -18 LUFS), peak limit -1 dBFS, 150 ms silence trimmed to 40 ms head and tail.
- `text` is stored in the manifest so the caption can always be drawn from the manifest when i18n has no string, and so a QA tool can diff the manifest against `script.json`.
- Manifest only lists clips that exist (the sourcing agent's `clips.json`); missing id = fallback chain below. Split per world for prompts (`prompts.<world>.json`) so the first screen loads under 5 KB.
- Prompt lines keyed `e.<questionId>` (e.g. `e.animaux.ferme.001`).

### 3.2 Playback component

`<Voice lineId text />` and `useVoice()` in `platform/voice` (replaces `useSpeaker` as the default; `useSpeaker` stays behind the parent setting).

- Engine: Web Audio, reuse `WebAudioPlayer` unlock and decode (`web-audio-player.ts`), plus a separate gain node; SFX duck to 40 percent while a voice clip plays.
- Preload: on screen entry load the next 2 to 3 likely line ids (current question prompt, hint, correct pool, again pool) at low priority (`requestIdleCallback`); keep an LRU of 40 decoded buffers; the service worker caches the manifest and clips (stale-while-revalidate). Skip preload when `navigator.connection.saveData` or effectiveType is `2g/slow-2g`; then fetch only on a user tap.
- Pools: a line request is `pool: "b.correct"`; the player picks a random variant not equal to the last two played (extends `audio/pick.ts`). With 10 correct, 8 again, 7 hint variants repetition is rare.
- Autoplay: first play only after a user gesture (existing unlock logic); until then the caption shows and the speaker button waits.

### 3.3 Fallback chain (per request)

1. Human clip for `id@locale` is in the manifest and loads within 1.5 s: play it.
2. Else: silent. Show the caption (text from i18n or manifest) and nothing else. No error toast, no spinner loop.
3. Else, only if the parent enabled "Read aloud (computer voice)" in settings (default OFF): browser TTS through the existing `useSpeaker`, with the locale-strict picker (reject `fr-FR`/`fr-BE`/`en-US` unless the parent picked that voice by name).
4. Never TTS by default; never TTS for classes a, b, c, d (we have human clips; a missing clip is a recording bug, log it to telemetry as `voice_miss` with id).

Rename the settings: `screens.parent.settings.readAloud` becomes "Read aloud (computer voice)" and default false; add a second child-facing toggle "Numa's voice" (default ON when human clips exist; independent of computer voice). `prefs.voice` default flips to the new meaning.

### 3.4 Pacing, comfort, accessibility

- No timers, no countdowns, no auto-advance because audio ended. Audio never gates the next button. A clip may be tapped away or replaced; starting a new clip cancels the old one (no overlap).
- One voice at a time; at most one unprompted Numa line per question; feedback line plays at most 1.2 s after the answer (not delayed to create tension).
- Hints are spoken only when the child taps Hint (or the hint button, after the optional highlight). No spoken nagging.
- Captions: always rendered when a line plays, for every voiced line (fix `DoneScreen.tsx:78`). Text size and contrast unchanged. Captions appear with the audio start, not after.
- Mute: global "Sound" and separate "Numa's voice" switch (existing `useAudioSettings`); mute cancels the current clip; state persists per child. A speaker button replays the current line.
- Reduced motion: unaffected audio, but no waveform or bounce animations on the speaker button; static icon with a small ring state. Data saver: no preload; clips fetched on tap, tiny (about 4 KB per second).
- Screen readers: the caption is the accessible name; the clip is `aria-hidden`; no auto announcement duplicated with audio (live region politeness off while a clip plays).
- Language switch stops playback (replace `speechSynthesis.cancel()` in `MeScreen.tsx:59` and `clear.ts:22`).

### 3.5 Which content can be human-recorded

| Class | Content | Lines per language | Finished audio (approx) | Feasible? |
|---|---|---|---|---|
| a | Fixed UI and Numa lines (welcome, think, curious, amazed, exit, UI prompts) | 47 | 2.5 min | Yes. Priority P1. |
| b | Feedback (correct 10, again 8, hint 7, encourage 6, streak 3, card 3, chapter pass 6, chapter miss 4, detour 5, surprise 5) | 57 | 3 min | Yes. P1. Repetition rare. |
| c | Number, unit, operator words (composer) | 170 fr, 172 en | 4.6 min | Yes. P1. See 3.6. |
| d | World intros (20) + generic chapter lines (4) + chapter titles (89 quests) | 113 | 3.8 min | Yes. World and generic P1; chapter titles P2. |
| e | Question prompts, seed `questions/*.json` | 1,229 (seed count; brief said 1,253: 24 not in the seed, probably drafts) | about 100 min | Yes but costly; stage it (3.7). |
| f | Narrator story beats | 92 canned (12 generic + 4 per world x 20) | 7 min | Only as canned human lines; LLM text cannot be human (3.8). |

Total in `script.json`: 1,708 lines fr-CA, 1,710 en-CA (3,418 entries). Without prompts: 479 and 481 lines, about 21 min finished audio per language (numbers recorded in two takes).

### 3.6 Spoken-number composer spec

Goal: speak any number up to 999,999 and decimals, money, units and the four operators from a small clip set, natural enough for children, never TTS.

Input: a number string (digits, optional `-`, optional decimal part) plus optional unit. Output: ordered clip list with take choice (`cont` for non-final, `fin` for the last clip of a phrase). Clips are joined gaplessly by scheduling Web Audio buffers back to back with a 15 ms crossfade and 20 to 60 ms natural pause only at `et`, decimal point and unit boundaries.

#### fr-CA

Quebec usage: 70 = **soixante-dix**, 80 = **quatre-vingts**, 90 = **quatre-vingt-dix**. Belgian/Swiss **septante, huitante/octante, nonante** are NOT used. Rules:

- 0 to 16: own words (zéro, un... seize). 17 to 19: dix-sept, dix-huit, dix-neuf.
- 20 to 69: tens (vingt, trente, quarante, cinquante, soixante) + unit; "et un" at 21, 31, 41, 51, 61 (vingt et un); otherwise hyphen (vingt-deux).
- 70 to 79: soixante-dix, soixante et onze, soixante-douze ... soixante-dix-neuf.
- 80: quatre-vingts. 81 to 99: quatre-vingt-un, quatre-vingt-deux ... quatre-vingt-onze ... quatre-vingt-dix-neuf (no "et", no "s").
- 100: cent. 200, 300 ...: "deux cents" if nothing follows, else "deux cent trois". 1000: mille (invariable): "deux mille", "trois mille deux cent cinq". 1,000,000: un million; "deux millions".
- Decimals: virgule ("deux virgule cinq"); percent: "pour cent". Money: "deux dollars cinquante" (2,50 $), ¢ as "cents".
- Operators: plus, moins, fois, divisé par, égale. Fractions: "un demi", "un tiers", "un quart", "trois sur quatre" (for a/b).

Minimal clip set (31): zéro, un, deux, trois, quatre, cinq, six, sept, huit, neuf, dix, onze, douze, treize, quatorze, quinze, seize, vingt, trente, quarante, cinquante, soixante, quatre-vingt, quatre-vingts, cent, cents, mille, million, millions, et, virgule. 17 to 19 and 70 to 99 compose from these. Quality set (recommended, in the script): full clips 0 to 100, each recorded twice (`cont`, `fin`) = 202 takes, plus the atoms for 100 and above and the 20 unit and 18 operator words. The composer prefers the full clip when it exists and falls back to atoms.

#### en-CA

- 0 to 19 own words; tens twenty to ninety, hyphenated with unit ("twenty-one").
- hundred, thousand, million. In maths speech no "and": "one hundred twenty-five". Decimals: "point" then digits ("two point five"). Money: "two dollars and fifty cents" / "two fifty".
- Canadian spelling in captions: colour, centre, metre, litre; "zero" (not "nil").

Minimal clip set (33): zero ... nineteen (20), twenty, thirty, forty, fifty, sixty, seventy, eighty, ninety, hundred, thousand, million, and, point. Quality set: full clips 0 to 100 in two takes, plus atoms.

Fallback: if any clip in the sequence is missing the whole number is dropped (never half a number) and the caption shows the digits; computer voice only if the parent enabled it.

### 3.7 Question prompts: size, hours, cost, plan

Seed stats (1,229 questions, avg 10.3 words fr, 9.8 en; Grade 1: 331, G2: 408, G3: 347, G4: 143):

- Finished audio at a child-friendly 2.2 words per second plus pauses: about 104 min fr, 99 min en (about 1.7 h each).
- Studio time with retakes, per language: about 5 to 6 h (3x finished audio), 2 days of two sessions; both languages about 10 to 12 h. Actors can record from 30-line batches from `script.json`.
- Cost, human voice actor, Canadian market, all-in (talent, direction, edit, mastering), rough planning ranges not quotes: about CAD 1,500 to 3,000 per language for all prompts; about CAD 400 to 900 per language for classes a, b, c, d, f (21 min, 479 lines, but minimum session fees apply). Total programme about CAD 4,000 to 8,000 for both languages. Get 2 quotes; confirm buyout and AI-training clauses in the contract (no cloning or synthetic derivation without a separate agreement).

Staging (tier in `priority`):

| Tier | Content | Count per language | Approx audio |
|---|---|---|---|
| P1 | classes a, b, c, d (worlds, generic), f generic | about 310 | about 12 min |
| P2 | per-world story lines, chapter titles | 169 | about 7 min |
| P3 | Grade 1 prompts of 14 words or fewer | 269 | about 20 min |
| P4 | Grade 2 prompts of 14 words or fewer | 344 | about 24 min |
| P5 | Grade 3 prompts of 14 words or fewer | 292 | about 20 min |
| P6 | Grade 4 and all longer prompts (over 14 words) | 324 | about 38 min |

Recommendation: record P1 to P3 first (pre-readers matter most, Grade 1 is the audience that cannot read the prompt). Ship P4 and P5 next. P6 stays caption-only until funded. Question types matter little for the audio (numeric, choice, word, true/false, order, match, visual, estimate); `visual` prompts are shortest. Hints, explanations and per-error feedback are about 22,000 words per language across 1,229 questions: do not record; use the generic hint/again/correct pools instead.

Note: prompts contain digits ("6 poules"). The actor reads them as words; the script keeps the digit form for the printed text. Generated (LLM) questions on the pending flow have no recording and are caption-only.

### 3.8 Narrator story beats (class f)

LLM scene and denouement text is generated per session; it cannot be human-recorded. Options, in order of preference:

1. Silent captions by default (today's text stream), plus a recorded "Numa lead-in" line per world and phase that is independent of the generated text: opening (before the scene), correct, typical error, surprise (before the denouement). Script: 4 lines per world x 20 worlds + 12 generic = 92 lines per language (7 min). Lead-ins are written to be true for any scene in that world and hero-neutral (no names, no objects), so they never contradict the generated text and also work for worlds without a canned kit.
2. For `source: canned` text (templated), the kit has only 8 worlds with 2 heroes, 3 openings each; recording full templates would need hero and object variants, rejected as poor value.
3. Computer voice for the generated body only if the parent enabled it (existing code path, strict locale picker).

Engine rule: lead-in plays when the phase starts; the caption streams in; the generated body is not spoken unless step 3 applies.

### 3.9 Build order (frontend, after the branches merge)

1. `platform/voice`: manifest loader, `useVoice`, fallback chain, caption contract, telemetry `voice_miss`.
2. Swap call sites in table rows 5 to 10 (add rows 16 to 19 incrementally), flip defaults, rename settings.
3. Composer and tests (`fr`: 21, 70, 71, 80, 81, 90, 91, 99, 100, 200, 201, 1000, 1990; `en`: 21, 100, 125, 2.5).
4. Update QA scripts to stub the clip player instead of `speechSynthesis`.
5. Voice pack CI check: every id in `script.json` priority P1 has a clip for both locales; manifest hash matches files.

## 4. Gaps and risks

- Recording talent, studio and contracts not arranged; no clips exist yet (another agent owns sourcing in `tools/voice`, `seed/voice/clips.json`).
- Existing i18n lines with masculine forms for Numa (see Problems 9) need fixing so captions equal the recorded text; the script uses neutral or feminine forms. Caption text must always equal the recording, so recording drives the i18n update for classes a and b.
- Kit covers 8 of 20 worlds; extend kit or accept generic fallback text.
- fr-CA phrasing was written to OQLF-style Quebec usage and checked for France-only forms, but needs a Quebec native review before the studio day. en-CA likewise.
- Unverified: actual runtime behaviour in browsers (no run); `appConfig.features.readAloud` default; where `lines.*` i18n source files live (only the generated JSON was read).
