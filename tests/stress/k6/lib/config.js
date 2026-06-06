// Central config — everything is env-overridable so the same specs can run
// against the local compose env or, later, a remote BASE_URL (staging/prod).
//
// Defaults are deliberately LIGHT — this box runs other workloads. Crank the
// numbers (ITEMS, RATE_LIMIT, VUS) via env on a beefier machine.

function env(name, fallback) {
  const v = __ENV[name];
  return v === undefined || v === "" ? fallback : v;
}

function envInt(name, fallback) {
  const v = env(name, undefined);
  return v === undefined ? fallback : parseInt(v, 10);
}

// core-api public API (what k6 drives). In compose: the server service.
export const BASE_URL = env("BASE_URL", "http://server:8080");

// Sink introspection URL (as seen by k6).
export const SINK_URL = env("SINK_URL", "http://sink:9000");

// Sink ingest URL (as seen by the core-api scheduler when it executes items).
// In compose this is the same host; against a remote BASE_URL it must be a
// sink reachable FROM that deployment.
export const SINK_TARGET_URL = env("SINK_TARGET_URL", "http://sink:9000/ingest");

// Auth: HS256 JWT signed with the secret the server is configured with.
export const JWT_SECRET = env("JWT_SECRET", "stress-harness-local-secret-0123456789abcdef");
export const USER_ID = env("USER_ID", "user_stress_local");

// Light defaults for the load-shaped specs.
export const ITEMS = envInt("ITEMS", 10);
export const RATE_LIMIT = envInt("RATE_LIMIT", 5);

// A unique-ish, deterministic-per-run label (k6 has Date.now()).
export function stamp(prefix) {
  return `${prefix}-${Date.now()}`;
}
