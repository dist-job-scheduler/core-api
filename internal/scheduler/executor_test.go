package scheduler_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/scheduler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func TestExecutor_Run_Success_200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exec := scheduler.NewExecutor(slog.Default())
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

	exec := scheduler.NewExecutor(slog.Default())
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

	exec := scheduler.NewExecutor(slog.Default())
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

	exec := scheduler.NewExecutor(slog.Default())
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

	exec := scheduler.NewExecutor(slog.Default())
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
