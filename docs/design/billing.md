# Billing

Fliq uses a credit-based billing model. Each execution attempt (including [retries](retries.md)) costs 1 credit. Free-plan users get a daily allowance; paid users top up via Stripe.

## Credit model

| Plan | Credits | Refresh |
|------|---------|---------|
| Free | `daily_free_limit` per day | Lazy UTC-day refresh |
| Paid | Purchased via Stripe | No refresh, balance decrements |

## Dual HasCredits gate

Credits are checked at two points to minimize wasted work:

1. **At job creation** (`usecase.CreateJob()`): prevents scheduling jobs the user can't afford. Returns an error to the API caller.

2. **At execution** (`worker.runJob()`): catches retries for users who ran out of credits between creation and execution. Fails the job immediately with `"insufficient credits"` — no HTTP call made.

> **Why two gates:** Gate 1 is a UX optimization (fail fast at the API). Gate 2 is the real guard — without it, a free-plan user could schedule 5,000 jobs at 11:59 PM and they'd all execute the next day when credits refresh, far exceeding the daily limit on retries.

## Lazy daily refresh

Free-plan credits refresh on first use each UTC day, not on a cron schedule:

```sql
UPDATE user_credits
SET    balance = daily_free_limit, refreshed_at = NOW()
WHERE  user_id = $1
  AND  plan = 'free'
  AND  DATE(refreshed_at AT TIME ZONE 'UTC') < CURRENT_DATE
```

If the refresh applies, a `daily_grant` transaction is recorded in the audit ledger.

> **Why lazy refresh:** No background job needed. Users who don't make requests don't generate unnecessary writes. The check is inside a transaction with `HasCredits` for consistency.

## Credit deduction

Called after the HTTP call in `runJob()`, regardless of outcome:

```go
if err := w.credits.Deduct(ctx, job.UserID, job.ID); err != nil {
    w.logger.WarnContext(ctx, "credit deduction failed", ...)
    // Non-fatal: job outcome is unaffected
}
```

- **Non-fatal**: if the DB fails during deduction, the job's success/failure result is not changed. The user gets one free attempt — acceptable tradeoff for simplicity.
- **Post-execution**: we charge for work done, not work attempted. If the worker crashes before `Deduct()`, the reaper reschedules the job and the next attempt will deduct.

## Audit ledger

`credit_transactions` is an immutable, append-only table:

| Type | Amount | When |
|------|--------|------|
| `job_execution` | -1 | After each execution attempt |
| `daily_grant` | +`daily_free_limit` | On first use each UTC day (free plan) |
| `stripe_topup` | +purchased | After Stripe payment confirmation |

Each row references the triggering entity (`job_id` for executions, `stripe_payment_intent_id` for top-ups).

## Stripe integration

- **Checkout sessions**: created via Stripe Go SDK for credit purchases.
- **Webhooks**: `checkout.session.completed` triggers `TopUp()` to credit the user's balance.
- **Customer mapping**: `stripe_customers` table links `user_id` to `stripe_customer_id`.

## Source files

- `internal/infrastructure/postgres/billing_repo.go` — `HasCredits()`, `Deduct()`, `TopUp()`, lazy refresh
- `internal/usecase/job.go` — gate 1 (creation-time credit check)
- `internal/scheduler/worker.go` — gate 2 (execution-time credit check), deduction
- `internal/domain/billing.go` — `CreditBalance`, `CreditTransaction`, `Plan` types
- `internal/stripe/client.go` — Stripe checkout and webhook handling
- `migrations/20260304000000_billing.sql` — schema
