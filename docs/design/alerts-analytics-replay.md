# Failure alerts, analytics & dead-letter replay

Three operator-facing features built on top of the existing execution engine. None
of them change how jobs or buffer items execute — they surface what already
happens and let users act on it.

## Failure alerts

A user configures **alert channels** (`/alerts`). Each channel has a `type`
(`webhook` or `slack`) and a `target` URL. When a job or buffer item exhausts its
retries — i.e. reaches a *permanent* failure, including the out-of-credits failure
— the scheduler fans the event out to every **enabled** channel.

- Delivery lives in `internal/scheduler` (`AlertNotifier`), alongside the existing
  `WebhookNotifier`, because only the scheduler observes terminal failures. It is
  fire-and-forget and best-effort: errors are logged, never returned, and never
  block job processing. The notifier is injected into both the `Worker` and the
  `BufferDrainer`; a nil notifier is a safe no-op so the worker can run without it.
- `slack` channels receive a `{"text": ...}` message; `webhook` channels receive a
  structured JSON payload (`resource_type`, `resource_id`, `last_error`,
  `status_code`, `attempts`, …). Body shaping is a pure function (`buildAlertBody`)
  so it is unit-tested without the network.
- Outbound delivery uses the same SSRF-safe dialer as the executor/webhook paths.
- **`email` is intentionally not implemented yet** — it needs a mail provider and
  credentials this service does not have. The type enum is open for it later; the
  API rejects unknown types at the binding layer.

Why fire on *permanent* failure only (not every retry): retries are expected and
self-healing. An alert means "your endpoint is down and Fliq has given up" — the
signal a reliability tool exists to provide. Permanent failures are rare relative
to total executions, so the one indexed `ListEnabled` query per failure is cheap.

## Usage & failure analytics

Read-only aggregates over data the engine already records — no new write paths.

- **`GET /stats/jobs?days=N`** — from `job_attempts` joined to `jobs`: total
  completed attempts, success/failure counts, success rate, avg & p95 duration. A
  success is a 2xx with no transport error; everything else is a failure.
- **`GET /stats/usage?days=N`** — from `credit_transactions`: per-UTC-day execution
  counts split job vs buffer, plus window totals and the current credit balance
  (best-effort; a missing balance does not fail the report). This is the
  "what am I paying for" view.
- **`GET /buffers/:id/stats`** — per-buffer item status breakdown (pending/running/
  completed/failed/total) and success rate. Ownership is enforced via the buffer's
  `GetByID` before the stats query runs.

`days` defaults to 30 and is clamped to `[1, 365]` in the usecase.

## Dead-letter replay

- **`POST /jobs/:id/replay`** and **`POST /buffers/:id/items/:itemId/replay`** —
  re-run a permanently-**failed** job/item. Anything not in `failed` returns 409.

Replay **clones** rather than resetting the original row in place:

- Job attempt records are keyed `UNIQUE(job_id, attempt_num)`. Re-running the same
  row with `retry_count` reset would collide on `attempt_num`. A fresh job gets a
  clean attempt sequence.
- The original failed row is preserved as history/audit.
- The clone carries a `replay_of` FK back to the source (`ON DELETE SET NULL`) for
  lineage, a fresh idempotency key, `status = pending`, and `scheduled_at = now`.
  A replayed buffer item is appended to the **tail** of the buffer (new
  `created_at`), so it drains in order after the current queue rather than jumping
  ahead of in-flight work.

A replay enqueues a new execution, so it passes the same credit gate as a fresh
create/push (402 when out of credits).
