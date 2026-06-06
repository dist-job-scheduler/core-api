# Buffer delivery: ordering, retries, and rate limiting

Status: **Accepted** (supersedes the original buffer drainer behavior)

This decision records how a **buffer** delivers its **items** — the ordering
guarantee, how retries and permanent failures interact with that guarantee, and
what `rate_limit` means. It is the design gate for the strict-ordering and
token-bucket work. For the credit/billing model see [billing.md](billing.md);
for the generic executor see [http-executor.md](http-executor.md).

## Context

A buffer is a **per-client ordered stream**: a client pushes items and expects
them delivered to the buffer's target in submission order. The original drainer
did not honor that:

- **Batch claiming, sequential execution.** `drainBuffer` claimed up to
  `rate_limit` items per poll cycle and ran them one at a time. Order held
  *within* a batch but not across batches.
- **Retry reordering.** A failed item was rescheduled to a future
  `scheduled_at`; the next claim cycle skipped it (it was no longer due) and ran
  later items — so a successor overtook a retrying predecessor.
- **Overlapping cycles.** A slow `drainBuffer` from one tick could still be
  running when the next tick started another for the same buffer (disjoint items
  via `SKIP LOCKED`), putting two of a buffer's items in flight at once.
- **Fuzzy rate.** `rate_limit` was effectively "items claimed per poll cycle"
  (`DRAINER_POLL_INTERVAL_SEC`), not a defined items/sec rate. Slow batches could
  overlap the next cycle and exceed it.

## Decision

### 1. Strict per-buffer ordering

At most **one item per buffer is in flight at any time**. Item *N+1* is
dispatched only after item *N* has reached a **terminal** state (`completed` or
`failed`). Items are ordered by `created_at` (FIFO), which is already the claim
order.

Consequences:

- Cross-buffer concurrency is unchanged — different buffers still drain
  concurrently up to `DRAINER_CONCURRENCY`. Ordering is a *per-buffer* property.
- A buffer's effective in-flight concurrency is 1. This is intentional: ordering
  and parallelism are mutually exclusive for a single ordered stream.

### 2. Ordered retries (head-of-line blocking is deliberate)

A retrying item **blocks its successors** until it is terminal. A transient
failure on item *N* holds the line; *N* is retried with the existing backoff
(see [retries.md](retries.md)) and no later item is delivered until *N*
completes. This makes head-of-line blocking an accepted cost of ordered
delivery, not a bug.

### 3. Poison-pill policy: skip-after-permanent-fail

When item *N* exhausts `max_retries` it transitions to `failed` (terminal) and
the buffer **advances** to *N+1*. The poison item does not stall the stream
indefinitely. Order is preserved among everything that is delivered; a
permanently-failed item is simply absent from the delivered sequence (and
visible as `failed` via the API).

Rejected alternative: *dead-letter + pause the buffer*. Pausing the whole stream
on one bad item couples unrelated items' fate together and needs manual
intervention to resume — worse for the common case. We may revisit a dead-letter
**sink** later as an additive feature, but the default is to keep moving.

### 4. `rate_limit` is a true per-second token bucket

`rate_limit` means **maximum deliveries per second**, per buffer — not per poll
cycle. Each buffer has a token bucket refilled at `rate_limit` tokens/second
(capacity = `rate_limit`). A delivery requires a token; with one in-flight item
the bucket paces successive deliveries.

The bucket is **DB-coordinated** so it stays correct if the scheduler ever runs
more than one replica: tokens and a `last_refill_at` are persisted per buffer and
claimed atomically in SQL (lazy refill computed from elapsed time on claim, same
shape as the lazy daily credit refresh in [billing.md](billing.md)). No
in-memory-only bucket — that would over-admit across replicas.

### 5. Reaper compatibility

The buffer reaper still rescues items stuck in `running` past the heartbeat
timeout. Because rescue resets `status` to `pending` without changing
`created_at`, a rescued item keeps its position in the order. Recovery therefore
preserves ordering. (Delivery remains **at-least-once** across a crash — a target
hit before the crash may be re-hit on recovery; see issue #28 for the
`X-Fliq-Delivery-Id` idempotency key consumers use to dedupe.)

## Implications for implementation

- **Claiming** changes from "up to `rate_limit` items" to "the next due item, only
  if the buffer has no in-flight item and a rate token is available," in a single
  atomic statement using `FOR UPDATE SKIP LOCKED`.
- The per-buffer "no in-flight item" gate replaces the batch loop in
  `drainBuffer`.
- Throughput for a buffer is bounded by `min(rate_limit/sec, 1 / per-item
  latency)`. Clients that need higher throughput without ordering should use
  multiple buffers.

## Reconciliation with the slice issues

- **#30 (strict ordering):** items #1–#3 and #5 above. Acceptance: in-order
  delivery for a healthy target; no successor overtakes a retrying item; a
  permanently-failed item advances the stream (skip-after-fail); no two items of
  one buffer in flight; reaper keeps order.
- **#31 (token bucket):** item #4. Acceptance: ≤ `rate_limit` deliveries/sec over
  a sustained burst; multi-replica-safe token accounting; pacing never reorders.
