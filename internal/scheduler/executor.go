package scheduler

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/requestid"
)

type Executor struct {
	client *http.Client
	logger *slog.Logger
}

func NewExecutor(logger *slog.Logger) *Executor {
	return &Executor{
		client: &http.Client{
			// Per-job timeouts are set via context; this is a safety net.
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS12,
				},
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
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

	return ExecutionResult{StatusCode: resp.StatusCode, Duration: duration}
}
