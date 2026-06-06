// Thin black-box driver for the core-api buffers HTTP API. No knowledge of
// internals — just the public REST surface. If the implementation changes
// shape, only this file moves.

import http from "k6/http";
import { sleep, fail } from "k6";
import { BASE_URL } from "./config.js";

function authHeaders(token) {
  return {
    Authorization: `Bearer ${token}`,
    "Content-Type": "application/json",
  };
}

function expect(res, want, what) {
  if (res.status !== want) {
    fail(`${what}: expected ${want}, got ${res.status} — ${res.body}`);
  }
  return res.json();
}

export function createBuffer(token, input) {
  const res = http.post(`${BASE_URL}/buffers`, JSON.stringify(input), {
    headers: authHeaders(token),
  });
  return expect(res, 201, "createBuffer");
}

export function pushItem(token, bufferId, { body, headers } = {}) {
  const payload = {};
  if (body !== undefined) payload.body = body;
  if (headers !== undefined) payload.headers = headers;
  const res = http.post(
    `${BASE_URL}/buffers/${bufferId}/items`,
    JSON.stringify(payload),
    { headers: authHeaders(token) },
  );
  return expect(res, 201, "pushItem");
}

export function getItem(token, bufferId, itemId) {
  const res = http.get(`${BASE_URL}/buffers/${bufferId}/items/${itemId}`, {
    headers: authHeaders(token),
  });
  return expect(res, 200, "getItem");
}

export function listBuffers(token, { limit = 100, cursor = "" } = {}) {
  const q = `?limit=${limit}${cursor ? `&cursor=${cursor}` : ""}`;
  const res = http.get(`${BASE_URL}/buffers${q}`, { headers: authHeaders(token) });
  return expect(res, 200, "listBuffers");
}

// Find a buffer by exact name (paginates). Returns the buffer or null.
export function findBufferByName(token, name) {
  let cursor = "";
  for (let page = 0; page < 50; page++) {
    const { buffers: page_, next_cursor } = listBuffers(token, { cursor });
    const hit = (page_ || []).find((b) => b.name === name);
    if (hit) return hit;
    if (!next_cursor) return null;
    cursor = next_cursor;
  }
  return null;
}

// List all items in a buffer (paginates). Returns the full array.
export function listAllItems(token, bufferId) {
  const out = [];
  let cursor = "";
  for (let page = 0; page < 200; page++) {
    const q = `?limit=100${cursor ? `&cursor=${cursor}` : ""}`;
    const res = http.get(`${BASE_URL}/buffers/${bufferId}/items${q}`, {
      headers: authHeaders(token),
    });
    const body = expect(res, 200, "listItems");
    out.push(...(body.items || []));
    if (!body.next_cursor) break;
    cursor = body.next_cursor;
  }
  return out;
}

const TERMINAL = ["completed", "failed"];

export function isTerminal(item) {
  return TERMINAL.includes(item.status);
}

// Poll a single item until it reaches a terminal state or the timeout elapses.
export function pollUntilTerminal(token, bufferId, itemId, { timeoutMs = 30000, intervalMs = 500 } = {}) {
  const deadline = Date.now() + timeoutMs;
  let item = getItem(token, bufferId, itemId);
  while (!isTerminal(item) && Date.now() < deadline) {
    sleep(intervalMs / 1000);
    item = getItem(token, bufferId, itemId);
  }
  return item;
}

// Poll many items until all are terminal (or timeout). Returns the final items
// keyed by id. Polls the per-item endpoint; fine for the light item counts here.
export function pollItemsUntilTerminal(token, bufferId, itemIds, { timeoutMs = 60000, intervalMs = 500 } = {}) {
  const deadline = Date.now() + timeoutMs;
  const final = {};
  let pending = itemIds.slice();
  while (pending.length > 0 && Date.now() < deadline) {
    const still = [];
    for (const id of pending) {
      const item = getItem(token, bufferId, id);
      if (isTerminal(item)) final[id] = item;
      else still.push(id);
    }
    pending = still;
    if (pending.length > 0) sleep(intervalMs / 1000);
  }
  // Record whatever is left as its last-seen state.
  for (const id of pending) final[id] = getItem(token, bufferId, id);
  return final;
}

export function pauseBuffer(token, bufferId) {
  const res = http.post(`${BASE_URL}/buffers/${bufferId}/pause`, null, {
    headers: authHeaders(token),
  });
  if (res.status !== 204) fail(`pauseBuffer: expected 204, got ${res.status} — ${res.body}`);
}

export function resumeBuffer(token, bufferId) {
  const res = http.post(`${BASE_URL}/buffers/${bufferId}/resume`, null, {
    headers: authHeaders(token),
  });
  if (res.status !== 204) fail(`resumeBuffer: expected 204, got ${res.status} — ${res.body}`);
}
