# Cron Scheduling

Cron schedules are recurring jobs defined by a standard 5-field cron expression. The dispatcher goroutine fires them into one-time jobs at the right time.

## Dispatcher

Runs every `DISPATCH_INTERVAL_SEC` (default 5s) inside the scheduler binary. Each cycle:

1. **Claim due schedules**: `SELECT ... FROM schedules WHERE next_run_at <= NOW() AND NOT paused ... FOR UPDATE SKIP LOCKED`
2. **For each schedule**: check credits → insert job → advance `next_run_at`
3. **Commit**: all-or-nothing transaction

> **Why single transaction:** If the scheduler crashes between inserting a job and advancing `next_run_at`, the transaction rolls back. No orphaned jobs, no stuck schedules.

## ClaimAndFire (atomic transaction)

All operations happen in one Postgres transaction in `schedule_repo.go`:

```
BEGIN
  SELECT schedules ... FOR UPDATE SKIP LOCKED  (claim)
  For each schedule:
    INSERT INTO jobs (... idempotency_key = 'sched:{id}:{timestamp}')
    UPDATE schedules SET next_run_at = ..., last_run_at = NOW()
COMMIT
```

- `FOR UPDATE SKIP LOCKED` prevents double-firing across multiple dispatcher replicas.
- [Idempotency keys](idempotency.md) (`sched:{id}:{timestamp}`) prevent duplicate jobs if the transaction is retried.
- Batch limit: 100 schedules per cycle.

## Computing next run time

Uses `robfig/cron/v3` `ParseStandard()` to parse the cron expression, then:

```go
next := sched.Next(s.NextRunAt)
for next.Before(now) {
    next = sched.Next(next)
}
```

This **skips missed runs**. If the scheduler was down for 3 hours and a schedule fires every 10 minutes, it fires once (the next future time), not 18 times.

> **Why skip missed runs:** Catching up on missed runs would create a burst of potentially stale jobs. Most cron users expect "run at the next matching time", not "make up for lost time". If catch-up is needed, it should be an explicit feature.

## Credit gate

Before inserting a job, the dispatcher checks `creditRepo.HasCredits(userID)`:

- **Sufficient credits**: fire the job normally.
- **Insufficient credits**: skip the job, but **still advance `next_run_at`** so the schedule doesn't get stuck.
- **DB error**: fail open (fire the job). Prefer a potentially uncharged execution over silently dropping a scheduled job.

> **Why fail open:** A transient DB error shouldn't cause missed cron fires. The credit deduction happens later in the worker — if the user truly has no credits, the worker's own credit check will catch it.

## Pause / Resume

`SetPaused()` uses an atomic update with state check:

```sql
UPDATE schedules SET paused = $3 WHERE id = $1 AND user_id = $2 AND paused = $4
```

- 0 rows affected + schedule exists → already in desired state (409 Conflict)
- 0 rows affected + schedule missing → 404 Not Found
- Paused schedules are excluded from the dispatcher's claim query.

## Source files

- `internal/scheduler/dispatcher.go` — dispatch loop, `computeNext()`
- `internal/infrastructure/postgres/schedule_repo.go` — `ClaimAndFire()`, `SetPaused()`
- `internal/domain/schedule.go` — `Schedule` struct, cron expression field
