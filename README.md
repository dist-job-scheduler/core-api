# Fliq

**Reliable HTTP job scheduling for developers and engineering teams.**

Stop reinventing cron. Fliq lets you schedule one-off HTTP calls and recurring cron jobs with a single API call — with built-in retries, crash recovery, and per-execution history. Whether you're triggering a webhook at a specific time or running a recurring data pipeline, Fliq handles the reliability layer so you don't have to.

---

## Why Fliq

**For startups** — Ship faster. Don't burn a sprint building a job queue. POST a job, move on. Fliq handles delivery, retries, and failure visibility out of the box.

**For developers** — No SDKs, no magic. A clean REST API, API token auth (`fliq_sk_*`), idempotency keys, and full execution history per job. Works with any HTTP endpoint.

**For engineering leaders** — One less piece of infrastructure to own. Postgres-native, no Redis or Kafka required. Sub-2s pickup latency. Exactly-once execution via row-level locking. Designed to scale.

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
| Exactly-once execution (FOR UPDATE SKIP LOCKED + reaper) | ✅ Done |
| Crash recovery (heartbeat + reaper process) | ✅ Done |
| API token auth (`fliq_sk_*`) + Clerk JWT | ✅ Done |
| Per-user job isolation (ownership enforced at query level) | ✅ Done |
| Credit system — free tier + pay-as-you-go via Stripe | ✅ Done |
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

## Roadmap

### Phase 1 — Core backend ✅
- Job CRUD, worker, reaper, retry with backoff
- Cron schedules with pause/resume
- Exactly-once execution via Postgres row-level locking
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
