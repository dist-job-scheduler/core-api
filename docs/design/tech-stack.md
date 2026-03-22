# Tech Stack

Core-api is a Go HTTP API server and background scheduler backed by PostgreSQL. The stack is chosen for low latency, operational simplicity (single binary, no message broker), and strong concurrency primitives.

## Runtime

- **Go 1.25** — goroutine-native concurrency for the scheduler, single binary deploys, strong stdlib.
- **Gin** — lightweight HTTP framework with middleware chaining. All handlers use `ctx.Request.Context()` to propagate cancellation and request IDs into usecases.

## Database

- **PostgreSQL 16** via `pgx/v5` (`pgxpool.Pool`). No ORM — all queries are hand-written SQL.
- **Migrations**: goose with `-- +goose Up/Down` annotations, SQL-only. Stored in `migrations/`.

> **Why pgx over database/sql:** native Postgres types (JSONB, TIMESTAMPTZ), `FOR UPDATE SKIP LOCKED` support without driver quirks, built-in connection pooling, and future LISTEN/NOTIFY potential.

### Connection pool

Configured in `internal/infrastructure/postgres/db.go`:

| Setting | Value | Why |
|---------|-------|-----|
| MaxConns | 25 | Enough for API + scheduler without exhausting Postgres `max_connections` |
| MinConns | 5 | Warm pool on startup, avoid cold-start latency |
| MaxConnLifetime | 1 hour | Rotate connections to pick up DNS changes and prevent stale TCP |
| MaxConnIdleTime | 30 min | Release idle conns after K8s pod scale-down |
| HealthCheckPeriod | 30s | Detect broken connections before they fail a request |
| ConnectTimeout | 5s | Fail fast on network issues |

## Configuration

- **`caarlos0/env/v11`** with struct tags + `go-playground/validator/v10` for validation.
- No `.env` files loaded by Go — `direnv` handles local environment.
- See `config/config.go` for the full config struct and defaults.

## Auth

- **`lestrrat-go/jwx/v2`** — JWKS fetching and RS256 JWT validation (Clerk in production).
- **`golang-jwt/jwt/v5`** — HS256 fallback for local dev without Clerk.
- API tokens (`fliq_sk_*`) hashed with SHA-256, stored in `api_tokens` table.

## Cron

- **`robfig/cron/v3`** — standard 5-field cron expression parsing for schedule `next_run_at` computation.

## Billing

- **Stripe Go SDK (`stripe-go/v82`)** — checkout sessions for credit top-ups, webhook for payment confirmation.

## Monitoring

- **Prometheus `client_golang`** — histograms, counters, gauges for scheduler and HTTP metrics.
- **`log/slog`** + `lmittmann/tint` (local colored output) / `slog.NewJSONHandler` (prod JSON for Datadog/Cloud Logging).

## HTTP client

- TLS 1.2+ enforced on both the job executor and webhook delivery clients (`crypto/tls.Config{MinVersion: tls.VersionTLS12}`).
- Shared `http.Client` with connection pooling (100 max idle, 10 per host, 90s idle timeout).

## Source files

- `config/config.go` — config struct, env parsing, validation
- `internal/infrastructure/postgres/db.go` — pool configuration
- `go.mod` — dependency versions
