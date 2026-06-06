package scheduler_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/safedialer"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/scheduler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

// testExecutor creates an executor with the default (non-safe) transport so
// httptest servers on 127.0.0.1 are reachable.
func testExecutor() *scheduler.Executor {
	return scheduler.NewExecutorWithTransport(slog.Default(), http.DefaultTransport)
}

func TestExecutor_Run_Success_200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := testExecutor()
	job := testutil.NewJob(testutil.WithURL(srv.URL), testutil.WithMethod("GET"))

	result := exec.Run(context.Background(), job, "")

	if result.Err != nil {
		t.Fatalf("Run() error = %v, want nil", result.Err)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
}

func TestExecutor_Run_NonOK_Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	exec := testExecutor()
	job := testutil.NewJob(testutil.WithURL(srv.URL), testutil.WithMethod("GET"))

	result := exec.Run(context.Background(), job, "")

	if result.Err != nil {
		t.Fatalf("Run() error = %v, want nil (non-OK status is not an error)", result.Err)
	}
	if result.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusInternalServerError)
	}
}

func TestExecutor_Run_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := testExecutor()
	job := testutil.NewJob(testutil.WithURL(srv.URL), testutil.WithMethod("GET"))
	job.TimeoutSeconds = 1

	result := exec.Run(context.Background(), job, "")

	if result.Err == nil {
		t.Fatal("Run() error = nil, want context deadline exceeded")
	}
	if !strings.Contains(result.Err.Error(), "context deadline exceeded") {
		t.Errorf("Run() error = %v, want to contain %q", result.Err, "context deadline exceeded")
	}
}

func TestExecutor_Run_CustomHeaders(t *testing.T) {
	var gotContentType, gotCustom string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotCustom = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := testExecutor()
	job := testutil.NewJob(testutil.WithURL(srv.URL), testutil.WithMethod("POST"))
	job.Headers = map[string]string{
		"Content-Type":    "application/xml",
		"X-Custom-Header": "custom-value",
	}

	result := exec.Run(context.Background(), job, "")

	if result.Err != nil {
		t.Fatalf("Run() error = %v, want nil", result.Err)
	}
	if gotContentType != "application/xml" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/xml")
	}
	if gotCustom != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want %q", gotCustom, "custom-value")
	}
}

func TestExecutor_Run_SetsRequestID(t *testing.T) {
	var gotRequestID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestID = r.Header.Get("X-Request-ID")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := testExecutor()
	job := testutil.NewJob(testutil.WithURL(srv.URL), testutil.WithMethod("GET"))

	result := exec.Run(context.Background(), job, "")

	if result.Err != nil {
		t.Fatalf("Run() error = %v, want nil", result.Err)
	}
	if gotRequestID == "" {
		t.Fatal("X-Request-ID header not set on outbound request")
	}
	// UUID v4 is 36 characters: 8-4-4-4-12
	if len(gotRequestID) != 36 {
		t.Errorf("X-Request-ID length = %d, want 36 (UUID format)", len(gotRequestID))
	}
}

func TestExecutor_Run_SetsDeliveryID(t *testing.T) {
	var gotDeliveryID string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotDeliveryID = r.Header.Get("X-Fliq-Delivery-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := testExecutor()
	job := testutil.NewJob(testutil.WithURL(srv.URL), testutil.WithMethod("POST"))
	// A user-supplied header for the same key must not win.
	job.Headers = map[string]string{"X-Fliq-Delivery-Id": "attacker-controlled"}

	result := exec.Run(context.Background(), job, "")

	if result.Err != nil {
		t.Fatalf("Run() error = %v, want nil", result.Err)
	}
	if gotDeliveryID != job.ID {
		t.Errorf("X-Fliq-Delivery-Id = %q, want %q (job ID)", gotDeliveryID, job.ID)
	}
}

func TestExecutor_Run_BlocksSSRF(t *testing.T) {
	// Use the production executor (with safe dialer) to verify SSRF blocking.
	exec := scheduler.NewExecutor(slog.Default())
	job := testutil.NewJob(testutil.WithURL("http://127.0.0.1:9999/secret"), testutil.WithMethod("GET"))

	result := exec.Run(context.Background(), job, "")

	if result.Err == nil {
		t.Fatal("Run() error = nil, want SSRF blocked error")
	}
	if !errors.Is(result.Err, safedialer.ErrBlockedAddress) && !strings.Contains(result.Err.Error(), "blocked network range") {
		t.Errorf("Run() error = %v, want to contain blocked address error", result.Err)
	}
}
