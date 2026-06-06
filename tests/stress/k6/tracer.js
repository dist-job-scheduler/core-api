// Tracer bullet: the smallest end-to-end proof that the harness works.
//
// Push ONE item to a buffer pointed at the sink, wait for it to reach a
// terminal state, and assert:
//   1. the item completed
//   2. the sink received exactly one delivery of that item's token
//
// If this passes, the whole pipe is wired: auth -> create buffer -> push ->
// drainer executes -> sink records -> introspection -> assertions.

import { check } from "k6";
import { mintJWT } from "./lib/auth.js";
import * as buffers from "./lib/buffers.js";
import * as sink from "./lib/sink.js";
import { SINK_TARGET_URL, USER_ID, stamp } from "./lib/config.js";

export const options = {
  vus: 1,
  iterations: 1,
  // Any failed check fails the whole run (non-zero exit) — this is the CI gate.
  thresholds: { checks: ["rate==1.0"] },
};

export default function () {
  const token = mintJWT(USER_ID);
  sink.reset();

  const name = stamp("tracer");
  const buf = buffers.createBuffer(token, {
    name,
    url: SINK_TARGET_URL,
    rate_limit: 5,
    max_retries: 3,
  });

  const itemToken = `${name}-item-1`;
  const item = buffers.pushItem(token, buf.id, {
    headers: { "X-Stress-Token": itemToken },
    body: itemToken,
  });

  const final = buffers.pollUntilTerminal(token, buf.id, item.id, {
    timeoutMs: 30000,
    intervalMs: 500,
  });

  check(final, {
    "item reached completed": (it) => it.status === "completed",
  });

  const deliveries = sink.deliveries(itemToken);
  check(deliveries, {
    "sink received exactly one delivery": (d) => d.length === 1,
    "delivery was acknowledged 2xx": (d) =>
      d.length > 0 && d[0].status >= 200 && d[0].status < 300,
  });
}
