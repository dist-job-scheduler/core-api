# HTTP Executor

The executor makes outbound HTTP calls to customer URLs when jobs fire. It is designed for safety (timeouts, TLS), observability (request IDs, signing), and connection efficiency (pooling, body drain).

## Timeout strategy

Two layers of protection:

1. **Per-job context timeout**: `context.WithTimeout(ctx, job.TimeoutSeconds * time.Second)`. This is the real enforcement — it governs the dial, TLS handshake, request send, and response read. Default: 30s, configurable per job.

2. **Safety-net client timeout**: `http.Client.Timeout = 5 * time.Minute`. Last resort if context is somehow misconfigured. Never the primary mechanism.

> **Why two layers:** The context timeout respects per-job configuration and propagates cancellation from graceful shutdown. The client timeout is a backstop against leaked goroutines if a context bug allows a request to hang.

## Transport configuration

| Setting | Value | Why |
|---------|-------|-----|
| TLS MinVersion | 1.2 | Security baseline, no legacy TLS |
| MaxIdleConns | 100 | Connection reuse across hosts |
| MaxIdleConnsPerHost | 10 | Prevent one busy host from starving others |
| IdleConnTimeout | 90s | Release unused connections |
| DialTimeout | 10s | Fail fast on unreachable hosts |
| KeepAlive | 30s | Detect dead TCP connections |
| MaxRedirects | 10 | Prevent infinite redirect loops |

## Request signing

Optional HMAC-SHA256 signing for customers who want to verify requests came from Fliq.

- **Payload**: `"{timestamp}.{method}.{url}.{body}"`
- **Headers**: `X-Fliq-Timestamp` (Unix seconds) + `X-Fliq-Signature` (`v1=<hex>`)
- **Secret**: fetched from the user's active signing secret (`user_signing_secrets` table).
- If no secret configured or fetch fails: request goes unsigned (degraded, not blocked).

> **Why `v1=` prefix:** Allows us to change the signing algorithm in the future without breaking existing integrations. Customers match on the prefix.

## Request ID propagation

Each execution generates a new UUID v4, set as `X-Request-ID` on the outbound request. This is stored in context for log correlation throughout the execution lifecycle. Customers can use it to correlate their logs with Fliq's execution history.

## Response handling

- Body is always drained (`io.Copy(io.Discard, resp.Body)`) and closed, even on error. This enables HTTP connection reuse — without draining, Go's transport marks the connection as unusable.
- Only `HTTP 200` counts as success. All other status codes are treated as failures and trigger retry logic.

## No HTTP inside DB transactions

Job execution happens entirely outside any database transaction. DB writes in `runJob()` (create attempt, complete attempt, reschedule, etc.) are independent statements.

> **Why:** A job execution can take up to the configured timeout (default 30s, up to hours). Holding a Postgres connection open that long would starve the pool (max 25 conns). Independent writes also mean a DB hiccup during execution doesn't cancel an otherwise successful HTTP call.

## Source files

- `internal/scheduler/executor.go` — HTTP client, transport config, execution logic
- `internal/scheduler/signer.go` — HMAC-SHA256 signing
- `internal/scheduler/worker.go` — `runJob()` orchestration
- `internal/requestid/requestid.go` — request ID context helpers
