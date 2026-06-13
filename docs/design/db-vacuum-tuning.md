# DB churn, vacuum & bloat — analysis and plan

The scheduler is Postgres-native and write-heavy: every job goes through
`claim → heartbeat (×N) → complete/reschedule`, and the buffer drainer does the
same on `buffer_items` plus a per-claim token-bucket update on `buffers`. This
note records where we waste storage/IO and the staged plan to fix it.

## Findings

### 1. Heartbeat write-amplification (the big one)

`UpdateHeartbeat` runs every 10s per running job:

```sql
UPDATE jobs SET heartbeat_at = NOW(), updated_at = NOW() WHERE id = $1 AND status = 'running'
```

Postgres MVCC rewrites the **entire** row to bump one timestamp — and a job row
carries `url`, `headers` (JSONB), and `body`, ~1.3 KB in our measurement. Worse,
`heartbeat_at` is indexed (`idx_jobs_stale`), so the update can **never be HOT**:
each beat also writes a new index entry and leaves a dead one. `buffer_items` has
the identical shape (`idx_buffer_items_stale`).

Measured on PG 16 (autovacuum off, to show raw churn): 50 running jobs × 360
beats (1 hour) →

| design | HOT % | total size after | vs sidecar |
|---|---|---|---|
| current (`jobs`, beat on the full row) | **0 %** | **24 MB** | 16.7× |
| sidecar (`job_heartbeats`, tiny row) | ~53 % | 1.5 MB | 1× |

That 24 MB is dead tuples + index bloat **per hour per 50 concurrent jobs**, all
of which autovacuum then has to churn through. This is the dominant waste.

### 2. No per-table autovacuum tuning

Every table used the cluster defaults, notably
`autovacuum_vacuum_scale_factor = 0.2` — dead tuples are allowed to reach 20 % of
the table before a vacuum runs. On the hot tables that means large, bursty
vacuums and steady-state bloat.

### 3. `job_attempts` completion update wasn't HOT

`CreateAttempt` (insert) then `CompleteAttempt` (UPDATE of `completed_at`,
`status_code`, `error`, `duration_ms` — **no indexed column**) is HOT-eligible,
but with the default `fillfactor = 100` there's no room on the page, so it became
a dead tuple instead. With headroom (`fillfactor = 85`) it can self-clean.

### 4. Unbounded growth (future work)

`jobs` and `job_attempts` are never pruned (history is user-facing + billing).
Vacuum/scan cost grows with the tables forever. Addressed in Phase 3.

## Phase 1 — storage-parameter tuning (migration `20260614000000`)

Pure metadata DDL: brief `ACCESS EXCLUSIVE` lock, **no table rewrite**;
`fillfactor` only affects future writes. Zero behavior change, so it can't
regress correctness.

- `jobs`, `buffer_items`: aggressive autovacuum (`scale_factor 0.05`,
  `cost_limit 1000`) to clean the heavy churn sooner; `fillfactor 90` headroom.
- `job_attempts`: `fillfactor 85` to make the completion UPDATE HOT; eager
  autovacuum incl. `vacuum_insert_scale_factor 0.1` (insert-heavy).
- `buffers`: `fillfactor 85` so the token-bucket update is HOT.

**Existing bloat is not reclaimed by this** (fillfactor is forward-only). Run a
one-time `pg_repack` (online, no long lock) on `jobs`, `buffer_items`,
`job_attempts` after deploy to compact what's already there. `VACUUM FULL` also
works but takes an exclusive lock — avoid on the live scheduler.

## Phase 2 — heartbeat sidecar (jobs `20260614000001`, buffer_items `20260614000002`)

Status: **done for both the jobs scheduler path and the buffer drainer path.**
`buffer_item_heartbeats` mirrors `job_heartbeats` exactly (claim seeds it via a
CTE; heartbeat is a HOT sidecar upsert; complete/fail/reschedule drop it; the
reaper derives staleness via `LEFT JOIN`, a missing row counting as stale). The
buffer change was validated end-to-end by the `crash.js` chaos test: SIGKILL the
scheduler mid-flight → restart → reaper rescued every orphaned item to
`completed`, zero loss.

