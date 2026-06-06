// Completeness / no-loss: push N items to a healthy buffer; every one must
// reach `completed`, and the sink must have received each exactly once (no
// loss, no spurious duplicates) with a 2xx ack.
//
// Light by default (ITEMS=10). Override: ITEMS=200 make completeness

import { check } from "k6";
import { mintJWT } from "./lib/auth.js";
import * as buffers from "./lib/buffers.js";
import * as sink from "./lib/sink.js";
import { SINK_TARGET_URL, USER_ID, ITEMS, RATE_LIMIT, stamp } from "./lib/config.js";

export const options = {
  vus: 1,
  iterations: 1,
  thresholds: { checks: ["rate==1.0"] },
};

export default function () {
  const token = mintJWT(USER_ID);
  sink.reset();

  const name = stamp("completeness");
  const buf = buffers.createBuffer(token, {
    name,
    url: SINK_TARGET_URL,
    rate_limit: RATE_LIMIT,
    max_retries: 3,
  });

  // Push N items, each carrying a unique token.
  const ids = [];
  const tokens = [];
  for (let i = 0; i < ITEMS; i++) {
    const itemToken = `${name}-item-${i}`;
    tokens.push(itemToken);
    const item = buffers.pushItem(token, buf.id, {
      headers: { "X-Stress-Token": itemToken },
      body: itemToken,
    });
    ids.push(item.id);
  }

  const final = buffers.pollItemsUntilTerminal(token, buf.id, ids, {
    timeoutMs: 90000,
    intervalMs: 500,
  });

  const completed = Object.values(final).filter((it) => it.status === "completed").length;
  check(null, {
    "all items reached terminal": () => Object.keys(final).length === ITEMS,
    "all items completed": () => completed === ITEMS,
  });

  // No loss, no duplicate acked delivery: each token delivered, exactly one 2xx.
  let missing = 0;
  let multiAck = 0;
  for (const t of tokens) {
    const ds = sink.deliveries(t);
    const acks = ds.filter((d) => d.status >= 200 && d.status < 300).length;
    if (acks === 0) missing++;
    if (acks > 1) multiAck++;
  }
  check(null, {
    "no item lost (every token acked once)": () => missing === 0,
    "no item acked more than once": () => multiAck === 0,
  });

  console.log(`completeness: ${completed}/${ITEMS} completed, missing=${missing}, multiAck=${multiAck}`);
}
