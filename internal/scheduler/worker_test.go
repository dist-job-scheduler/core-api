package scheduler

import (
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

func TestRetryDelay_Exponential_FirstRetry(t *testing.T) {
	// retryCount=0 → base * 2^0 = 30s, jitter ±25% → [22.5s, 37.5s]
	const iterations = 100
	for i := range iterations {
		d := retryDelay(domain.BackoffExponential, 0)
		lo := 22500 * time.Millisecond
		hi := 37500 * time.Millisecond
		if d < lo || d > hi {
			t.Fatalf("iteration %d: retryDelay(exponential, 0) = %v, want in [%v, %v]", i, d, lo, hi)
		}
	}
}

func TestRetryDelay_Exponential_CappedAt1Hour(t *testing.T) {
	// retryCount=20 → 30s * 2^20 would be ~8738h, but capped at 1h.
	// Jitter ±25% of 1h → [45m, 1h15m]
	const iterations = 100
	for i := range iterations {
		d := retryDelay(domain.BackoffExponential, 20)
		lo := 45 * time.Minute
		hi := 75 * time.Minute
		if d < lo || d > hi {
			t.Fatalf("iteration %d: retryDelay(exponential, 20) = %v, want in [%v, %v]", i, d, lo, hi)
		}
	}
}

func TestRetryDelay_Linear(t *testing.T) {
	tests := []struct {
		retryCount int
		want       time.Duration
	}{
		{0, 30 * time.Second},
		{1, 60 * time.Second},
		{2, 90 * time.Second},
		{5, 180 * time.Second},
	}

	for _, tt := range tests {
		got := retryDelay(domain.BackoffLinear, tt.retryCount)
		if got != tt.want {
			t.Errorf("retryDelay(linear, %d) = %v, want %v", tt.retryCount, got, tt.want)
		}
	}
}

func TestRetryDelay_DefaultFallback(t *testing.T) {
	got := retryDelay("unknown_backoff", 5)
	want := 30 * time.Second
	if got != want {
		t.Errorf("retryDelay(unknown, 5) = %v, want %v", got, want)
	}
}
