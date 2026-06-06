// Rate-limit adherence — per-second token bucket (issue #31).
//
// With the token bucket, rate_limit means "max deliveries per second" (burst up
// to rate_limit). Push a burst at a healthy sink and verify:
//   - sustained throughput stays at/below rate_limit per second
//   - the limiter actually slowed delivery (without it, strict-ordering drains
//     the whole queue in well under a second)
//   - no per-second window blows past the burst bound (capacity + 1s refill)
//
// Light by default. Override RATE_LIMIT / RATE_ITEMS / WINDOW_MS.

import { check } from "k6";
import { mintJWT } from "./lib/auth.js";
import * as buffers from "./lib/buffers.js";
import * as sink from "./lib/sink.js";
import { SINK_TARGET_URL, USER_ID, RATE_LIMIT, stamp } from "./lib/config.js";

const WINDOW_MS = parseInt(__ENV.WINDOW_MS || "1000", 10);
const ITEMS = parseInt(__ENV.RATE_ITEMS || String(RATE_LIMIT * 6), 10);
const RATE_TOLERANCE = parseFloat(__ENV.RATE_TOLERANCE || "0.35");

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
    const t = `${name}-item-${i}`;
    const item = buffers.pushItem(token, buf.id, { headers: { "X-Stress-Token": t }, body: t });
    ids.push(item.id);
  }

  buffers.pollItemsUntilTerminal(token, buf.id, ids, { timeoutMs: 180000, intervalMs: 500 });

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
  const spanSec = spanMs / 1000 || 1;
  const avgPerSec = all.length / spanSec;

  // Throttling proof: at rate_limit/sec, ITEMS deliveries take ~ITEMS/rate_limit
  // seconds. Without the limiter, strict-ordering drains the queue in << 1s.
  const expectedSec = ITEMS / RATE_LIMIT;

  console.log(
    `rate: items=${ITEMS} rate_limit=${RATE_LIMIT}/s delivered=${all.length} ` +
      `spanMs=${spanMs} avgPerSec=${avgPerSec.toFixed(2)} maxWindow=${maxWindow} ` +
      `expected~${expectedSec.toFixed(1)}s`,
  );
  console.log(`rate: per-window counts = [${counts.join(", ")}]`);

  check(null, {
    "all items delivered": () => all.length === ITEMS,
    "sustained throughput <= rate_limit/sec": () => avgPerSec <= RATE_LIMIT * (1 + RATE_TOLERANCE),
    "limiter actually throttled (span ~ items/rate)": () => spanSec >= expectedSec * 0.6,
    "no window exceeds burst bound (capacity + 1s refill)": () => maxWindow <= RATE_LIMIT * 2 + 1,
  });
}
