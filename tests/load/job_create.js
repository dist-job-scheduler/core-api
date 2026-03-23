import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

// Custom metrics
const errorRate = new Rate("errors");
const createDuration = new Trend("create_duration", true);

// Configuration — override via environment variables:
//   K6_BASE_URL=https://job.enkiduck.com k6 run job_create.js
const BASE_URL = __ENV.K6_BASE_URL || "http://localhost:8080";
const AUTH_TOKEN = __ENV.K6_AUTH_TOKEN || "";

export const options = {
  stages: [
    { duration: "30s", target: 20 },  // ramp up
    { duration: "1m", target: 50 },   // sustain
    { duration: "1m", target: 100 },  // peak
    { duration: "30s", target: 0 },   // ramp down
  ],
  thresholds: {
    http_req_duration: ["p(95)<500", "p(99)<1000"],
    errors: ["rate<0.05"],
  },
};

export default function () {
  const scheduledAt = new Date(Date.now() + 3600_000).toISOString(); // 1h from now

  const payload = JSON.stringify({
    url: "https://httpbin.org/post",
    method: "POST",
    scheduled_at: scheduledAt,
    timeout_seconds: 30,
    headers: { "X-Test": "load-test" },
  });

  const params = {
    headers: {
      "Content-Type": "application/json",
      ...(AUTH_TOKEN ? { Authorization: `Bearer ${AUTH_TOKEN}` } : {}),
    },
  };

  const start = Date.now();
  const res = http.post(`${BASE_URL}/jobs`, payload, params);
  createDuration.add(Date.now() - start);

  const ok = check(res, {
    "status is 201": (r) => r.status === 201,
    "has job id": (r) => {
      try {
        return JSON.parse(r.body).id !== undefined;
      } catch {
        return false;
      }
    },
  });

  errorRate.add(!ok);
  sleep(0.1); // 100ms think time
}
