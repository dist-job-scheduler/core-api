# Scheduler

The scheduler binary (`cmd/scheduler/main.go`) runs three concurrent goroutines — Worker, Reaper, and Dispatcher — plus a metrics/health HTTP server. It is designed for single-replica-per-region deployment, though all components are safe to run as multiple replicas.

## Architecture

```
cmd/scheduler/main.go
    ├── Worker     — claims and executes pending jobs
    ├── Reaper     — detects crashed workers, rescues stale jobs
    ├── Dispatcher — fires cron schedules into jobs (see cron-scheduling.md)
    └── Metrics    — Prometheus + health endpoints on :9090
```

All goroutines share a `context.Context` from `signal.NotifyContext(SIGINT, SIGTERM)` for coordinated shutdown.

## Job claiming

Jobs are claimed using PostgreSQL's `FOR UPDATE SKIP LOCKED` in a single atomic statement:

```sql
UPDATE jobs
SET    status = 'running', claimed_at = NOW(),
       claimed_by = $1, heartbeat_at = NOW()
WHERE id IN (
    SELECT id FROM jobs
    WHERE  status = 'pending' AND scheduled_at <= NOW()
    ORDER BY scheduled_at ASC
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
RETURNING ...
```

- **Atomic**: `pending` → `running` in one statement, no race window.
- **Disjoint**: `SKIP LOCKED` ensures competing workers each get a different set of jobs.
- **FIFO**: `ORDER BY scheduled_at ASC` processes oldest-due jobs first.
- **Partial index**: `idx_jobs_due ON jobs(scheduled_at) WHERE status = 'pending'` keeps the poll fast.

> **Why no Redis/Kafka:** Postgres `SKIP LOCKED` is sufficient at our scale. One fewer operational dependency. If we need higher throughput, we can partition by region or add workers — the claiming pattern scales horizontally.

## Worker concurrency

The worker uses a **buffered channel semaphore** (`chan struct{}` of size `WORKER_COUNT`, default 5, max 100):

```
Poll loop (every POLL_INTERVAL_SEC):
  1. available = cap(sem) - len(sem)
  2. if available == 0: skip (backpressure)
  3. Claim(workerID, available) — only claim what we can start
  4. For each job: acquire sem slot, spawn goroutine, release on exit
```

The poll loop is never blocked by slow jobs. Each goroutine holds its semaphore slot for the duration of execution, naturally limiting concurrency.

Worker ID format: `hostname-pid` (e.g., `scheduler-pod-7b4f-12345`), used for tracing and debugging stale claims.

## Heartbeat

While a job executes, a background goroutine updates `heartbeat_at = NOW()` every **10 seconds**. It is cancelled via context when the job finishes.

```go
go w.heartbeat(heartbeatCtx, job.ID)  // cancelled when runJob returns
```

> **Why heartbeat over timeout:** A fixed timeout would either be too short (kills slow-but-healthy jobs) or too long (delays crash detection). Heartbeats let us detect crashes within ~30s while supporting jobs that run for minutes.

## Reaper

The reaper runs every **30 seconds** and scans for jobs stuck in `running` with a stale heartbeat (> 30s ago = ~3 missed beats):

- **RescheduleStale**: `retry_count < max_retries` → reset to `pending`, increment `retry_count`, set `last_error = 'worker timeout'`. The job re-enters the claim queue with its existing `scheduled_at`.
- **FailStale**: `retry_count >= max_retries` → mark `failed` with `'worker timeout: max retries exceeded'`.

Both queries use `FOR UPDATE SKIP LOCKED` with `LIMIT 100` — safe with multiple reaper replicas and bounded per cycle.

**Partial index**: `idx_jobs_stale ON jobs(heartbeat_at) WHERE status = 'running'` ensures the reaper only scans running jobs.

## Graceful shutdown

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
```

On signal: context cancels → worker stops claiming → in-flight jobs finish (context propagates) → metrics server gets 10s `Shutdown()`. No abrupt kills.

## Source files

- `cmd/scheduler/main.go` — entry point, goroutine orchestration
- `internal/scheduler/worker.go` — poll loop, semaphore, heartbeat, job execution
- `internal/scheduler/reaper.go` — stale job detection and rescue
- `internal/infrastructure/postgres/job_repo.go` — `Claim()`, `RescheduleStale()`, `FailStale()`, `UpdateHeartbeat()`
