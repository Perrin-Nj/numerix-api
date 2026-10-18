# Load test

Tool: `cmd/loadtest` (stdlib only). Open-loop generator: requests start on a clock at the target rate; each virtual kid
(one family each) loops start session, next beat, answer a random choice. Every 10th request is `GET /v1/health`.

## Hardware (shared Windows 11 dev laptop, other apps running, host CPU 25-40% busy)
12 logical CPUs, 15.7 GB RAM, SQLite file on a OneDrive folder, server and generator on the same machine.

## Commands
    # server (kids come from fake IPs through X-Forwarded-For, so the per-IP limits are not the thing measured)
    PORT=8090 DB_PATH=./data/load.db APP_ENV=development FEATURE_WELCOME_GIFT=false TRUST_PROXY=true PPROF=true ./bin/api.exe
    go build -o bin/loadtest.exe ./cmd/loadtest
    ./bin/loadtest.exe -rps 50  -duration 60s -families 300
    ./bin/loadtest.exe -rps 200 -duration 30s -families 400
    ./bin/loadtest.exe -stages "20:5s,300:20s,20:5s" -families 400      # spike
Knobs added: `RATE_LIMIT_IP_PER_MIN` (default 600, contract value), `PPROF=true` (dev only, refused in production,
serves `/debug/pprof/`). The 5-registers-per-hour IP limit is why `TRUST_PROXY=true` plus a distinct
`X-Forwarded-For` per family is needed.

## Fixes made
- `internal/store`: read pool kept only 2 idle connections (database/sql default), so bursts closed and reopened SQLite
  files over and over (goroutine dump showed 8 goroutines in `CreateFileW`); now all read connections stay idle-open.
- `internal/auth`: argon2 concurrency capped at NumCPU/2 (each run is 19 MiB and a full core).

## Results (client-side latency, same machine)
Zero 5xx and zero `database is locked` in every run. Server log: 0 errors.

| Scenario | answer p95 | nextBeat p95 | startSession p95 | health p95 | 429 | verdict |
|---|---|---|---|---|---|---|
| 50 rps x 60 s (best run, default GOMAXPROCS) | 3.1 ms | 92 ms | 1.06 s | 24 ms | 0 | start fails |
| 50 rps x 60 s (worst run) | 883 ms | 1.04 s | 1.5 s | 501 ms | 0 | fails |
| 200 rps x 30 s (GOMAXPROCS=6) | 294 ms | 1.38 s | 1.8 ms | 63 ms | 44 | beats fail |
| spike 20 -> 300 -> 20 rps | 1.5 s | 725 ms | 4 ms | 109 ms | 176 | fails |

Targets (answer <400 ms, beats <800 ms, health <100 ms) are NOT reliably met on this machine.

## Reading the numbers
- p50 is 1-3 ms everywhere. Failures are multi-second pauses: the generator's own watchdog (a 10 ms sleeper) woke
  1.0-1.7 s late in most runs, and new TCP connects to localhost took up to 1.5 s, while `GET /health` (no database) was
  hit too. The pauses hit client and server at once, so they are host scheduling stalls, not a database queue.
- Server-side `duration_ms` in the access log during the load phase: p50 2 ms, p95 4 ms, p99 270 ms (one 50 rps run).
- 429s at 200+ rps come from `MAX_CONCURRENT_REQUESTS=256` after a stall piles up requests (by design) and from
  30/min per-session limits when kids are few.

## Remaining risk
Re-run on a quiet Linux host or CI box for a trustworthy verdict. SQLite has one writer (nextBeat and answer both write);
expect that to be the first real limit above a few hundred rps. Single-process rate limiter state.
