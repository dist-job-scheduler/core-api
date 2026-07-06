package scheduler

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/metrics"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/safedialer"
)

// WebhookPayload is the JSON body POSTed to the user's webhook endpoint.
type WebhookPayload struct {
	Event       string  `json:"event,omitempty"`
	JobID       string  `json:"job_id"`
	Status      string  `json:"status"`
	StatusCode  *int    `json:"status_code,omitempty"`
	LastError   *string `json:"last_error,omitempty"`
	CompletedAt string  `json:"completed_at"`
	AttemptNum  int     `json:"attempt_num"`
}

// WebhookNotifier signs and POSTs webhook notifications. Its low-level Deliver is
// the single delivery primitive used by both the durable job webhook dispatcher
// (see WebhookDispatcher) and the buffer drainer's best-effort Notify.
type WebhookNotifier struct {
	client         *http.Client
	logger         *slog.Logger
	signingSecrets repository.SigningSecretRepository
}

// NewWebhookNotifier creates a notifier with a dedicated HTTP client (10s timeout)
// whose dialer refuses private/loopback addresses (SSRF-safe).
func NewWebhookNotifier(logger *slog.Logger, signingSecrets repository.SigningSecretRepository) *WebhookNotifier {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext:     safedialer.NewSafeDialContext(5*time.Second, 30*time.Second),
		},
	}
	return newWebhookNotifier(logger, signingSecrets, client)
}

// newWebhookNotifier is the shared constructor; tests inject a client whose
// transport permits loopback so httptest servers are reachable.
func newWebhookNotifier(logger *slog.Logger, signingSecrets repository.SigningSecretRepository, client *http.Client) *WebhookNotifier {
	return &WebhookNotifier{
		client:         client,
		logger:         logger.With("component", "webhook_notifier"),
		signingSecrets: signingSecrets,
	}
}

// Deliver signs (if the user has an active signing secret) and POSTs body to url
// on behalf of userID, returning the HTTP status code (0 if the request never
// completed) and a transport error if any. It records the duration metric but
// does not log, retry, or interpret the status code — the caller owns the
// outcome. This is the shared delivery primitive; callers add retry/logging.
func (n *WebhookNotifier) Deliver(ctx context.Context, userID, url string, headers map[string]string, body []byte) (int, error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply user-defined webhook headers.
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// Sign the request if the user has an active signing secret.
	secret, secretErr := n.signingSecrets.GetActive(ctx, userID)
	if secretErr != nil && !errors.Is(secretErr, domain.ErrSigningSecretNotFound) {
		n.logger.WarnContext(ctx, "fetch signing secret for webhook failed, proceeding unsigned",
			"user_id", userID, "error", secretErr)
	} else if secretErr == nil {
		ts, sig := signRequest(secret.Secret, http.MethodPost, url, body, time.Now())
		req.Header.Set("X-Fliq-Timestamp", ts)
		req.Header.Set("X-Fliq-Signature", sig)
	}

	resp, err := n.client.Do(req)
	metrics.WebhookDuration.Observe(time.Since(start).Seconds())
	if err != nil {
		return 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	return resp.StatusCode, nil
}

// Notify sends a best-effort, fire-once webhook POST for a terminal resource
// state. Errors are logged but never returned and never retried — used by the
// buffer drainer, whose items don't (yet) have the durable delivery log that jobs
// get via WebhookDispatcher.
func (n *WebhookNotifier) Notify(ctx context.Context, job *domain.Job, statusCode *int) {
	if job.WebhookURL == nil {
		return
	}

	completedAt := time.Now().UTC().Format(time.RFC3339)
	if job.CompletedAt != nil {
		completedAt = job.CompletedAt.UTC().Format(time.RFC3339)
	}

	payload := WebhookPayload{
		JobID:       job.ID,
		Status:      string(job.Status),
		StatusCode:  statusCode,
		LastError:   job.LastError,
		CompletedAt: completedAt,
		AttemptNum:  job.RetryCount + 1,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		n.logger.ErrorContext(ctx, "marshal webhook payload", "job_id", job.ID, "error", err)
		return
	}

	code, err := n.Deliver(ctx, job.UserID, *job.WebhookURL, job.WebhookHeaders, body)
	if err != nil {
		metrics.WebhookDeliveriesTotal.WithLabelValues("failure").Inc()
		n.logger.WarnContext(ctx, "webhook delivery failed",
			"job_id", job.ID, "webhook_url", *job.WebhookURL, "error", err)
		return
	}

	if code >= 200 && code < 300 {
		metrics.WebhookDeliveriesTotal.WithLabelValues("success").Inc()
		n.logger.InfoContext(ctx, "webhook delivered",
			"job_id", job.ID, "webhook_url", *job.WebhookURL, "status_code", code)
	} else {
		metrics.WebhookDeliveriesTotal.WithLabelValues("failure").Inc()
		n.logger.WarnContext(ctx, "webhook delivery non-2xx",
			"job_id", job.ID, "webhook_url", *job.WebhookURL, "status_code", code)
	}
}

// notifyAsync fires the webhook in a background goroutine so it never blocks the
// caller. Used by the buffer drainer.
func (n *WebhookNotifier) notifyAsync(ctx context.Context, job *domain.Job, statusCode *int) {
	if job.WebhookURL == nil {
		return
	}
	// Detach from the parent deadline but keep cancellation for shutdown.
	go func() {
		notifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		n.Notify(notifyCtx, job, statusCode)
	}()
}
