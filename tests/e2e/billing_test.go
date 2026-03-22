//go:build e2e

package e2e_test

import (
	"net/http"
	"testing"
)

func TestE2E_GetBalance(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/billing/balance", nil)
	expectStatus(t, resp, http.StatusOK)

	var result map[string]any
	readJSON(t, resp, &result)
	if result["balance"] == nil {
		t.Fatal("expected balance in response")
	}
	if result["plan"] == nil {
		t.Fatal("expected plan in response")
	}
	if result["daily_limit"] == nil {
		t.Fatal("expected daily_limit in response")
	}
	if result["credits_per_dollar"] == nil {
		t.Fatal("expected credits_per_dollar in response")
	}
}

func TestE2E_ListTransactions(t *testing.T) {
	resp := doJSON(t, http.MethodGet, "/billing/transactions?limit=10", nil)
	expectStatus(t, resp, http.StatusOK)

	var result map[string]any
	readJSON(t, resp, &result)
	if result["transactions"] == nil {
		t.Fatal("expected transactions in response")
	}
}
