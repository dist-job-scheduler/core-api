# Observability

Core-api uses structured logging with request ID correlation, Prometheus metrics for dashboards and alerting, and health check endpoints for Kubernetes probes.

## Structured logging

- **Library**: `log/slog` with a custom `ContextHandler` wrapper that auto-extracts `request_id` from context on every log call.
- **Local**: `lmittmann/tint` — colored, human-readable, Kitchen time format.
- **Production**: `slog.NewJSONHandler` — one JSON object per line, parseable by Datadog/Cloud Logging.
- **Level**: configurable via `LOG_LEVEL` env (debug|info|warn|error, default info).

### Convention

Always use `*Context` methods:

```go
logger.InfoContext(ctx, "job completed", "job_id", job.ID)   // correct
logger.Info("job completed", "job_id", job.ID)               // loses request_id correlation
```

Components pre-tag their logger: `logger.With("component", "executor")`.

**Level guidelines**: `Info` for normal flow, `Warn` for retryable/non-fatal failures (e.g., credit deduction failed), `Error` for unexpected errors requiring investigation.

## Request ID

### Inbound (API server)

`middleware.RequestID()` runs on every request:
- Preserves incoming `X-Request-ID` if present (e.g., from a load balancer).
- Generates UUID v4 if absent.
- Stored in context via `requestid.WithRequestID()`.
- Returned in response header.

### Outbound (executor)

Each job execution generates a new UUID v4, set as `X-Request-ID` on the outbound HTTP request. Customers can use it to correlate their logs with Fliq's execution history.

## Prometheus metrics

All metrics registered in `internal/metrics/metrics.go`, under the `scheduler` namespace:

### Worker metrics
| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `job_pickup_latency_seconds` | Histogram | — | Time from job creation to claim |
| `job_execution_duration_seconds` | Histogram | `status` | Execution time (success/failure) |
| `worker_jobs_in_flight` | Gauge | — | Currently executing jobs |
| `jobs_completed_total` | Counter | `outcome` | success/retry/failed counts |

### Reaper metrics
| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `reaper_rescued_total` | Counter | `action` | rescheduled/failed counts |
| `reaper_cycle_duration_seconds` | Histogram | — | Time per reaper scan |

### HTTP metrics
| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `http_request_duration_seconds` | Histogram | `method`, `path`, `status` | API response latency |
| `http_requests_total` | Counter | `method`, `path`, `status` | Request counts |

### Webhook metrics
| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `webhook_deliveries_total` | Counter | `outcome` | success/failure |
| `webhook_duration_seconds` | Histogram | — | Delivery latency |

## Health checks

Served by the metrics server (default `:9090`):

| Endpoint | Purpose | K8s probe |
|----------|---------|-----------|
| `/healthz` | Liveness — is the process running | `livenessProbe` |
| `/readyz` | Readiness — can it serve traffic (DB reachable) | `readinessProbe` |
| `/metrics` | Prometheus scrape endpoint | — |

## Source files

- `internal/log/handler.go` — `ContextHandler`, request ID extraction
- `internal/metrics/metrics.go` — all Prometheus metric definitions, metrics server
- `internal/requestid/requestid.go` — context helpers
- `internal/http/middleware/requestid.go` — inbound request ID middleware
- `internal/http/middleware/metrics.go` — HTTP metrics middleware
- `internal/health/checker.go` — health check handlers
