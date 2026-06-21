# Fliq

**Reliable HTTP job scheduling for developers and engineering teams.**

Stop reinventing cron. Fliq lets you schedule one-off HTTP calls and recurring cron jobs with a single API call — with built-in retries, crash recovery, and per-execution history. Whether you're triggering a webhook at a specific time or running a recurring data pipeline, Fliq handles the reliability layer so you don't have to.

---

## Why Fliq

**For startups** — Ship faster. Don't burn a sprint building a job queue. POST a job, move on. Fliq handles delivery, retries, and failure visibility out of the box.

**For developers** — No SDKs, no magic. A clean REST API, API token auth (`fliq_sk_*`), idempotency keys, and full execution history per job. Works with any HTTP endpoint. Delivery is at-least-once — every outbound request carries a stable `X-Fliq-Delivery-Id` header so your endpoint can dedupe.

**For engineering leaders** — One less piece of infrastructure to own. Postgres-native, no Redis or Kafka required. Sub-2s pickup latency. At-least-once execution with crash-safe retries via row-level locking. Designed to scale.

---

## Pricing

| Plan | Executions | Price |
|---|---|---|
| Free | 5,000 / day | $0 |
| Pay-as-you-go | Unlimited | $1 per 100,000 executions |

No seat fees. No contracts. Pay only for what you execute.

---

## What's built

| Area | Status |
|---|---|
| One-off job scheduling — create, claim, execute, retry | ✅ Done |
| Cron schedules — recurring jobs with pause/resume | ✅ Done |
| At-least-once execution (FOR UPDATE SKIP LOCKED + reaper) | ✅ Done |
| Crash recovery (heartbeat + reaper process) | ✅ Done |
| API token auth (`fliq_sk_*`) + Clerk JWT | ✅ Done |
| Per-user job isolation (ownership enforced at query level) | ✅ Done |
| Credit system — free tier + pay-as-you-go via Stripe | ✅ Done |
| Failure alerts — notify Slack/webhook channels on retry exhaustion | ✅ Done |
| Usage & failure analytics — per-account stats + per-buffer breakdown | ✅ Done |
| Dead-letter replay — re-run a failed job or buffer item | ✅ Done |
| CI pipeline (lint, tests, migrations against real Postgres) | ✅ Done |

---

## System map

```
[ Client ]
    │  REST API
    ▼
[ server ]  ──────────────────────────────────┐
    │                                         │
    ▼                                         ▼
[ PostgreSQL ] ◄──────────────── [ scheduler ]
                                  Worker + Reaper + Dispatcher + Executor
```

---

## Stack

| Concern | Choice |
|---|---|
| Language | Go 1.25 |
| Web framework | Gin |
| Database | PostgreSQL via `pgx/v5` |
| Migrations | goose |
| Auth | Clerk (JWT RS256 via JWKS) + API tokens (HS256 fallback for local dev) |
| Billing | Stripe — webhooks + credit system |
| Config | `caarlos0/env` — struct tags, no `.env` files in Go code |
| Linter | golangci-lint v2 |

---

## Local dev

```bash
# Prerequisites: Docker, direnv, goose
eval "$(direnv hook zsh)"   # if not already in ~/.zshrc

docker compose up -d postgres
direnv allow
goose -dir ./migrations postgres "$DATABASE_URL" up

go run ./cmd/server        # terminal 1
go run ./cmd/scheduler     # terminal 2
```

See `CLAUDE.md` for the full local setup guide and coding conventions.

---

## Self-host

Fliq is open source and runs the whole stack — API, scheduler, and Postgres — from
one `docker compose`. No Clerk account, no Redis, no external services. Only Docker
is required.

```bash
git clone https://github.com/fliq-sh/core-api.git && cd core-api

# Build + start Postgres, run migrations, then bring up the server + scheduler.
docker compose --profile migrate --profile app up --build
```

That's it — the API is live on `http://localhost:8080`:

```bash
curl localhost:8080/health
# {"status":"ok","version":"dev","time":"..."}
```

### Get a first credential (no Clerk)

Self-hosted Fliq authenticates with **HS256 JWTs** signed by `JWT_SECRET` (set in
`docker-compose.yml` — override it for anything real). Mint one for any user id;
the first authenticated request auto-provisions that user with free credits.

```bash
SECRET='dev-only-change-me-0123456789abcdef0123456789'   # must match the server's JWT_SECRET
b64() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }
h=$(printf '{"alg":"HS256","typ":"JWT"}' | b64)
p=$(printf '{"sub":"local-user","exp":%d}' "$(($(date +%s)+31536000))" | b64)
s=$(printf '%s.%s' "$h" "$p" | openssl dgst -sha256 -hmac "$SECRET" -binary | b64)
JWT="$h.$p.$s"

# Exchange the JWT for a long-lived API token (fliq_sk_*) you can use everywhere:
curl -s -X POST localhost:8080/tokens \
  -H "Authorization: Bearer $JWT" -H 'Content-Type: application/json' \
  -d '{"name":"self-host"}'
# → {"token":"fliq_sk_...", ...}
```

Then schedule a job with the `fliq_sk_*` token:

```bash
curl -X POST localhost:8080/jobs \
  -H "Authorization: Bearer fliq_sk_..." -H 'Content-Type: application/json' \
  -d '{"url":"https://httpbin.org/post","method":"POST","scheduled_at":"2030-01-01T00:00:00Z"}'
```

### Configuration

Everything is env-driven (`caarlos0/env`); the compose file sets sane local defaults.
The ones that matter when self-hosting:

| Var | Purpose |
|---|---|
| `DATABASE_URL` | Postgres connection string |
| `JWT_SECRET` | HS256 signing secret for local auth (min 32 chars) — **change it** |
| `CLERK_JWKS_URL` | set only to use Clerk RS256 auth instead of the HS256 path |
| `WORKER_COUNT` / `POLL_INTERVAL_SEC` | scheduler concurrency + poll cadence |
| `STRIPE_*` | optional — only needed for paid top-ups |

To run the binaries directly instead of in Docker, see **Local dev** above.

---

## Roadmap

### Phase 1 — Core backend ✅
- Job CRUD, worker, reaper, retry with backoff
- Cron schedules with pause/resume
- At-least-once execution with crash-safe retries via Postgres row-level locking
- API token auth + Clerk JWT; jobs scoped to authenticated users
- Credit system: free tier (5k/day) + pay-as-you-go via Stripe
- CI: lint + test + migrations on every PR

### Phase 2 — Deployment 🔄 In progress
- Docker images (`Dockerfile.server`, `Dockerfile.scheduler`, `Dockerfile.migrate`)
- Deploy to K8S on rented VM
- Staging and production environments
- Terraform for infra provisioning (Enkidu)

### Phase 3 — Observability
- OpenTelemetry instrumentation (traces + metrics)
- Prometheus + Grafana dashboards
- Key metrics: job pickup latency, reaper rescue rate, worker lifetime, API p99

### Phase 4 — Frontend
- Dashboard: job list, execution history, schedule management
- Docs integrated into the website — simple, developer-focused
