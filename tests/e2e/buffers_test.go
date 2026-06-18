//go:build e2e

package e2e_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// uniqueName returns a buffer name unique to this test run so the suite stays
// re-runnable against a persistent database (buffers have a unique (user, name)
// constraint).
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

// createBuffer creates a buffer pointing at the given URL and returns its id.
// It registers a t.Cleanup that deletes the buffer afterwards.
func createBuffer(t *testing.T, url string, extra map[string]any) string {
	t.Helper()
	body := map[string]any{
		"name":   uniqueName("e2e-buf"),
		"url":    url,
		"method": "POST",
	}
	for k, v := range extra {
		body[k] = v
	}
	resp := doJSON(t, http.MethodPost, "/buffers", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)
	id, ok := created["id"].(string)
	if !ok || id == "" {
		t.Fatal("expected non-empty buffer id")
	}
	t.Cleanup(func() {
		resp := doJSON(t, http.MethodDelete, "/buffers/"+id, nil)
		_ = resp.Body.Close()
	})
	return id
}

func TestE2E_CreateBuffer(t *testing.T) {
	id := createBuffer(t, "https://httpbin.org/post", map[string]any{
		"rate_limit":      5,
		"timeout_seconds": 10,
	})

	resp := doJSON(t, http.MethodGet, "/buffers/"+id, nil)
	expectStatus(t, resp, http.StatusOK)
	var got map[string]any
	readJSON(t, resp, &got)
	if got["rate_limit"] != float64(5) {
		t.Fatalf("rate_limit = %v, want 5", got["rate_limit"])
	}
	if got["method"] != "POST" {
		t.Fatalf("method = %v, want POST", got["method"])
	}
	if got["paused"] != false {
		t.Fatalf("paused = %v, want false", got["paused"])
	}
}

func TestE2E_CreateBuffer_Validation(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing url", map[string]any{"name": uniqueName("e2e-noval")}},
		{"missing name", map[string]any{"url": "https://httpbin.org/post"}},
		{"bad url", map[string]any{"name": uniqueName("e2e-badurl"), "url": "not-a-url"}},
		{"bad method", map[string]any{"name": uniqueName("e2e-badm"), "url": "https://httpbin.org/post", "method": "FETCH"}},
		{"rate_limit too high", map[string]any{"name": uniqueName("e2e-rl"), "url": "https://httpbin.org/post", "rate_limit": 100000}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := doJSON(t, http.MethodPost, "/buffers", tc.body)
			expectStatus(t, resp, http.StatusBadRequest)
			_ = resp.Body.Close()
		})
	}
}

func TestE2E_CreateBuffer_DuplicateName(t *testing.T) {
	name := uniqueName("e2e-buf-dup")
	body := map[string]any{"name": name, "url": "https://httpbin.org/post"}

	resp := doJSON(t, http.MethodPost, "/buffers", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)
	t.Cleanup(func() {
		resp := doJSON(t, http.MethodDelete, "/buffers/"+created["id"].(string), nil)
		_ = resp.Body.Close()
	})

	// Same name again → 409
	resp = doJSON(t, http.MethodPost, "/buffers", body)
	expectStatus(t, resp, http.StatusConflict)
	_ = resp.Body.Close()
}

func TestE2E_GetBuffer_NotFound(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/buffers/buf_does_not_exist", nil)
	expectStatus(t, resp, http.StatusNotFound)
	_ = resp.Body.Close()
}

func TestE2E_ListBuffers(t *testing.T) {
	createBuffer(t, "https://httpbin.org/post", nil)

	resp := doJSON(t, http.MethodGet, "/buffers?limit=10", nil)
	expectStatus(t, resp, http.StatusOK)
	var result map[string]any
	readJSON(t, resp, &result)
	buffers, ok := result["buffers"].([]any)
	if !ok {
		t.Fatal("expected buffers array in response")
	}
	if len(buffers) == 0 {
		t.Fatal("expected at least 1 buffer")
	}
}

func TestE2E_ListBuffers_InvalidLimit(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/buffers?limit=abc", nil)
	expectStatus(t, resp, http.StatusBadRequest)
	_ = resp.Body.Close()
}

