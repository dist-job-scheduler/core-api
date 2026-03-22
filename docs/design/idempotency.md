# Idempotency

Idempotency keys prevent duplicate job creation. A client can safely retry a `POST /jobs` request with the same key and get the same result without creating a second job.

## Database constraint

```sql
UNIQUE (user_id, idempotency_key)
```

- **Per-user scoping**: different users can reuse the same key independently. This simplifies client-side key generation — no need for globally unique keys.
- PostgreSQL error code `23505` (unique violation) is mapped to `domain.ErrDuplicateJob` at the repo layer → HTTP 409 Conflict at the handler layer.

## Auto-generated keys

If the client omits `idempotencyKey`, the usecase generates a UUID v4 (`google/uuid`). This means every call without a key creates a new job — idempotency is opt-in.

## Schedule-generated keys

When the cron [dispatcher](cron-scheduling.md) fires a schedule, it generates:

```
sched:{schedule_id}:{unix_timestamp_of_next_run_at}
```

This ties each cron fire to a specific scheduled time. If the dispatcher retries (e.g., transaction rolled back and re-executed), the same key produces a duplicate violation instead of a second job.

## Duplicate handling in ClaimAndFire

When `ClaimAndFire` encounters a duplicate key during schedule firing:
1. The job insert is skipped (caught by unique constraint).
2. The schedule's `next_run_at` is **still advanced** — the schedule doesn't get stuck.

> **Why not fail the whole transaction:** A duplicate means the job was already created (likely by a previous dispatcher cycle that committed). The correct action is to advance the schedule, not retry indefinitely.

## Source files

- `internal/usecase/job.go` — `CreateJob()`, auto-generate key if missing
- `internal/infrastructure/postgres/job_repo.go` — `Create()`, duplicate detection
- `internal/infrastructure/postgres/schedule_repo.go` — `ClaimAndFire()`, schedule-generated keys
- `migrations/20260222082618_initial_schema.sql` — original constraint
- `migrations/20260301000000_jobs_add_user_id.sql` — per-user scoped constraint
