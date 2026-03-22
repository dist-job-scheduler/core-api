# Retries

Every job execution can result in success, a retriable failure, or a permanent failure. Retries use configurable backoff with jitter to spread load and avoid thundering herds.

## Backoff strategies

Configured per job via the `backoff` field:

### Exponential (default)

```
delay = 30s × 2^retryCount
delay = min(delay, 1 hour)       // cap
jitter = uniform_random(-delay/4, +delay/4)   // ±25%
final  = delay + jitter
```

Progression: ~30s → ~1m → ~2m → ~4m → ~8m → ... → capped at 1h.

### Linear

```
delay = 30s × (retryCount + 1)
```

Progression: 30s → 60s → 90s → 120s → ...

### Default max retries

- **Free plan**: 3 retries
- **Growth plan**: up to 10 retries
- Configurable per job at creation time, within plan limits.

## Three outcomes per execution

1. **Success** (`HTTP 200`): mark job `completed`, send webhook notification.

2. **Retriable failure** (`retryCount < maxRetries`): compute next retry time via backoff, call `repo.Reschedule()` — resets status to `pending`, increments `retry_count`, sets new `scheduled_at`, clears `claimed_at/claimed_by/heartbeat_at`. Job re-enters the claim queue.

3. **Permanent failure** (`retryCount >= maxRetries`): mark job `failed`, set `last_error`, send webhook notification.

## Reaper retries

If a worker crashes mid-execution (heartbeat goes stale), the [reaper](scheduler.md) handles it identically:

- Under max retries → `RescheduleStale()`: back to `pending` with `last_error = 'worker timeout'`
- Exhausted → `FailStale()`: marked `failed`

The reaper increments `retry_count`, so the job doesn't get infinite retries from repeated crashes.

## Credit cost

Each execution attempt — including retries — costs **1 credit**. Credit is deducted after the HTTP call completes, regardless of outcome. See [billing.md](billing.md) for details.

> **Why charge for retries:** Retries consume real compute and network resources. Making them free would incentivize aggressive retry configs and create a tragedy-of-the-commons on shared infrastructure.

## Source files

- `internal/scheduler/worker.go` — `retryDelay()`, `runJob()` outcome branches
- `internal/infrastructure/postgres/job_repo.go` — `Reschedule()`, `RescheduleStale()`, `FailStale()`
- `internal/domain/job.go` — `Backoff` type, `Status` constants