func TestE2E_PushItem_GetAndList(t *testing.T) {
	id := createBuffer(t, "https://httpbin.org/post", nil)

	// Push an item.
	bodyStr := `{"hello":"world"}`
	resp := doJSON(t, http.MethodPost, "/buffers/"+id+"/items", map[string]any{"body": bodyStr})
	expectStatus(t, resp, http.StatusCreated)
	var item map[string]any
	readJSON(t, resp, &item)
	itemID, ok := item["id"].(string)
	if !ok || itemID == "" {
		t.Fatal("expected non-empty item id")
	}
	if item["buffer_id"] != id {
		t.Fatalf("buffer_id = %v, want %s", item["buffer_id"], id)
	}

	// Get the item back.
	resp = doJSON(t, http.MethodGet, "/buffers/"+id+"/items/"+itemID, nil)
	expectStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()

	// List items.
	resp = doJSON(t, http.MethodGet, "/buffers/"+id+"/items?limit=10", nil)
	expectStatus(t, resp, http.StatusOK)
	var list map[string]any
	readJSON(t, resp, &list)
	items, ok := list["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatal("expected at least 1 item in list")
	}
}

func TestE2E_ListItems_InvalidStatus(t *testing.T) {
	id := createBuffer(t, "https://httpbin.org/post", nil)
	resp := doJSON(t, http.MethodGet, "/buffers/"+id+"/items?status=bogus", nil)
	expectStatus(t, resp, http.StatusBadRequest)
	_ = resp.Body.Close()
}

func TestE2E_PushItem_BufferNotFound(t *testing.T) {
	resp := doJSON(t, http.MethodPost, "/buffers/buf_missing/items", map[string]any{"body": "x"})
	expectStatus(t, resp, http.StatusNotFound)
	_ = resp.Body.Close()
}

func TestE2E_PauseResumeBuffer(t *testing.T) {
	id := createBuffer(t, "https://httpbin.org/post", nil)

	// Pause → 204, then pausing again → 409.
	resp := doJSON(t, http.MethodPost, "/buffers/"+id+"/pause", nil)
	expectStatus(t, resp, http.StatusNoContent)
	_ = resp.Body.Close()

	resp = doJSON(t, http.MethodPost, "/buffers/"+id+"/pause", nil)
	expectStatus(t, resp, http.StatusConflict)
	_ = resp.Body.Close()

	// Confirm paused flag is reflected.
	resp = doJSON(t, http.MethodGet, "/buffers/"+id, nil)
	expectStatus(t, resp, http.StatusOK)
	var got map[string]any
	readJSON(t, resp, &got)
	if got["paused"] != true {
		t.Fatalf("paused = %v, want true", got["paused"])
	}

	// Resume → 204, then resuming again → 409.
	resp = doJSON(t, http.MethodPost, "/buffers/"+id+"/resume", nil)
	expectStatus(t, resp, http.StatusNoContent)
	_ = resp.Body.Close()

	resp = doJSON(t, http.MethodPost, "/buffers/"+id+"/resume", nil)
	expectStatus(t, resp, http.StatusConflict)
	_ = resp.Body.Close()
}

func TestE2E_DeleteBuffer(t *testing.T) {
	// Create directly (no cleanup helper) since this test deletes it.
	body := map[string]any{"name": uniqueName("e2e-buf-del"), "url": "https://httpbin.org/post"}
	resp := doJSON(t, http.MethodPost, "/buffers", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)
	id := created["id"].(string)

	resp = doJSON(t, http.MethodDelete, "/buffers/"+id, nil)
	expectStatus(t, resp, http.StatusNoContent)
	_ = resp.Body.Close()

	// Now gone.
	resp = doJSON(t, http.MethodGet, "/buffers/"+id, nil)
	expectStatus(t, resp, http.StatusNotFound)
	_ = resp.Body.Close()
}

