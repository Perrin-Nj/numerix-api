# Discover feed media (200 curated items)

The Discover feed shows 200 real-world items in a private random order: 110 photos, 40 animations, 50 short captioned videos.
Every item ties to maths in real life or to a wonder fact, in fr-CA and en-CA, with a source, a creator and a licence.
Nothing is generated, nothing is cartoon or emoji art, nothing links out of the app.

## Numbers

| Kind | Items | Main file size (min / avg / max) | Total with posters, small variants, captions |
|---|---|---|---|
| photo (WebP 960 x 720 and 480 x 360) | 110 | 3 / 45 / 137 KB | 6.4 MB |
| animation (H.264 MP4, silent loop, 6 to 12 s) | 40 | 23 / 177 / 325 KB | 7.2 MB |
| video (H.264 MP4 480p, 5 to 20 s, WebVTT fr-CA + en-CA) | 50 | 153 / 596 / 803 KB | 30.3 MB |
| Total | 200 | | 43.9 MB in 500 files (budget 60 MB) |

Licence mix: CC0 134, public domain 37, CC BY 29 (the credit line on the card is the attribution). No SA, NC or ND.
Source mix: Wikimedia Commons 90 (all videos and animations), Openverse 107 (CC0 photos indexed from StockSnap 53,
rawpixel 52, WordPress Photo Directory 2), NASA Image Library 3. 116 items carry a tap-to-reveal question.

## Sources and licence pages (all fetched on 2026-10-19)

1. Wikimedia Commons, what is accepted: https://commons.wikimedia.org/wiki/Commons:Licensing (CC0, CC BY, CC BY-SA, public domain; NC and ND refused). We keep only CC0, public domain and CC BY.
2. Commons, reuse rules: https://commons.wikimedia.org/wiki/Commons:Reusing_content_outside_Wikimedia (credit the creator for CC BY, name the licence).
3. Creative Commons CC0 1.0: https://creativecommons.org/publicdomain/zero/1.0/
4. Creative Commons CC BY 4.0: https://creativecommons.org/licenses/by/4.0/ (credit, licence link, say if changed; we resized and cut clips, the credit line says so by naming the original).
5. Openverse API (keyless, CC and public domain only): https://docs.openverse.org/api/reference/made_with_ov.html ; endpoint `https://api.openverse.org/v1/images/?license=cc0,pdm,by`.
6. NASA media usage guidelines: https://www.nasa.gov/nasa-brand-center/images-and-media/ (generally not copyrighted, credit NASA, no endorsement, third-party material excluded). Image API: https://images-api.nasa.gov . Many NASA clips are also on Commons as public domain.
7. Pexels licence: https://www.pexels.com/license/ (no attribution required) and 8. Pixabay licence summary: https://pixabay.com/service/license-summary/ . Read and judged compatible, but not used: their video APIs need a key (`PEXELS_API_KEY`, `PIXABAY_API_KEY`, both empty here) and the picks from Commons and Openverse covered every topic.
9. Internet Archive terms https://archive.org/about/terms.php and 10. Smithsonian Open Access https://www.si.edu/openaccess : the pages are rendered by script (Archive) or refused the fetch (Smithsonian, HTTP 403), so no licence statement could be verified. They are NOT used.
11. YouTube: no download, no embed. The few Commons clips that were first on YouTube were re-hosted on Commons under CC BY and are self-hosted here.

## What a child sees

- Photo: full-bleed 4:3 image with a blur-up placeholder, title, a one or two sentence fact, an optional question ("Voir la réponse" reveals the answer and owl Numa cheers), like and save, credit line.
- Animation: silent loop, plays only while on screen.
- Video: muted until the child taps the sound button (shown only when the clip has a track: 3 clips), captions on by default and a toggle, pause when scrolled away. Captions are descriptive (title, then the fact) because the clips are mostly silent nature footage.
- Reduced motion or data saver: the poster only, a 56 px play button starts the clip.

## Review log (contact sheets and frames read by hand)

- Photos: the first automatic picks (Commons text search) were about half wrong; the pipeline moved to Openverse and NASA, then every photo was inspected on contact sheets, and 11 topics were replaced (engravings, drawings, brand names, crowded or off-topic photos). Texts never state a count that depends on a detail of the photo.
- Videos: 21 of the first 54 clips rejected (dark, text overlays and logos, crowds, mould, a CGI toy, blurry, black bars), replaced from a second and a third round.
- Animations: 19 rejected (English text overlays, dark, unreadable maps) and one AI-script animation (bubble sort, its credit says "AI-generated python script") replaced.
- Facts were written from well known figures (spider 8 legs, hexagon 6 sides, 71 % ocean, 384 000 km to the Moon, 88-day Mercury year). Questions are generic ("5 rows of 8"), never tied to what the photo shows.

## Pipeline (tools/feed)

```
cd tools/feed && npm ci                 # ffmpeg-static 5.2.0, ffprobe-static 3.1.0, sharp 0.34.5 (all older than 2 weeks)
node photos.mjs                          # Openverse / NASA picks -> work/photos.json
node clips.mjs videos | anims            # Commons download (480p transcode when large), cut, H.264 under a size budget
node framesheet.mjs <name> <ids>         # contact sheets of frames, to review by eye
node build.mjs                           # seed/feed-media/<id>-<hash8>.<ext> and seed/feed/items.json
go run ./cmd/feedseed --validate-only    # texts, dashes, licences, files, captions
```

`work/` and `.cache/` are git-ignored. File names carry the first 8 hex of the content hash, so URLs are immutable.

## Serving and storage

- Items: table `feed_media` (migration 0010), loaded by `cmd/feedseed` (idempotent, deletes removed items).
- Files: on disk in `MEDIA_DIR` (default `./seed/feed-media`), served by `GET /v1/feed-media/{file}` with a strong ETag, `Cache-Control: public, max-age=31536000, immutable`, `Accept-Ranges` and Range (206, 416) so a video starts before it is downloaded. Videos are not stored as SQLite blobs: blobs would bloat the database file that Litestream replicates.
- Backup: the media directory is static and rebuildable (`tools/feed`), but back it up with the release: copy `MEDIA_DIR` to the same object store as the database backups (see DEPLOY_ORACLE.md) and keep it in the release checklist.

## Order and paging

`GET /children/{childId}/discover?cursor&limit` (page of 12). Position `p` is a puzzle when `p % 8 == 7` (an existing feed puzzle, graded by `answerFeedPuzzle`), else the next media item of a per-child seeded shuffle of the items whose `grade_min <= grade + 1`. A pass shows every eligible item once; then a new shuffle (`cycle` + 1) whose first item differs from the last one shown. Videos are spread: never more than two in a row, never first or last of a pass. The cursor is `base64url(pos.crc)`, bound to the child; a forged one is a 422. No per-child state is stored: likes and saves reuse `feed_reactions` (ids `m-<slug>`).

## Not done

Pull to refresh, a views event in the Mongo outbox, and a credits page that lists the 29 CC BY credits with links (the credit text is on each card; links are kept out of the child area on purpose).
