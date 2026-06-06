package scheduler

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/requestid"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/safedialer"
)

type Executor struct {
	client *http.Client
	logger *slog.Logger
}

// defaultTransport returns an http.Transport with SSRF-safe dialer.
func defaultTransport() *http.Transport {
	return &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
		DialContext:         safedialer.NewSafeDialContext(10*time.Second, 30*time.Second),
	}
}

func NewExecutor(logger *slog.Logger) *Executor {
	return newExecutor(logger, defaultTransport())
}

// NewExecutorWithTransport creates an Executor with a custom transport (for testing).
func NewExecutorWithTransport(logger *slog.Logger, transport http.RoundTripper) *Executor {
	return newExecutor(logger, transport)
}

func newExecutor(logger *slog.Logger, transport http.RoundTripper) *Executor {
	return &Executor{
		client: &http.Client{
			// Per-job timeouts are set via context; this is a safety net.
			Timeout:   5 * time.Minute,
			Transport: transport,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("stopped after 10 redirects")
				}
				return nil
			},
		},
		logger: logger.With("component", "executor"),
	}
}

type ExecutionResult struct {
	StatusCode int
	Err        error
	Duration   time.Duration
	RetryAfter *time.Duration // populated when status is 429 and Retry-After header is present
}

func (e *Executor) Run(ctx context.Context, job *domain.Job, signingSecret string) ExecutionResult {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, time.Duration(job.TimeoutSeconds)*time.Second)
	defer cancel()

	var bodyReader io.Reader
	if job.Body != nil {
		bodyReader = strings.NewReader(*job.Body)
	}

	req, err := http.NewRequestWithContext(ctx, job.Method, job.URL, bodyReader)
	if err != nil {
		return ExecutionResult{Err: fmt.Errorf("build request: %w", err), Duration: time.Since(start)}
	}

	for k, v := range job.Headers {
		req.Header.Set(k, v)
	}

	// Set after user headers so it cannot be clobbered. Stable across retries
	// and post-crash redeliveries; lets targets dedupe at-least-once deliveries.
	req.Header.Set("X-Fliq-Delivery-Id", job.ID)

	if signingSecret != "" {
		var bodyBytes []byte
		if job.Body != nil {
			bodyBytes = []byte(*job.Body)
		}
		ts, sig := signRequest(signingSecret, job.Method, job.URL, bodyBytes, time.Now())
		req.Header.Set("X-Fliq-Timestamp", ts)
		req.Header.Set("X-Fliq-Signature", sig)
	}

	reqID := requestid.New()
	req.Header.Set("X-Request-ID", reqID)
	ctx = requestid.WithRequestID(ctx, reqID)

	e.logger.InfoContext(ctx, "sending request",
		"job_id", job.ID,
		"method", job.Method,
		"url", job.URL,
	)

	resp, err := e.client.Do(req)
	if err != nil {
		e.logger.ErrorContext(ctx, "request failed",
			"job_id", job.ID,
			"error", err,
			"duration", time.Since(start),
		)
		return ExecutionResult{Err: fmt.Errorf("do request: %w", err), Duration: time.Since(start)}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body) // drain so the connection can be reused by the pool

	duration := time.Since(start)
	e.logger.InfoContext(ctx, "received response",
		"job_id", job.ID,
		"status", resp.StatusCode,
		"duration", duration,
	)

	result := ExecutionResult{StatusCode: resp.StatusCode, Duration: duration}

	if resp.StatusCode == http.StatusTooManyRequests {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if seconds, parseErr := strconv.Atoi(ra); parseErr == nil {
				d := time.Duration(seconds) * time.Second
				result.RetryAfter = &d
			} else if t, parseErr := http.ParseTime(ra); parseErr == nil {
				d := time.Until(t)
				if d < 0 {
					d = 0
				}
				result.RetryAfter = &d
			}
		}
	}

	return result
}
