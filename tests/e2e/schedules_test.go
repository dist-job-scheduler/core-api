//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"
)

func TestE2E_CreateSchedule(t *testing.T) {
	body := map[string]any{
		"name":      "e2e-test-schedule",
		"cron_expr": "0 */6 * * *",
		"url":       "https://httpbin.org/post",
	}
	resp := doJSON(t, http.MethodPost, "/schedules", body)
	expectStatus(t, resp, http.StatusCreated)

	var result map[string]any
	readJSON(t, resp, &result)
	if result["id"] == nil || result["id"] == "" {
		t.Fatal("expected non-empty schedule id")
	}

	// Cleanup: delete
	resp = doJSON(t, http.MethodDelete, "/schedules/"+result["id"].(string), nil)
	_ = resp.Body.Close()
}

func TestE2E_GetSchedule(t *testing.T) {
	body := map[string]any{
		"name":      "e2e-get-test",
		"cron_expr": "0 0 * * *",
		"url":       "https://httpbin.org/post",
	}
	resp := doJSON(t, http.MethodPost, "/schedules", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)

	resp = doJSON(t, http.MethodGet, "/schedules/"+created["id"].(string), nil)
	expectStatus(t, resp, http.StatusOK)
	var got map[string]any
	readJSON(t, resp, &got)
	if got["name"] != "e2e-get-test" {
		t.Fatalf("name = %v, want e2e-get-test", got["name"])
	}

	// Cleanup
	resp = doJSON(t, http.MethodDelete, "/schedules/"+created["id"].(string), nil)
	_ = resp.Body.Close()
}

func TestE2E_ListSchedules(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/schedules?limit=10", nil)
	expectStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()
}

func TestE2E_PauseResumeSchedule(t *testing.T) {
	body := map[string]any{
		"name":      "e2e-pause-test",
		"cron_expr": "0 0 * * *",
		"url":       "https://httpbin.org/post",
	}
	resp := doJSON(t, http.MethodPost, "/schedules", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)
	id := created["id"].(string)

	// Pause
	resp = doJSON(t, http.MethodPost, "/schedules/"+id+"/pause", nil)
	expectStatus(t, resp, http.StatusNoContent)
	_ = resp.Body.Close()

	// Verify paused
	resp = doJSON(t, http.MethodGet, "/schedules/"+id, nil)
	expectStatus(t, resp, http.StatusOK)
	var got map[string]any
	readJSON(t, resp, &got)
	if got["paused"] != true {
		t.Fatal("expected paused=true")
	}

	// Pause again → 409
	resp = doJSON(t, http.MethodPost, "/schedules/"+id+"/pause", nil)
	expectStatus(t, resp, http.StatusConflict)
	_ = resp.Body.Close()

	// Resume
	resp = doJSON(t, http.MethodPost, "/schedules/"+id+"/resume", nil)
	expectStatus(t, resp, http.StatusNoContent)
	_ = resp.Body.Close()

	// Resume again → 409
	resp = doJSON(t, http.MethodPost, "/schedules/"+id+"/resume", nil)
	expectStatus(t, resp, http.StatusConflict)
	_ = resp.Body.Close()

	// Cleanup
	resp = doJSON(t, http.MethodDelete, "/schedules/"+id, nil)
	_ = resp.Body.Close()
}

func TestE2E_DeleteSchedule(t *testing.T) {
	body := map[string]any{
		"name":      "e2e-delete-test",
		"cron_expr": "0 0 * * *",
		"url":       "https://httpbin.org/post",
	}
	resp := doJSON(t, http.MethodPost, "/schedules", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)
	id := created["id"].(string)

	resp = doJSON(t, http.MethodDelete, "/schedules/"+id, nil)
	expectStatus(t, resp, http.StatusNoContent)
	_ = resp.Body.Close()

	// Verify gone
	resp = doJSON(t, http.MethodGet, "/schedules/"+id, nil)
	expectStatus(t, resp, http.StatusNotFound)
	_ = resp.Body.Close()
}

func TestE2E_CreateSchedule_InvalidCron(t *testing.T) {
	body := map[string]any{
		"name":      "bad-cron",
		"cron_expr": "not a cron",
		"url":       "https://httpbin.org/post",
	}
	resp := doJSON(t, http.MethodPost, "/schedules", body)
	expectStatus(t, resp, http.StatusBadRequest)
	_ = resp.Body.Close()
}

func TestE2E_CreateSchedule_DuplicateName(t *testing.T) {
	name := "e2e-dup-name-test"
	body := map[string]any{
		"name":      name,
		"cron_expr": "0 0 * * *",
		"url":       "https://httpbin.org/post",
	}
	resp := doJSON(t, http.MethodPost, "/schedules", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)

	// Same name → 409
	resp = doJSON(t, http.MethodPost, "/schedules", body)
	expectStatus(t, resp, http.StatusConflict)
	_ = resp.Body.Close()

	// Cleanup
	resp = doJSON(t, http.MethodDelete, "/schedules/"+created["id"].(string), nil)
	_ = resp.Body.Close()
}

func TestE2E_ScheduleListJobs(t *testing.T) {
	body := map[string]any{
		"name":      "e2e-list-jobs-test",
		"cron_expr": "0 0 * * *",
		"url":       "https://httpbin.org/post",
	}
	resp := doJSON(t, http.MethodPost, "/schedules", body)
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)
	id := created["id"].(string)

	resp = doJSON(t, http.MethodGet, "/schedules/"+id+"/jobs?limit=5", nil)
	expectStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()

	// Cleanup
	resp = doJSON(t, http.MethodDelete, "/schedules/"+id, nil)
	_ = resp.Body.Close()
}
