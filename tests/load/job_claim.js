import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

// Tests job claiming throughput by creating many pending jobs, then polling
// the list endpoint to observe how quickly they move to running/completed.

const errorRate = new Rate("errors");
const listDuration = new Trend("list_duration", true);

const BASE_URL = __ENV.K6_BASE_URL || "http://localhost:8080";
const AUTH_TOKEN = __ENV.K6_AUTH_TOKEN || "";

export const options = {
  scenarios: {
    // Phase 1: Seed pending jobs (10 VUs for 30s ≈ 300 jobs)
    seed: {
      executor: "constant-vus",
      vus: 10,
      duration: "30s",
      exec: "seedJobs",
    },
    // Phase 2: Monitor claim throughput (starts after seed)
    monitor: {
      executor: "constant-vus",
      vus: 5,
      duration: "2m",
      startTime: "35s",
      exec: "monitorJobs",
    },
  },
  thresholds: {
    errors: ["rate<0.1"],
    list_duration: ["p(95)<1000"],
  },
};

function headers() {
  return {
    "Content-Type": "application/json",
    ...(AUTH_TOKEN ? { Authorization: `Bearer ${AUTH_TOKEN}` } : {}),
  };
}

export function seedJobs() {
  // Schedule jobs to fire immediately so the scheduler picks them up.
  const scheduledAt = new Date().toISOString();

  const payload = JSON.stringify({
    url: "https://httpbin.org/delay/1", // 1s response
    method: "GET",
    scheduled_at: scheduledAt,
    timeout_seconds: 10,
    max_retries: 0,
  });

  const res = http.post(`${BASE_URL}/jobs`, payload, { headers: headers() });
  const ok = check(res, { "seed: status 201": (r) => r.status === 201 });
  errorRate.add(!ok);
  sleep(0.1);
}

export function monitorJobs() {
  const start = Date.now();
  const res = http.get(`${BASE_URL}/jobs?status=running&limit=100`, {
    headers: headers(),
  });
  listDuration.add(Date.now() - start);

  const ok = check(res, { "monitor: status 200": (r) => r.status === 200 });
  errorRate.add(!ok);

  if (res.status === 200) {
    try {
      const body = JSON.parse(res.body);
      console.log(
        `running=${body.jobs.length} next_cursor=${body.next_cursor || "null"}`
      );
    } catch {
      // ignore parse errors
    }
  }

  sleep(2);
}
