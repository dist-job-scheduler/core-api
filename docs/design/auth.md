# Auth

Authentication supports two paths: JWT tokens (for dashboard sessions via Clerk) and API tokens (for programmatic access). Authorization is user-scoped — every query filters by `user_id`.

## JWT authentication (Clerk)

- **Production**: RS256 via JWKS. The middleware fetches Clerk's public keys from `CLERK_JWKS_URL` and caches them with a 15-minute refresh interval. JWT subject (`sub`) becomes the `userID`.
- **Local dev**: HS256 fallback when `CLERK_JWKS_URL` is empty. Uses `JWT_SECRET` (min 32 chars). Intended for development only.

> **Why JWKS cache with 15-min refresh:** Clerk rotates keys periodically. Caching avoids a network call per request while picking up rotations within minutes. If the fetch fails, the request is rejected (no stale key fallback) — we prefer a hard failure over accepting a potentially revoked key.

## API token authentication

- Format: `fliq_sk_XXXXXXXX` (prefix identifies the token type).
- Stored as **SHA-256 hash** in the `api_tokens` table — the plaintext is never persisted.
- `last_used_at` updated asynchronously (`go tokenRepo.UpdateLastUsed(...)`) to avoid adding latency to every API call.

The auth middleware distinguishes the two paths by token prefix: `eyJ` (base64 JWT header) routes to JWT validation, `fliq_sk_` routes to token lookup.

## EnsureUser middleware

Runs after auth on all protected routes. Two operations:

1. **Upsert user**: inserts the Clerk user ID into the `users` table if it doesn't exist. This handles first-time users without a separate registration flow.
2. **Ensure credits**: creates a `user_credits` row with the free-plan defaults if missing. Prevents FK constraint violations when creating jobs.

> **Why upsert on every request:** Clerk handles user registration. We don't get a webhook for "user created" — we discover new users when they first hit our API. The upsert is idempotent and cheap (single `INSERT ... ON CONFLICT DO NOTHING`).

## User-scoped authorization

Every repository query includes `user_id` in the WHERE clause:

```sql
SELECT ... FROM jobs WHERE id = $1 AND user_id = $2
```

A resource belonging to another user returns `domain.ErrJobNotFound` → HTTP 404, **never 403**.

> **Why 404 instead of 403:** Returning 403 reveals that the resource exists. 404 leaks nothing — the attacker can't distinguish "wrong user" from "doesn't exist".

## Security headers

Applied globally via `middleware.Security()` on the root router (not per-route group), so they cover 404s, 401s, and error responses too:

- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Strict-Transport-Security: max-age=63072000; includeSubDomains`
- `Referrer-Policy: strict-origin-when-cross-origin`
- `Permissions-Policy: camera=(), microphone=(), geolocation=()`

## Source files

- `internal/http/middleware/auth.go` — JWT and API token validation
- `internal/http/middleware/ensure_user.go` — user upsert, credit initialization
- `internal/http/middleware/security.go` — security headers
- `config/config.go` — `CLERK_JWKS_URL`, `JWT_SECRET`
