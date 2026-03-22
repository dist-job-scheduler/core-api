package scheduler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func TestSignRequest(t *testing.T) {
	fixedTime := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)
	fixedTS := "1774094400"

	tests := []struct {
		name   string
		secret string
		method string
		url    string
		body   []byte
	}{
		{
			name:   "POST with body",
			secret: "whsec_test-secret-123",
			method: "POST",
			url:    "https://example.com/webhook",
			body:   []byte(`{"key":"value"}`),
		},
		{
			name:   "GET no body",
			secret: "whsec_another-secret",
			method: "GET",
			url:    "https://example.com/health",
			body:   nil,
		},
		{
			name:   "PUT with body",
			secret: "whsec_put-secret",
			method: "PUT",
			url:    "https://api.example.com/update",
			body:   []byte("plain text body"),
		},
		{
			name:   "empty body bytes",
			secret: "whsec_empty",
			method: "POST",
			url:    "https://example.com/ping",
			body:   []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, sig := signRequest(tt.secret, tt.method, tt.url, tt.body, fixedTime)

			if ts != fixedTS {
				t.Errorf("timestamp = %q, want %q", ts, fixedTS)
			}

			if len(sig) < 4 || sig[:3] != "v1=" {
				t.Fatalf("signature missing v1= prefix: %q", sig)
			}

			// Verify by recomputing HMAC independently.
			bodyStr := ""
			if len(tt.body) > 0 {
				bodyStr = string(tt.body)
			}
			payload := fmt.Sprintf("%s.%s.%s.%s", fixedTS, tt.method, tt.url, bodyStr)
			mac := hmac.New(sha256.New, []byte(tt.secret))
			mac.Write([]byte(payload))
			expected := "v1=" + hex.EncodeToString(mac.Sum(nil))

			if sig != expected {
				t.Errorf("signature mismatch\n  got:  %s\n  want: %s", sig, expected)
			}
		})
	}
}

func TestSignRequestDeterministic(t *testing.T) {
	now := time.Now()
	ts1, sig1 := signRequest("secret", "POST", "https://example.com", []byte("body"), now)
	ts2, sig2 := signRequest("secret", "POST", "https://example.com", []byte("body"), now)

	if ts1 != ts2 || sig1 != sig2 {
		t.Error("same inputs should produce identical output")
	}
}

func TestSignRequestDifferentSecrets(t *testing.T) {
	now := time.Now()
	_, sig1 := signRequest("secret-a", "POST", "https://example.com", []byte("body"), now)
	_, sig2 := signRequest("secret-b", "POST", "https://example.com", []byte("body"), now)

	if sig1 == sig2 {
		t.Error("different secrets should produce different signatures")
	}
}
