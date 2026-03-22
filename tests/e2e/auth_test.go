//go:build e2e

package e2e_test

import (
	"context"
	"net/http"
	"testing"
)

func TestE2E_NoAuth_Returns401(t *testing.T) {
	resp := doJSONNoAuth(t, http.MethodGet, "/jobs", nil)
	expectStatus(t, resp, http.StatusUnauthorized)
	_ = resp.Body.Close()
}

func TestE2E_InvalidToken_Returns401(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/jobs", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-value")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestE2E_SecurityHeaders(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/jobs?limit=1", nil)
	expectStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()

	checks := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
	}
	for header, want := range checks {
		got := resp.Header.Get(header)
		if got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
}

func TestE2E_RequestID(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/jobs?limit=1", nil)
	expectStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()

	if resp.Header.Get("X-Request-ID") == "" {
		t.Fatal("expected X-Request-ID response header")
	}
}
