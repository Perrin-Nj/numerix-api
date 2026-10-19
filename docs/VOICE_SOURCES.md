# Voice sources: real human voices for Numerix (fr-CA, en-CA)

Goal: replace AI/TTS with real human speech. Status 2026-10-19. Not legal advice; a Quebec lawyer must validate the final licence and release texts.

## 1. What was gathered now

Tool: `tools/voice/fetch.mjs` (idempotent; `cd tools/voice && npm i && node fetch.mjs [--force] [--no-fallback]`). Output: `seed/voice/clips.json` + `seed/voice/files/*.m4a` (AAC-LC 64 kbps mono 48 kHz, silence trimmed both ends, -16 LUFS, limiter -2 dBFS), `misses.txt`, `qa-failed.json`. ffmpeg: not on PATH here, so `ffmpeg-static@5.3.0` (published 2025-11-14) is pinned in `tools/voice/package.json`.

Source: Lingua Libre recordings on Wikimedia Commons, found by querying Lingua Libre's SPARQL endpoint for speakers whose self-declared residence is in Canada (Wikidata), then checking each candidate file's licence template on Commons. Only CC0 / CC BY / CC BY-SA files are accepted.

Result: 365 clips, 2.3 MB (budget 25 MB), 13 more rejected by QA. QA (ffmpeg, no listening possible): duration 0.15 to 4 s, mean level above -40 dB, output peak below -0.3 dBFS, source clipping (>3 samples at 0 dBFS). 365 of 378 pass (96.6%); rejected: 6 output peak, 7 clipped sources.

| Lang | Locale label | Speaker residence | Clips | Licence mix |
|---|---|---|---|---|
| fr | fr-CA (tier canada) | Québec (self-declared) | 103 | 88 CC0, 14 CC BY-SA 4.0, 1 CC BY 4.0 |
| fr | fr-FR (tier fallback) | Lyon, Vendée, southern France | 95 | CC BY-SA 4.0 |
| en | en-CA (tier canada) | Ontario 1, British Columbia 1 | 2 | CC0, CC BY 4.0 |
| en | en-GB (fallback) | London | 139 | CC BY-SA 4.0 |
| en | en-US (fallback) | Texas, California | 26 | CC0 |

Honesty notes:
- `locale: fr-CA` means "speaker lists a Canadian residence on Lingua Libre". The accent is not verified. `speakerRegion` and `regionSource` are stored per clip.
- Fallback clips carry their real country locale (fr-FR, en-GB, en-US). They must not be shipped as fr-CA / en-CA without an explicit product decision. They mix voices: most fr-CA numbers 0 to 50 are one speaker (Estrie, 82 clips, CC0), numbers 51 to 100 come from a fr-FR speaker.
- English Canadian human clips from public sources are essentially absent (2 clips). English Canadian speakers on Lingua Libre mostly recorded place names.

### Coverage of the Numerix vocabulary (209 keys per language)

Vocabulary: numbers 0 to 100 plus 200 to 900 and 1000, plus, minus, times, equals, divided, fraction words, days, months, shapes, colours, praise words, letters a to z.

| Category (keys) | fr-CA tier | fr any | en-CA tier | en any |
|---|---|---|---|---|
| numbers (110) | 54 | 108 | 1 | 97 |
| operators (5) | 2 | 5 | 0 | 4 |
| fractions (6) | 1 | 6 | 0 | 4 |
| days (7) | 3 | 5 | 0 | 7 |
| months (12) | 2 | 12 | 0 | 12 |
| shapes (13) | 3 | 10 | 0 | 7 |
| colours (11) | 8 | 11 | 1 | 9 |
| praise/feedback (19) | 4 | 15 | 0 | 15 |
| letters (26) | 26 | 26 | 0 | 12 |
| Total (209) | 103 = 49% | 198 = 95% | 2 = 1% | 167 = 80% |

Still missing in French (any accent): rectangle, losange, ovale, bravo, essaie encore, bien joué. Missing in English: 200 to 900 except 500, equals, tenth, fraction, several shapes, purple, try again, well done, keep going, letters b f g j k n p q r u v w x z. `misses.txt` has the exact list.

## 2. Source verdicts

"Verified" = I fetched the page this session. Items marked (unverified) returned 403/empty or only a search snippet.

| Source | Verdict | Notes |
|---|---|---|
| Lingua Libre / Commons, CC0 files | Usable | No attribution required; credit recommended. |
| Lingua Libre / Commons, CC BY 4.0 | Usable with attribution | Name speaker, licence, link, say if modified. |
| Lingua Libre / Commons, CC BY-SA 4.0 | Usable with attribution + SA on the clip | See SA analysis below. |
| Mozilla Common Voice (CC0) | Usable but not downloaded | Sentence-level crowd recordings; CC0. See below. |
| LibriVox | Usable (public domain per LibriVox, US law) | Long book readings in fr/en; wrong style and wrong content for word clips. |
| Freesound CC0 | Usable per file | Almost no clean spoken words for our vocabulary; CC BY needs credit, CC BY-NC not usable. |
| Forvo | Not usable | 403 on terms page (unverified); secondary sources say CC BY-NC-SA was replaced by a restrictive ad hoc licence and API is paid. Do not scrape. |
| Government of Canada open data (OGL-Canada 2.0) | Usable licence, no suitable audio found | Commercial use allowed with attribution; excludes personal information and third-party rights. I found no kid-suitable word audio. |
| Internet Archive | Per item only | Terms page returned no usable text (unverified); each item has its own licence. No bulk source of our words. |
| Spoken Wikipedia | Not useful | Long article readings; licence of audio not stated on the project page I fetched. |

