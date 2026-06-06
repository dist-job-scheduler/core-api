// Crash recovery + at-most-once, driven by scripts/crash-run.sh.
//
//   PHASE=load   : sink holds every connection open (sleep), buffer pushes N
//                  items so they all sit in `running`. The orchestrator then
//                  SIGKILLs the scheduler mid-flight and restarts it.
//   PHASE=verify : flip the sink fast, wait for the buffer reaper to rescue the
//                  orphaned `running` items, and assert every item reaches
//                  `completed` (no loss). Separately REPORT duplicate target
//                  executions — a crash after delivery-before-complete re-fires
//                  the target, so this characterizes the at-least-once edge.
//
// Hard gate: recovery (no item lost). Duplicate acks are reported, not gated —
// they reveal whether "exactly-once" holds under crash.

import { check, sleep } from "k6";
import { Counter } from "k6/metrics";
import { mintJWT } from "./lib/auth.js";
import * as buffers from "./lib/buffers.js";
import * as sink from "./lib/sink.js";
import { SINK_TARGET_URL, USER_ID } from "./lib/config.js";

const PHASE = __ENV.PHASE;
const RUN = __ENV.CRASH_RUN;
const N = parseInt(__ENV.CRASH_ITEMS || "10", 10);
const SLEEP_MS = parseInt(__ENV.CRASH_SLEEP_MS || "8000", 10);

export const options = { vus: 1, iterations: 1, thresholds: { checks: ["rate==1.0"] } };

const duplicateAcks = new Counter("duplicate_acks");

export default function () {
  if (!RUN) throw new Error("CRASH_RUN must be set");
  const token = mintJWT(USER_ID);
  if (PHASE === "load") return loadPhase(token);
  if (PHASE === "verify") return verifyPhase(token);
  throw new Error("PHASE must be 'load' or 'verify'");
}

function loadPhase(token) {
  sink.reset();
  // Every delivery hangs for SLEEP_MS so items stay `running` long enough to be
  // caught mid-flight by the kill.
  sink.setDefault({ mode: "sleep", sleep_ms: SLEEP_MS });

  const buf = buffers.createBuffer(token, {
    name: RUN,
    url: SINK_TARGET_URL,
    rate_limit: N, // claim them all in one cycle -> all in-flight together
    max_retries: 5,
  });

  for (let i = 0; i < N; i++) {
    const t = `${RUN}-item-${i}`;
    buffers.pushItem(token, buf.id, { headers: { "X-Stress-Token": t }, body: t });
  }
  console.log(`crash/load: pushed ${N} items to buffer ${buf.id}, sink sleeping ${SLEEP_MS}ms`);
}

function verifyPhase(token) {
  sink.setDefault({ mode: "ok" }); // recovered redeliveries complete fast

  const buf = buffers.findBufferByName(token, RUN);
  check(buf, { "buffer survived restart": (b) => b !== null });
  if (!buf) return;

  // Wait for the reaper (heartbeat_timeout 30s + interval 30s) to rescue the
  // orphaned items and the drainer to finish them.
  const deadline = Date.now() + 150000;
  let items = [];
  let completed = 0;
  while (Date.now() < deadline) {
    items = buffers.listAllItems(token, buf.id);
    completed = items.filter((it) => it.status === "completed").length;
    const terminal = items.filter((it) => buffers.isTerminal(it)).length;
    if (items.length >= N && terminal === items.length) break;
    sleep(2);
  }

  // Duplicate analysis from the sink's record.
  const all = sink.deliveries("");
  const byToken = {};
  for (const d of all) (byToken[d.token] = byToken[d.token] || []).push(d);
  let dupDelivered = 0;
  let dupAck = 0;
  for (let i = 0; i < N; i++) {
    const ds = byToken[`${RUN}-item-${i}`] || [];
    const acks = ds.filter((d) => d.status >= 200 && d.status < 300).length;
    if (ds.length > 1) dupDelivered++;
    if (acks > 1) {
      dupAck++;
      duplicateAcks.add(acks - 1);
    }
  }

  console.log(
    `crash/verify: items=${N} completed=${completed} ` +
      `dupDelivered=${dupDelivered} dupAck=${dupAck} ` +
      `(dupAck>0 => at-least-once: target re-fired on recovery)`,
  );

  // Hard gate: recovery / no loss.
  check(null, {
    "all items recovered to completed (no loss)": () => completed === N,
  });
}
