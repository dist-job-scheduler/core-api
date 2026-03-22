//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"
	"time"
)

func TestE2E_CreateJob(t *testing.T) {
	body := map[string]any{
		"url":          "https://httpbin.org/post",
		"method":       "POST",
		"scheduled_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	resp := doJSON(t, http.MethodPost, "/jobs", body)
	expectStatus(t, resp, http.StatusCreated)

	var result map[string]any
	readJSON(t, resp, &result)
	if result["id"] == nil || result["id"] == "" {
		t.Fatal("expected non-empty job id")
	}
}

func TestE2E_GetJob(t *testing.T) {
	// Create
	body := map[string]any{
		"url":          "https://httpbin.org/get",
		"method":       "GET",
		"scheduled_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	resp := doJSON(t, http.MethodPost, "/jobs", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)

	// Get
	resp = doJSON(t, http.MethodGet, "/jobs/"+created["id"].(string), nil)
	expectStatus(t, resp, http.StatusOK)
	var got map[string]any
	readJSON(t, resp, &got)
	if got["status"] != "pending" {
		t.Fatalf("status = %v, want pending", got["status"])
	}
}

func TestE2E_ListJobs(t *testing.T) {
	// Create a job first
	body := map[string]any{
		"url":          "https://httpbin.org/get",
		"method":       "GET",
		"scheduled_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	resp := doJSON(t, http.MethodPost, "/jobs", body)
	expectStatus(t, resp, http.StatusCreated)

	// List
	resp = doJSON(t, http.MethodGet, "/jobs?limit=10", nil)
	expectStatus(t, resp, http.StatusOK)
	var result map[string]any
	readJSON(t, resp, &result)
	jobs, ok := result["jobs"].([]any)
	if !ok {
		t.Fatal("expected jobs array in response")
	}
	if len(jobs) == 0 {
		t.Fatal("expected at least 1 job")
	}
}

func TestE2E_ListJobs_StatusFilter(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/jobs?status=pending&limit=5", nil)
	expectStatus(t, resp, http.StatusOK)
}

func TestE2E_ListJobs_InvalidStatus(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/jobs?status=bogus", nil)
	expectStatus(t, resp, http.StatusBadRequest)
	_ = resp.Body.Close()
}

func TestE2E_CancelJob(t *testing.T) {
	body := map[string]any{
		"url":          "https://httpbin.org/post",
		"method":       "POST",
		"scheduled_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	resp := doJSON(t, http.MethodPost, "/jobs", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)

	// Cancel
	resp = doJSON(t, http.MethodDelete, "/jobs/"+created["id"].(string), nil)
	expectStatus(t, resp, http.StatusNoContent)
	_ = resp.Body.Close()

	// Verify cancelled
	resp = doJSON(t, http.MethodGet, "/jobs/"+created["id"].(string), nil)
	expectStatus(t, resp, http.StatusOK)
	var got map[string]any
	readJSON(t, resp, &got)
	if got["status"] != "cancelled" {
		t.Fatalf("status = %v, want cancelled", got["status"])
	}
}

func TestE2E_CancelJob_AlreadyCancelled(t *testing.T) {
	body := map[string]any{
		"url":          "https://httpbin.org/post",
		"method":       "POST",
		"scheduled_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	resp := doJSON(t, http.MethodPost, "/jobs", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)

	// Cancel first time
	resp = doJSON(t, http.MethodDelete, "/jobs/"+created["id"].(string), nil)
	expectStatus(t, resp, http.StatusNoContent)
	_ = resp.Body.Close()

	// Cancel second time → 409
	resp = doJSON(t, http.MethodDelete, "/jobs/"+created["id"].(string), nil)
	expectStatus(t, resp, http.StatusConflict)
	_ = resp.Body.Close()
}

func TestE2E_GetJob_NotFound(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/jobs/nonexistent-id-12345", nil)
	expectStatus(t, resp, http.StatusNotFound)
	_ = resp.Body.Close()
}

func TestE2E_CreateJob_BadJSON(t *testing.T) {
	resp := doJSON(t, http.MethodPost, "/jobs", map[string]any{"method": "POST"})
	expectStatus(t, resp, http.StatusBadRequest)
	_ = resp.Body.Close()
}

func TestE2E_ListAttempts(t *testing.T) {
	body := map[string]any{
		"url":          "https://httpbin.org/post",
		"method":       "POST",
		"scheduled_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	}
	resp := doJSON(t, http.MethodPost, "/jobs", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)

	resp = doJSON(t, http.MethodGet, "/jobs/"+created["id"].(string)+"/attempts", nil)
	expectStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()
}
