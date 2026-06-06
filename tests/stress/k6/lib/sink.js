// Client for the controllable sink service — the target the buffers point at.
// k6 uses this to reset state, configure per-token response policies, and read
// back the deliveries the scheduler actually made.

import http from "k6/http";
import { fail } from "k6";
import { SINK_URL } from "./config.js";

export function reset() {
  const res = http.post(`${SINK_URL}/control/reset`);
  if (res.status !== 200) fail(`sink.reset: got ${res.status} — ${res.body}`);
}

// policy: { mode: "ok"|"fail_n"|"always_fail"|"sleep"|"status", n, sleep_ms, status, retry_after }
export function setPolicy(token, policy) {
  const res = http.post(
    `${SINK_URL}/control/policy`,
    JSON.stringify({ token, ...policy }),
    { headers: { "Content-Type": "application/json" } },
  );
  if (res.status !== 200) fail(`sink.setPolicy: got ${res.status} — ${res.body}`);
}

// Set the global default policy (applied to tokens with no explicit policy).
export function setDefault(policy) {
  const res = http.post(`${SINK_URL}/control/default`, JSON.stringify(policy), {
    headers: { "Content-Type": "application/json" },
  });
  if (res.status !== 200) fail(`sink.setDefault: got ${res.status} — ${res.body}`);
}

// All recorded deliveries for a token, oldest first: [{ token, ts_ms, status, method }]
export function deliveries(token) {
  const res = http.get(`${SINK_URL}/deliveries?token=${encodeURIComponent(token)}`);
  if (res.status !== 200) fail(`sink.deliveries: got ${res.status} — ${res.body}`);
  return res.json("deliveries");
}

// Aggregate stats across all tokens.
export function stats() {
  const res = http.get(`${SINK_URL}/stats`);
  if (res.status !== 200) fail(`sink.stats: got ${res.status} — ${res.body}`);
  return res.json();
}
