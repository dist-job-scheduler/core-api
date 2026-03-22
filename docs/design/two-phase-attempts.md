# Two-Phase Attempt Writes

Every job execution creates an attempt record in two phases: **open before the HTTP call, close after**. This ensures crash visibility — if a worker dies mid-execution, the incomplete attempt is immediately visible.

## Phase 1: CreateAttempt (before HTTP call)

Insert a `job_attempts` row with:
- `job_id`, `attempt_num = retry_count + 1`, `worker_id`, `started_at`
- `completed_at = NULL` (signals "in progress")

**If CreateAttempt fails**: `runJob()` returns immediately. The job stays in `running` status with no heartbeat updates. The [reaper](scheduler.md) detects the stale heartbeat within ~30s and reschedules.

> **Why abort on failure:** If the DB rejected this write, all subsequent writes (Complete, Reschedule, Fail) would also fail. Executing the HTTP call blind wastes the attempt and leaves the user with no visibility.

## Phase 2: CompleteAttempt (after HTTP call)

Update the attempt row:
- `completed_at = NOW()`, `status_code`, `error`, `duration_ms`

**If CompleteAttempt fails**: logged as warning, but the job outcome is unaffected. The job's status (completed/failed/rescheduled) is determined by a separate DB call.

## Crash visibility

An attempt row with `completed_at = NULL` is immediately visible in `GET /jobs/:id/attempts`. This tells the user (and the dashboard) that a worker was executing this job and either:
- Is still running (heartbeat recent), or
- Crashed (heartbeat stale, reaper will rescue)

No polling or reaper cycle needed for the user to see the state.

## Safety constraint

```sql
UNIQUE (job_id, attempt_num)
```

`FOR UPDATE SKIP LOCKED` already prevents two workers from claiming the same job, but if a bug ever breaks claiming, the DB rejects the duplicate attempt rather than storing phantom data.

> **Why not wrap the HTTP call in a DB transaction:** The outbound call can take up to the configured timeout (30s default, up to hours). Holding a Postgres connection open that long would starve the pool. See [http-executor.md](http-executor.md).

## Source files

- `internal/scheduler/worker.go` — `runJob()` (phases 1 & 2), `closeAttempt()`
- `internal/infrastructure/postgres/attempt_repo.go` — `CreateAttempt()`, `CompleteAttempt()`
- `internal/domain/job.go` — `JobAttempt` struct
- `migrations/20260301000001_attempts_unique_constraint.sql` — UNIQUE constraint
