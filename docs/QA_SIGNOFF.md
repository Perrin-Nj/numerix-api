# QA sign-off: MVP1 (child and parent flows)

Date: 2026-10-21
Verdict: **SIGNED OFF for MVP1 child and parent flows, staging only.** Not yet cleared for production.

## Scope
Child play (kid door, PIN, chapters, feedback, feed, cards, map, music and sound assets) and parent flow (register, create child, consent, press Play, session expiry). Admin and other flows: load checks only.

## Evidence
Run on `main` (frontend 0777c19 + test fix, API 7de8f1d), fresh API binary, production web build, ports 18400/13400.

| Check | Result |
|---|---|
| `test.sh ui` | all pass, 0 console errors (3 runs; 2 earlier runs had load flakes) |
| `test.sh play` | 9 of 9 pass (twice) |
| `test.sh flows` | all pass |
| `test.sh feed` | all pass except QA-B-001 below |
| `check-music`, `check-sounds` | pass |
| API `go build`, `go vet`, `go test ./...` | pass |
| Frontend `typecheck`, `lint` | pass (warnings only) |

Correction: the first sign-off run (earlier today) used a stale `bin/api.exe` (the QA runner silently keeps an old binary when `rm` fails on OneDrive). Everything above was rerun with the binary deleted first. If a run shows `404 /api/v1/config/public`, delete `Services/numerix-api/bin/api.exe` and rerun.

## Known issues (not blocking staging)
- QA-B-001 P1 (cosmetic): `feed` suite, one item can repeat inside one 200-item open (`m-ladybug`, earlier `m-chessboard`). Fix before prod.
- QA-V-001: no human voice clips shipped; computer voice is off by default. Owner decision, deferred.
- Flows suite: after the paid/unblock steps a chapter may already be open (409 `session_active`) on a reused database; rerun passes.
- UI suite is timing-sensitive on a busy machine (feed growth, idle coach hint); rerun before judging.
- Removed stale ui check "story read aloud by French woman voice" (QA-T-002): computer voice is off by default by design.
- Not run: load, `go test -count=3`, physics scenes by eye, admin screens by hand, `admin-propagation` (stale), `first-run` and `feed-shuffle` modes (the QA wrapper names them but the frontend runner has no such mode).
