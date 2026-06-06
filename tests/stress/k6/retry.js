// Retry & backoff.
//
// Configure the sink to fail an item's first RETRIES deliveries (500), then
// succeed. Assert the drainer retries the right number of times, the item
// finally completes, and the spacing between deliveries reflects the backoff
// (base 30s, exponential with jitter — so the first gap lands in ~[18s,45s]).
//
// Default RETRIES=1 keeps this ~30-40s. On a beefier box: RETRIES=2 make retry
// (≈90s) also checks that gaps GROW (exponential), not just that one exists.

import { check } from "k6";
import { mintJWT } from "./lib/auth.js";
import * as buffers from "./lib/buffers.js";
import * as sink from "./lib/sink.js";
import { SINK_TARGET_URL, USER_ID, stamp } from "./lib/config.js";

const RETRIES = parseInt(__ENV.RETRIES || "1", 10);

export const options = { vus: 1, iterations: 1, thresholds: { checks: ["rate==1.0"] } };

export default function () {
  const token = mintJWT(USER_ID);
  sink.reset();

  const name = stamp("retry");
  const buf = buffers.createBuffer(token, {
    name,
    url: SINK_TARGET_URL,
    rate_limit: 5,
    max_retries: RETRIES + 2, // headroom so the success isn't cut off
    backoff: "exponential",
  });

  const itemToken = `${name}-item-0`;
  // Fail the first RETRIES deliveries, then 200.
  sink.setPolicy(itemToken, { mode: "fail_n", n: RETRIES });

  const item = buffers.pushItem(token, buf.id, {
    headers: { "X-Stress-Token": itemToken },
    body: itemToken,
  });

  const final = buffers.pollUntilTerminal(token, buf.id, item.id, {
    timeoutMs: (RETRIES + 1) * 60000 + 30000,
    intervalMs: 1000,
  });

  const ds = sink.deliveries(itemToken).sort((a, b) => a.ts_ms - b.ts_ms);
  const fails = ds.filter((d) => d.status >= 500).length;
  const acks = ds.filter((d) => d.status >= 200 && d.status < 300).length;

  const gaps = [];
  for (let i = 1; i < ds.length; i++) gaps.push(ds[i].ts_ms - ds[i - 1].ts_ms);

  console.log(
    `retry: status=${final.status} retry_count=${final.retry_count} ` +
      `deliveries=${ds.length} fails=${fails} acks=${acks} gaps_ms=[${gaps.join(", ")}]`,
  );

  check(null, {
    "item eventually completed": () => final.status === "completed",
    "retried exactly RETRIES times": () => final.retry_count === RETRIES,
    "delivered RETRIES failures + 1 success": () =>
      fails === RETRIES && acks === 1 && ds.length === RETRIES + 1,
    "first retry waited out the backoff (~30s band)": () =>
      gaps.length >= 1 && gaps[0] >= 18000 && gaps[0] <= 50000,
    // Only meaningful with >=2 gaps: exponential => later gaps grow.
    "backoff grows (exponential)": () =>
      gaps.length < 2 || gaps[gaps.length - 1] > gaps[0],
  });
}
