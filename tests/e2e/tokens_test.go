//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"
)

func TestE2E_CreateToken(t *testing.T) {
	resp := doJSON(t, http.MethodPost, "/tokens", map[string]any{"name": "e2e-test-token"})
	expectStatus(t, resp, http.StatusCreated)

	var result map[string]any
	readJSON(t, resp, &result)
	if result["token"] == nil || result["token"] == "" {
		t.Fatal("expected raw token in response")
	}
	token := result["token"].(string)
	if len(token) < 8 || token[:8] != "fliq_sk_" {
		t.Fatalf("token prefix wrong: %s", token[:min(16, len(token))])
	}
	if result["prefix"] == nil {
		t.Fatal("expected prefix in response")
	}

	// Cleanup: delete the token
	if result["id"] != nil {
		resp = doJSON(t, http.MethodDelete, "/tokens/"+result["id"].(string), nil)
		_ = resp.Body.Close()
	}
}

func TestE2E_ListTokens(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/tokens", nil)
	expectStatus(t, resp, http.StatusOK)
	_ = resp.Body.Close()
}

func TestE2E_DeleteToken(t *testing.T) {
	// Create
	resp := doJSON(t, http.MethodPost, "/tokens", map[string]any{"name": "e2e-delete-token"})
	expectStatus(t, resp, http.StatusCreated)
	var created map[string]any
	readJSON(t, resp, &created)

	// Delete
	resp = doJSON(t, http.MethodDelete, "/tokens/"+created["id"].(string), nil)
	expectStatus(t, resp, http.StatusNoContent)
	_ = resp.Body.Close()

	// Delete again → 404
	resp = doJSON(t, http.MethodDelete, "/tokens/"+created["id"].(string), nil)
	expectStatus(t, resp, http.StatusNotFound)
	_ = resp.Body.Close()
}

func TestE2E_CreateToken_MissingName(t *testing.T) {
	resp := doJSON(t, http.MethodPost, "/tokens", map[string]any{})
	expectStatus(t, resp, http.StatusBadRequest)
	_ = resp.Body.Close()
}