### Lingua Libre, ShareAlike in practice
- Lingua Libre files are CC BY-SA 4.0 by default on Commons (template `cc-by-sa-4.0`), but some speakers publish CC0. The tool reads the licence from each file page.
- CC BY-SA 4.0: attribution (creator, licence link, indicate changes) and ShareAlike applies to "Adapted Material", i.e. when you modify the clip. The legal code defines audio as adapted when altered, or when synched with moving image. Our pipeline trims, normalises and re-encodes, so treat each processed CC BY-SA clip as an adaptation: publish that clip under CC BY-SA 4.0 and credit the speaker. The SA does not extend to the app code or to other clips; the CC deed and legal code speak only of Adapted Material, and CC's FAQ says CC licences are not recommended for software. A lawyer should confirm the "collection vs adaptation" boundary for a game that plays clips on demand.
- Safer option: prefer CC0 clips (88 of 103 fr-CA tier clips are CC0), ship unmodified CC BY-SA files if you want no adaptation claim, and show a credits screen with the `attribution` string from `clips.json`.
- Commons says to verify the licence and non-copyright restrictions (personality rights) yourself; the Foundation gives no warranty.

### Mozilla Common Voice
- Licence CC0 (Mozilla 2019 announcement). Accent, age, gender are opt-in metadata; accent labels such as Canadian or Quebec French exist but I could not verify names or sizes.
- Recent Mozilla Data Collective listings say CC0-1.0 but add restrictions: no speaker re-identification, no re-hosting of the dataset. Read the terms at download time.
- Content is read sentences from a public-domain corpus, not our words. Numbers appear only incidentally. It could be mined for words by forced alignment, but audio quality is uneven, voices are adults of every kind, and it is a research dataset, not a curated children's voice. Not worth it for the owl; maybe a later fallback for rare words.

## 3. What public clips cannot cover (honest gaps)
- Full sentences, question read-aloud, story text, hints.
- The owl character: personality lines, reactions, any consistent persona.
- Consistent voice across the whole app: public clips are a patchwork of speakers, volumes and rooms.
- Canadian English at all.
- Many Quebec words beyond numbers 0 to 50 and letters.

## 4. Paid / commissioned human voices

Prices are USD market guides, not quotes.

