//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

var (
	baseURL string
	authTok string
	client  *http.Client
)

func TestMain(m *testing.M) {
	baseURL = os.Getenv("E2E_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	authTok = os.Getenv("E2E_AUTH_TOKEN")
	if authTok == "" {
		fmt.Fprintln(os.Stderr, "E2E_AUTH_TOKEN not set — set a valid Bearer token or API token (fliq_sk_*)")
		os.Exit(1)
	}

	client = &http.Client{Timeout: 30 * time.Second}

	os.Exit(m.Run())
}

// doJSON sends an HTTP request with JSON body and returns the response.
func doJSON(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(context.Background(), method, baseURL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+authTok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// doJSONNoAuth sends a request without Authorization header.
func doJSONNoAuth(t *testing.T, method, path string, body any) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, baseURL+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// readJSON reads and decodes the response body into dest.
func readJSON(t *testing.T, resp *http.Response, dest any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if err := json.Unmarshal(b, dest); err != nil {
		t.Fatalf("unmarshal response body: %v\nbody: %s", err, string(b))
	}
}

// expectStatus asserts the HTTP status code and closes the body on mismatch.
func expectStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want %d, body: %s", resp.StatusCode, want, string(body))
	}
}

// pollJobStatus polls until the job reaches the target status or times out.
func pollJobStatus(t *testing.T, jobID, targetStatus string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := doJSON(t, http.MethodGet, "/jobs/"+jobID, nil)
		var result map[string]any
		readJSON(t, resp, &result)
		if result["status"] == targetStatus {
			return result
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach status %s within %s", jobID, targetStatus, timeout)
	return nil
}