Measured on the migrated schema (50 running jobs × 360 beats/hr, autovacuum off
to show raw churn): the old full-row heartbeat left **15.4k dead tuples / ~21 MB**
of churn on `jobs`; the sidecar moves that to a **~1.5 MB** table (mostly HOT) and
`jobs` now takes **zero** heartbeat writes.

Move the volatile heartbeat out of the heavy row:

```sql
CREATE TABLE job_heartbeats (
  job_id       TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
  heartbeat_at TIMESTAMPTZ NOT NULL
) WITH (fillfactor = 70);
-- buffer_items gets the same treatment.
```

- `Claim` inserts/sets the heartbeat row when it flips a job to `running`.
- `UpdateHeartbeat` becomes `UPDATE job_heartbeats SET heartbeat_at = NOW() WHERE job_id = $1`
  — a tiny, HOT, index-free write (~16× less data, isolated for cheap vacuum).
- The **reaper** changes its source of truth: stale = `jobs.status='running'`
  joined to `job_heartbeats.heartbeat_at < cutoff` (or a `LEFT JOIN ... IS NULL`
  for rows whose heartbeat row is missing). Drop `idx_jobs_stale`; add an index
  on `job_heartbeats(heartbeat_at)`.
- Clear the heartbeat row on complete/fail/reschedule (or rely on the `jobs`
  status filter + ON DELETE CASCADE for terminal one-offs).

Crash-recovery semantics are preserved: a worker that dies stops updating the
sidecar, the reaper sees a stale/absent heartbeat and reschedules. This touches
claim/heartbeat/reschedule/reaper, so it ships as its own PR with integration +
crash tests. It is the change that actually removes the write-amplification.

## Phase 3 — partition `job_attempts` by month (migration `20260614000003`)

Status: **done.** `job_attempts` is now `PARTITION BY RANGE (started_at)`, monthly,
with a `DEFAULT` catch-all. PK is `(id, started_at)` and the unique guard is
`(job_id, attempt_num, started_at)` (the partition key must be in every unique
key). `CompleteAttempt` now passes `started_at`, so the completion UPDATE prunes
to a single partition. The conversion migration renames the old table, frees the
colliding constraint/index names, creates the partitioned table + partitions, and
copies the data (validated: existing rows are preserved and routed to the right
month). `job_attempts` has no inbound FKs, so this is clean.

**Retention runbook.** Drop expired months instead of `DELETE` (instant, no dead
tuples):

```sql
DROP TABLE job_attempts_2026_03;   -- e.g. keep the last N months
```

**Partition rotation (operational follow-up).** Monthly partitions are
pre-created through 2027-12 plus a `DEFAULT`. Before then, a periodic job must
create next month's partition and drop expired ones — either a small scheduler
task or `pg_partman`. Until automated, the `DEFAULT` partition absorbs overflow
(keep it empty by pre-creating ahead so future `CREATE PARTITION` stays fast).

**Operational note.** The conversion copies all rows under a lock for the copy's
duration. `job_attempts` is small today; on a large table, run during a
low-traffic window or migrate online (e.g. `pg_partman` + background copy).

## Benchmarks

Hot-path Go benchmarks live in
`internal/infrastructure/postgres/hotpath_bench_test.go` (build tag
`integration`). Baseline / regression guard:

```bash
DATABASE_URL=postgres://... go test -tags integration -run='^$' \
    -bench=. -benchmem ./internal/infrastructure/postgres/
```

Black-box throughput/chaos lives in `tests/load` (k6) and `tests/stress` (k6 +
sink). Run those before/after any Phase 2/3 change to confirm no throughput or
correctness regression.
