# Buffer stress & chaos harness

A **black-box** stress/chaos harness for core-api **buffers**. It speaks only the
public HTTP API + a controllable sink — zero imports of `internal/` — so it
survives the architecture churn underneath. Built to grow into the final
SLA-gate step of CI/CD.

## What it proves

| Spec | Promise under test | Gate |
|---|---|---|
| `tracer.js` | end-to-end: push → execute → deliver → record | item completes, sink sees it once |
| `completeness.js` | no loss | all N items `completed`, each delivered+acked exactly once |
| `rate.js` | rate limiting | `rate_limit` caps per-poll-cycle dispatch (characterized) |
| `retry.js` | retry + backoff | retries N times, completes, gaps match the ~30s backoff |
| `crash.js` | crash recovery | SIGKILL scheduler mid-flight → reaper rescues all (no loss) |

## How it's wired

```
            docker-compose.stress.yml  (one reproducible env)
  postgres ── migrate ── server ── scheduler        ← system under test
                                       │ executes (real TLS dial)
                                       ▼
                          https://stress-sink.night.enkiduck.com   ← sink (public via Traefik)
                                       ▲
                                       │ introspection (internal http://sink:9000)
                                      k6  ← drives BASE_URL, asserts, exit-codes for CI
```

**Why the sink is public:** the executor's SSRF guard (`internal/safedialer`)
blocks all private IP ranges with no escape hatch, so a sink on the Docker subnet
is unreachable. Exposing it via Traefik at a public host lets the executor accept
it — and exercises the *real* dial/TLS path (closer to prod). k6 itself reads the
sink over the internal network (it isn't subject to the guard).

**Auth:** k6 mints a local HS256 JWT (mirrors `scripts/gentoken.go`); the server's
`EnsureUser` upserts the user and grants daily free credits on first call. No
Clerk, no seeding.

**The sink** (`sink/main.go`, stdlib only) records every delivery keyed by the
`X-Stress-Token` header and answers per a configurable policy
(`ok` / `fail_n` / `always_fail` / `sleep` / `status`), settable per-token or as a
global default via `/control/*`. Introspect at `/deliveries?token=` and `/stats`.

## Running

```bash
make up            # build + start the SUT (postgres, migrate, server, scheduler, sink)
make tracer        # smallest end-to-end proof
make completeness  # no-loss
make rate          # rate-limit characterization
make retry         # retry + backoff (~40s)
make crash         # crash recovery chaos (~40s)
make down          # tear down (incl. volumes)
```

Everything is **light by default** (single-digit/low-tens items, 1 VU) so it's
safe on a shared box. Crank it on a beefier machine via env:

```bash
ITEMS=500 RATE_LIMIT=50 make completeness
RATE_ITEMS=400 RATE_LIMIT=50 make rate
RETRIES=3 make retry                         # deeper exponential-backoff curve
CRASH_ITEMS=200 CRASH_SLEEP_MS=15000 make crash
```

## Targeting a remote (staging/prod) later

The specs take `BASE_URL`; point them at any reachable deployment. Caveat: the
sink must be reachable **from that deployment** (a public sink URL via
`SINK_TARGET_URL`) and you'll need a real token instead of the local HS256 mint.

## Findings surfaced so far (core-api, not harness bugs)

1. **Buffer executions are never billed.** `credits.Deduct` inserts a
   `credit_transactions` row FK'd to `jobs`, but buffer item IDs live in
   `buffer_items` → `credit_transactions_job_id_fkey` violation every run
   (logged WARN, swallowed). See `internal/scheduler/drainer.go`.
2. **At-least-once, not exactly-once, under crash.** A crash after delivery but
   before `CompleteItem` re-fires the target on recovery (`crash.js` reports
   `dupAck`). Fine as a semantic — but it contradicts "exactly-once" framing.
3. **Head-of-line blocking within a buffer.** Items in a claimed batch execute
   sequentially, so one slow item stalls the rest of its batch.
4. **`rate_limit` is "claims per poll cycle," not items/sec.** Observed dispatch
   ≈ `rate_limit` per `DRAINER_POLL_INTERVAL_SEC` (default 1s). `rate.js`
   reports the real per-window distribution.
