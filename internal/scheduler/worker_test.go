package scheduler

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
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

func TestWorker_Start_PollsOnInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var claimCount atomic.Int32

		repo := &testutil.MockJobRepository{
			ClaimFn: func(ctx context.Context, workerID string, limit int) ([]*domain.Job, error) {
				claimCount.Add(1)
				return nil, nil
			},
		}

		w := &Worker{
			id:             "test-worker",
			repo:           repo,
			attempts:       &testutil.MockAttemptRepository{},
			credits:        &testutil.MockCreditRepository{},
			signingSecrets: &testutil.MockSigningSecretRepository{},
			executor:       NewExecutor(slog.Default()),
			notifier:       NewWebhookNotifier(slog.Default(), &testutil.MockSigningSecretRepository{}),
			logger:         slog.Default(),
			pollInterval:   5 * time.Second,
			concurrency:    10,
			sem:            make(chan struct{}, 10),
		}

		ctx, cancel := context.WithCancel(t.Context())
		go w.Start(ctx)

		// First tick at 5s.
		time.Sleep(5 * time.Second)
		synctest.Wait()

		if got := claimCount.Load(); got != 1 {
			t.Fatalf("after 5s: claim count = %d, want 1", got)
		}

		// Second tick at 10s.
		time.Sleep(5 * time.Second)
		synctest.Wait()

		if got := claimCount.Load(); got != 2 {
			t.Fatalf("after 10s: claim count = %d, want 2", got)
		}

		cancel()
		synctest.Wait()
	})
}

func TestWorker_Heartbeat_FiresEvery10s(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var heartbeatCount atomic.Int32

		repo := &testutil.MockJobRepository{
			UpdateHeartbeatFn: func(ctx context.Context, jobID string) error {
				heartbeatCount.Add(1)
				return nil
			},
		}

		w := &Worker{
			repo:   repo,
			logger: slog.Default(),
		}

		ctx, cancel := context.WithCancel(t.Context())
		go w.heartbeat(ctx, "job-123")

		// First heartbeat at 10s.
		time.Sleep(10 * time.Second)
		synctest.Wait()

		if got := heartbeatCount.Load(); got != 1 {
			t.Fatalf("after 10s: heartbeat count = %d, want 1", got)
		}

		// Second heartbeat at 20s.
		time.Sleep(10 * time.Second)
		synctest.Wait()

		if got := heartbeatCount.Load(); got != 2 {
			t.Fatalf("after 20s: heartbeat count = %d, want 2", got)
		}

		// 5s more (25s total) — should still be 2, ticker hasn't fired.
		time.Sleep(5 * time.Second)
		synctest.Wait()

		if got := heartbeatCount.Load(); got != 2 {
			t.Fatalf("after 25s: heartbeat count = %d, want 2 (ticker not due)", got)
		}

		cancel()
		synctest.Wait()
	})
}