| Item | Range | Source |
|---|---|---|
| E-learning narration | $30 to $55 per finished minute, $0.20 to $0.35 per word, minimum $350 to $450 | [Voice Crafters / GVAA guide](https://www.voicecrafters.com/?p=140657) |
| E-learning educational (toys/games section) | $350 to $400 up to 5 min, $600 up to 10 min, up to 3 voices | same |
| Mobile game apps, full buyout | $300 to $500 per game (1 h max), or $200 to $300 per hour (2 h minimum) | same |
| Video games, full buyout | $200 to $350 per hour, 2 to 4 h minimum | same |
| Marketplace low end | $4 to $55 per finished minute; about 150 words per finished minute | [eLearning Art](https://elearningart.com/community/free/narration/), [ProZ](https://www2.proz.com/forum/voiceover/330281-guidelines_for_quoting_for_voiceover_projects.html) |
| Marketplace e-learning | $0.10 to $0.25 per word; buyout = one-time, unlimited, perpetual; time-limited licences renew | [VoiceBros 2026](https://voxbooster.com/blog/voiceover-rates-statistics-2026/) |
| ACTRA NCA 2024-25 | voice-over session 665 (4 h) per rate-chart snippet; I could not read the PDF (unverified) | [ACTRA rates PDF](https://actra.ca/wp-content/uploads/2024/06/NCA-Rates_2024-Jun-12-to-2025-May-31.pdf) |

Marketplaces: [Voice123](https://www.voice123.com/) (verified: post a job, auditions, escrow, rates depend on usage rights; fee not stated); Voices.com, Fiverr, Bodalgo, UDA site (all returned 403/404 here: unverified). For Quebec French: ask UDA-member studios and agents directly; the UDA site could not be fetched, so union tariffs are not verified. If you hire a union member you must work under the UDA (Québec) or ACTRA agreement, which adds session fees, residuals and pension contributions; non-union talent is paid by negotiated buyout. Check with each union before the casting call.

### Recommended brief
- Two talents: Quebec French (fr-CA) and Canadian English; warm adult, 25 to 40, friendly teacher energy; one separate owl character voice per language, same actor if possible.
- Scope per language: ~210 core words/numbers/letters; ~150 feedback and owl lines (about 1,200 words, 8 to 10 finished minutes); optional question read-aloud later (use TTS only as a bridge).
- Delivery: WAV 48 kHz 24-bit mono, dry (no reverb or compression), 3 takes, room tone, one file per line named by id from a spreadsheet; pick-ups for 1 revision round included.
- Rights: perpetual, worldwide, unlimited plays, all platforms and versions of Numerix (app, web, marketing), including editing (cut, level, pitch-neutral clean-up); explicitly no AI training and no synthetic voice cloning without a new written consent.
- Written release (Loi 25): separate, plain-language consent to collect and use the voice; purposes, retention, who has access, right to withdraw; parent consent if under 14 ([Quebec.ca](https://www.quebec.ca/nouvelles/actualites/details/loi-25-sur-la-protection-des-renseignements-personnels-des-citoyens-du-quebec-entree-en-vigueur-de-nouvelles-dispositions-qui-font-du-quebec-un-chef-de-file-mondial-50726)). A voice that identifies a person is personal information; credit with name only with consent.

### Budget estimate (USD, per language, non-union buyout)
- Core set (30 to 45 min of speech incl. retakes): $400 to $900.
- Owl + feedback (8 to 10 finished min): $350 to $600 at e-learning educational rate, up to $1,000 with character work and a studio.
- Direction / editing / QA: $200 to $500 if outsourced.
- Per language about $950 to $2,400; both languages about $1,900 to $4,800. Union scale adds roughly 30 to 60% (session fees, pension), to be confirmed with UDA/ACTRA.

## 5. Recommended plan
1. Now: use `seed/voice` fr-CA CC0 clips (numbers 0 to 50, letters, a few words) as placeholders; keep fallback clips flagged and off by default; keep TTS as the base for everything else.
2. Weeks 1 to 3: volunteers (family, with signed Loi 25 releases) record the core set from `docs/VOICE_RECORDING_GUIDE.md` using a small recorder page (prompt list from `clips.json` keys, 3 takes, upload WAV, then run the same `convert` pipeline). Target: fr-CA numbers 51 to 100, 200 to 1000, operators, fractions, shapes, praise, plus the en-CA set.
3. Weeks 3 to 6: commission one fr-CA and one en-CA voice actor for the owl and all feedback/story lines under a perpetual buyout; replace volunteer clips where quality is lower.
4. Show a credits screen from `attribution` for every CC BY / CC BY-SA clip still shipped; drop them as they are replaced.

## 6. Source URLs checked
1. https://commons.wikimedia.org/wiki/Category:Lingua_Libre_pronunciation-fra (fetched)
2. https://commons.wikimedia.org/w/api.php (queried; licence templates and file URLs)
3. https://lingualibre.org/bigdata/namespace/wdq/sparql (queried; speakers, residence, records)
4. https://query.wikidata.org/sparql (queried; Canada / Québec locations)
5. https://creativecommons.org/licenses/by-sa/4.0/ (fetched)
6. https://creativecommons.org/licenses/by-sa/4.0/legalcode (fetched)
7. https://creativecommons.org/faq/ (fetched)
8. https://commons.wikimedia.org/wiki/Commons:Reusing_content_outside_Wikimedia (fetched)
9. https://librivox.org/pages/public-domain/ (fetched)
10. https://freesound.org/help/faq/ (fetched)
11. https://open.canada.ca/en/open-government-licence-canada (fetched)
12. https://en.wikipedia.org/wiki/Wikipedia:WikiProject_Spoken_Wikipedia (fetched)
13. https://commonvoice.mozilla.org/en/about (fetched, thin) and https://blog.mozilla.org/blog/2019/02/28/sharing-our-common-voices-mozilla-releases-the-largest-to-date-public-domain-transcribed-voice-dataset/ (search result)
14. https://www.voicecrafters.com/?p=140657 (fetched)
15. https://www.voice123.com/ (fetched)
16. https://www.quebec.ca/nouvelles/actualites/details/loi-25-sur-la-protection-des-renseignements-personnels-des-citoyens-du-quebec-entree-en-vigueur-de-nouvelles-dispositions-qui-font-du-quebec-un-chef-de-file-mondial-50726 (fetched)
17. https://www.suchavoice.com/?p=15228 (search result, kids app voicing)
18. https://elearningart.com/community/free/narration/ and https://voxbooster.com/blog/voiceover-rates-statistics-2026/ (search results)
19. https://actra.ca/wp-content/uploads/2024/06/NCA-Rates_2024-Jun-12-to-2025-May-31.pdf (binary, not parsed)
20. https://en.wikipedia.org/wiki/Forvo (search result; forvo.com terms 403)
Failed (403/404): forvo.com/terms-of-use, voices.com/pricing, site.uda.ca, fiverr terms, bodalgo pricing, aes.org loudness PDF. Recording-guide numbers (48 kHz/24-bit, -16 LUFS, -1.5 dBTP) are common industry practice, not verified against a standard here.
