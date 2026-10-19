# Assets (phase P3)

Photos and sprites for the 20 worlds. Output is read by `internal/assetstore` (`LoadSeed`).

## Run

```
cd tools/assets && npm ci            # sharp 0.34.5 (pinned)
node tools/assets/fetch.mjs --dry-run  # lists what is not pinned yet, no network
node tools/assets/fetch.mjs            # fetch, process, emit seed/assets/*.json
node tools/assets/fetch.mjs --only ocean   # one world
node tools/assets/sheet.mjs            # contact sheets: tools/assets/.cache/sheets/*.png (not committed)
```

Flags: `--photos-only`, `--sprites-only`, `--no-verify` (skip licence page check).

## Keys

| Variable | Needed | Effect |
|---|---|---|
| `PEXELS_API_KEY` | optional (free) | Photos pinned to Openverse/Commons are re-searched on Pexels and replaced when a match is found (licence `PEXELS`, credit "Photo by X on Pexels"). Pexels photos need `creator_url` (always set). |
| Openverse, Commons, Fluent | no | Anonymous. Openverse allows 20 requests/min and 200/day: responses are cached in `tools/assets/.cache` (git-ignored), so a re-run costs nothing. |

Without the key every world still has 1 hero + 4 to 5 scenes. Most visible gain from Pexels: brighter, sharper, higher-resolution scenes (several Openverse picks are 1024 px wide, so no 1600 variant) and the slots left at 5 photos.

## Files

- `tools/assets/manifest.json`: per world 1 hero + 5 scene queries (`q1;q2|topic|objects|mood`; `commons:<title>` forces a Commons title search) and 75 Fluent sprites.
- `seed/assets/<world>.json`, `sprites.json`: asset lists (columns of table `assets`, plus `tags[]`, `variants[]`, `role`).
- `seed/assets/files/<id>-<w>.webp`: hero 480/960/1600 (16:9), scenes 480/960 (4:3), sprites 256 with alpha. Quality adapts so the 960 variant stays under 120 KB (hero) or 110 KB (scene). LQIP is a 24 px WebP data URI in each entry.
- `seed/assets/picks.json`: pinned choice per slot (idempotence; re-run changes nothing). `seed/assets/rejected.json`: rejected `source:id` and `title:` keys with a reason; a rejected pick is dropped on the next run and the next best candidate is taken.
- `tools/assets/reviews.json`: visual review result per asset id (status, `alt_fr`, `alt_en`). A photo without a review stays `pending` and gets a draft English alt.
- `seed/assets/credits.json`: credits page data (`photos[]` each with attribution, creator, source, licence; `groups[]` for the Fluent MIT notice).

Id = `a_` + first 20 hex of sha256 of the downloaded bytes.

## Licence rules

Accepted: CC0, PDM, CC BY 2.0/3.0/4.0 (attribution recorded), Pexels licence, MIT (Fluent Emoji 3D, copyright Microsoft Corporation). Anything else (SA, NC, ND, GFDL, "or" multi-licences, unknown) is dropped by the script. The script fetches each licence page once per run and logs the HTTP status. Filters on title and tags: people, brands, weapons, gambling, alcohol; Openverse `mature` and sensitivity flags; landscape only. Everything left was inspected on contact sheets.

## Review log (this run)

- 118 photos kept, 55 candidates rejected and replaced (people or faces, brand names or toys, text signs, B&W or dark, engravings instead of photos, off-topic).
- Open gaps: `sport` and `arts` have 5 photos (hero + 4 scenes); the hockey and kaleidoscope slots found nothing usable.
- Alt text is factual, fr-CA and en-CA, never states a count or an answer.
- Mixed quality remains (some Openverse picks are 1024 px, a few are dim or plain). Upgrade with the Pexels key, or drop a pick into `rejected.json` and re-run.

## Credits page

Show every `photos[]` line with CC BY licence (required) and, for completeness, the others; plus the `groups[]` MIT notice: "Fluent Emoji by Microsoft, MIT. Copyright (c) Microsoft Corporation." with a link to the licence. Pexels photos need a visible link to Pexels.

## Real-object photos (counting and measuring)

The 75 Fluent 3D emoji sprites are retired: `sprites.json` and their files are gone from the seed (kept only as `tools/assets/sprites-legacy.json`, id and tags, to map old question media to the new photos). Counting objects are real photographs, Wikimedia Commons only (CC0, public domain, CC BY 2.0 to 4.0).

```
node tools/assets/objects.mjs                  # search, cut out, square, WebP 256 and 512, seed/assets/objects.json
node tools/assets/objects.mjs --redo a,b       # reject the current pick of slugs a and b (objects-reject.json) and take the next
node tools/assets/rewire-objects.mjs           # question media: sprite -> matching object photo, scene photo for questions without one
node tools/assets/credits.mjs                  # rebuild seed/assets/credits.json (every asset, licence, alt fr and en)
```

A photo with a plain bright border is flood-filled to transparent and centred with 8 percent padding (cutout); any other becomes a square crop shown as a soft rounded tile. `objects-queries.json` holds the search terms per object, `objects-map.json` the chosen file per slug (source page, licence, creator). Titles that name people, brands, weapons, artwork, drawings or museum catalogue items are refused. Every pick was checked on a contact sheet.