func TestE2E_ReplayItem_NotReplayable(t *testing.T) {
	id := createBuffer(t, "https://httpbin.org/post", nil)

	// Push a fresh (pending) item; a not-yet-failed item is not replayable.
	resp := doJSON(t, http.MethodPost, "/buffers/"+id+"/items", map[string]any{"body": "x"})
	expectStatus(t, resp, http.StatusCreated)
	var item map[string]any
	readJSON(t, resp, &item)

	resp = doJSON(t, http.MethodPost, "/buffers/"+id+"/items/"+item["id"].(string)+"/replay", nil)
	expectStatus(t, resp, http.StatusConflict)
	_ = resp.Body.Close()
}

func TestE2E_BufferStats(t *testing.T) {
	id := createBuffer(t, "https://httpbin.org/post", nil)

	resp := doJSON(t, http.MethodGet, "/buffers/"+id+"/stats", nil)
	expectStatus(t, resp, http.StatusOK)
	var stats map[string]any
	readJSON(t, resp, &stats)
	for _, k := range []string{"pending", "running", "completed", "failed", "total", "success_rate"} {
		if _, ok := stats[k]; !ok {
			t.Fatalf("stats missing key %q: %v", k, stats)
		}
	}
}

// TestE2E_BufferDelivery_AndBilling is the end-to-end path that matters most:
// a pushed item is drained, delivered to the target, marked completed, and the
// execution is billed. This guards the buffer-item billing fix (a buffer
// execution must record a credit_transactions row referencing buffer_items, not
// jobs — see core-api#25). Requires the scheduler/drainer to be running.
func TestE2E_BufferDelivery_AndBilling(t *testing.T) {
	if testing.Short() {
		t.Skip("delivery test requires scheduler + outbound network")
	}

	// Balance before.
	startBalance := getBalance(t)

	id := createBuffer(t, "https://httpbin.org/post", map[string]any{
		"rate_limit":      10,
		"timeout_seconds": 30,
	})

	resp := doJSON(t, http.MethodPost, "/buffers/"+id+"/items", map[string]any{"body": `{"e2e":"delivery"}`})
	expectStatus(t, resp, http.StatusCreated)
	var item map[string]any
	readJSON(t, resp, &item)
	itemID := item["id"].(string)

	// Wait for the drainer to deliver and complete the item.
	final := pollBufferItem(t, id, itemID, "completed", 45*time.Second)
	if sc, ok := final["status_code"].(float64); ok && (sc < 200 || sc >= 300) {
		t.Fatalf("delivered status_code = %v, want 2xx", sc)
	}

	// The execution must have been billed: balance drops and a
	// buffer_item_execution ledger row appears.
	endBalance := getBalance(t)
	if endBalance >= startBalance {
		t.Fatalf("balance did not decrease after delivery: start=%d end=%d", startBalance, endBalance)
	}

	if !hasBufferExecutionTx(t) {
		t.Fatal("expected a credit transaction of type buffer_item_execution after delivery")
	}
}

// ── helpers for the delivery test ────────────────────────────────────────────

func getBalance(t *testing.T) int64 {
	t.Helper()
	resp := doJSON(t, http.MethodGet, "/billing/balance", nil)
	expectStatus(t, resp, http.StatusOK)
	var result map[string]any
	readJSON(t, resp, &result)
	bal, ok := result["balance"].(float64)
	if !ok {
		t.Fatalf("balance not a number: %v", result["balance"])
	}
	return int64(bal)
}

func pollBufferItem(t *testing.T, bufferID, itemID, target string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last map[string]any
	for time.Now().Before(deadline) {
		resp := doJSON(t, http.MethodGet, "/buffers/"+bufferID+"/items/"+itemID, nil)
		readJSON(t, resp, &last)
		if last["status"] == target {
			return last
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("item %s did not reach status %q within %s (last: %v)", itemID, target, timeout, last)
	return nil
}

func hasBufferExecutionTx(t *testing.T) bool {
	t.Helper()
	resp := doJSON(t, http.MethodGet, "/billing/transactions?limit=50", nil)
	expectStatus(t, resp, http.StatusOK)
	var result map[string]any
	readJSON(t, resp, &result)
	txs, ok := result["transactions"].([]any)
	if !ok {
		return false
	}
	for _, raw := range txs {
		tx, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if tx["type"] == "buffer_item_execution" {
			return true
		}
	}
	return false
}
