// Rate-limit adherence (characterization).
//
// The drainer claims up to `rate_limit` items per poll cycle (default poll =
// 1s), so the *advertised* "rate-limited endpoint" really means ~rate_limit
// dispatches per poll interval. This spec pushes a burst at a healthy sink,
// then reconstructs the per-second dispatch distribution from the sink's
// delivery timestamps and:
//   - reports the real distribution (so the human sees actual behavior)
//   - asserts rate_limit acts as an upper bound (no window blows past it)
//
// Light by default. Override RATE_LIMIT / WINDOW_MS / RATE_ITEMS via env.

import { check } from "k6";
import { mintJWT } from "./lib/auth.js";
import * as buffers from "./lib/buffers.js";
import * as sink from "./lib/sink.js";
import { SINK_TARGET_URL, USER_ID, RATE_LIMIT, stamp } from "./lib/config.js";

const WINDOW_MS = parseInt(__ENV.WINDOW_MS || "1000", 10); // = drainer poll interval
const ITEMS = parseInt(__ENV.RATE_ITEMS || String(RATE_LIMIT * 4), 10);
// Boundary jitter can split/merge cycles across a window edge; allow a little slack.
const SLACK = parseInt(__ENV.RATE_SLACK || "1", 10);

export const options = { vus: 1, iterations: 1, thresholds: { checks: ["rate==1.0"] } };

export default function () {
  const token = mintJWT(USER_ID);
  sink.reset();

  const name = stamp("rate");
  const buf = buffers.createBuffer(token, {
    name,
    url: SINK_TARGET_URL,
    rate_limit: RATE_LIMIT,
    max_retries: 3,
  });

  const ids = [];
  for (let i = 0; i < ITEMS; i++) {
    const itemToken = `${name}-item-${i}`;
    const item = buffers.pushItem(token, buf.id, {
      headers: { "X-Stress-Token": itemToken },
      body: itemToken,
    });
    ids.push(item.id);
  }

  buffers.pollItemsUntilTerminal(token, buf.id, ids, { timeoutMs: 120000, intervalMs: 500 });

  // Reconstruct dispatch timeline from acked deliveries.
  const all = sink.deliveries("").filter((d) => d.status >= 200 && d.status < 300);
  all.sort((a, b) => a.ts_ms - b.ts_ms);

  const t0 = all.length ? all[0].ts_ms : 0;
  const windows = {};
  for (const d of all) {
    const w = Math.floor((d.ts_ms - t0) / WINDOW_MS);
    windows[w] = (windows[w] || 0) + 1;
  }
  const counts = Object.values(windows);
  const maxWindow = counts.length ? Math.max(...counts) : 0;
  const spanMs = all.length ? all[all.length - 1].ts_ms - t0 : 0;
  const avgPerWindow = spanMs > 0 ? (all.length / (spanMs / WINDOW_MS)).toFixed(2) : all.length;

  console.log(
    `rate: items=${ITEMS} rate_limit=${RATE_LIMIT} window=${WINDOW_MS}ms ` +
      `delivered=${all.length} span=${spanMs}ms maxPerWindow=${maxWindow} avgPerWindow=${avgPerWindow}`,
  );
  console.log(`rate: per-window counts = [${counts.join(", ")}]`);

  check(null, {
    "all items dispatched": () => all.length === ITEMS,
    "rate_limit is an upper bound per window": () => maxWindow <= RATE_LIMIT + SLACK,
  });
}
