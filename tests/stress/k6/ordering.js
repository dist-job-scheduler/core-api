// Strict per-buffer ordering (issue #30).
//
// Push N items in order. Force an early item to fail once so it must retry
// (with backoff) — under strict ordering, NO later item may be delivered until
// that item finally succeeds. Assert the acked deliveries arrive in exact push
// order (0,1,2,...,N-1), and that the retried item really did retry.
//
// Light by default (N=5). The retried item adds one ~30s backoff wait.

import { check, sleep } from "k6";
import { mintJWT } from "./lib/auth.js";
import * as buffers from "./lib/buffers.js";
import * as sink from "./lib/sink.js";
import { SINK_TARGET_URL, USER_ID, stamp } from "./lib/config.js";

const N = parseInt(__ENV.ORDER_ITEMS || "5", 10);
const FAIL_INDEX = parseInt(__ENV.ORDER_FAIL_INDEX || "1", 10); // which item retries once

export const options = { vus: 1, iterations: 1, thresholds: { checks: ["rate==1.0"] } };

function idxOf(name, token) {
  return parseInt(token.slice(`${name}-i`.length), 10);
}

export default function () {
  const token = mintJWT(USER_ID);
  sink.reset();

  const name = stamp("ordering");
  const buf = buffers.createBuffer(token, {
    name,
    url: SINK_TARGET_URL,
    rate_limit: 100, // high: ordering, not rate, is what we're testing here
    max_retries: 3,
    backoff: "exponential",
  });

  const ids = [];
  for (let i = 0; i < N; i++) {
    const t = `${name}-i${String(i).padStart(3, "0")}`;
    if (i === FAIL_INDEX) sink.setPolicy(t, { mode: "fail_n", n: 1 }); // one 500 then 200
    const item = buffers.pushItem(token, buf.id, { headers: { "X-Stress-Token": t }, body: t });
    ids.push(item.id);
  }

  buffers.pollItemsUntilTerminal(token, buf.id, ids, { timeoutMs: 120000, intervalMs: 1000 });

  const all = sink.deliveries("").sort((a, b) => a.ts_ms - b.ts_ms);
  const ackedIdx = all.filter((d) => d.status >= 200 && d.status < 300).map((d) => idxOf(name, d.token));
  const failedForFailIndex = all.filter(
    (d) => idxOf(name, d.token) === FAIL_INDEX && d.status >= 500,
  ).length;

  const expected = Array.from({ length: N }, (_, i) => i);
  const inOrder = JSON.stringify(ackedIdx) === JSON.stringify(expected);

  console.log(`ordering: acked sequence = [${ackedIdx.join(",")}] expected [${expected.join(",")}] retried=${failedForFailIndex}`);

  check(null, {
    "acked deliveries arrive in exact push order": () => inOrder,
    "the forced-fail item actually retried": () => failedForFailIndex >= 1,
  });
}
