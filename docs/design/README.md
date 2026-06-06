# Core-API Design Decisions

Key design decisions in core-api, documented for the team. These explain **why** things are built the way they are. For coding conventions and quick-reference, see [`../../CLAUDE.md`](../../CLAUDE.md).

When changing a design, update the relevant doc in the same PR.

| Doc | Summary |
|-----|---------|
| [tech-stack.md](tech-stack.md) | Go 1.25, Gin, PostgreSQL 16/pgx, goose, Clerk, Stripe, Prometheus |
| [scheduler.md](scheduler.md) | Worker/Reaper/Dispatcher goroutines, job claiming, heartbeat, graceful shutdown |
| [http-executor.md](http-executor.md) | Per-job timeout, TLS 1.2+, request signing, connection reuse |
| [retries.md](retries.md) | Exponential/linear backoff, jitter, retry outcomes, credit-per-attempt |
| [two-phase-attempts.md](two-phase-attempts.md) | CreateAttempt before HTTP, CompleteAttempt after, crash visibility |
| [idempotency.md](idempotency.md) | UNIQUE(user_id, key), auto-gen UUID, schedule-generated keys |
| [cron-scheduling.md](cron-scheduling.md) | Dispatcher, ClaimAndFire atomic tx, missed-run handling, credit gate |
| [auth.md](auth.md) | JWT (Clerk RS256) + API tokens (fliq_sk_*), user-scoped 404s |
| [billing.md](billing.md) | Credit model, lazy daily refresh, dual HasCredits gate, audit ledger |
| [buffer-delivery.md](buffer-delivery.md) | Per-buffer ordering, ordered retries, skip-after-fail, per-second token bucket |
| [observability.md](observability.md) | Structured logging, Prometheus metrics, health checks, request ID |
