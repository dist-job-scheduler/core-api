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
	JobID       string  `json:"job_id"`
	Status      string  `json:"status"`
	StatusCode  *int    `json:"status_code,omitempty"`
	LastError   *string `json:"last_error,omitempty"`
	CompletedAt string  `json:"completed_at"`
	AttemptNum  int     `json:"attempt_num"`
}

// WebhookNotifier delivers fire-and-forget webhook notifications for terminal job states.
type WebhookNotifier struct {
	client         *http.Client
	logger         *slog.Logger
	signingSecrets repository.SigningSecretRepository
}

// NewWebhookNotifier creates a notifier with a dedicated HTTP client (10s timeout).
func NewWebhookNotifier(logger *slog.Logger, signingSecrets repository.SigningSecretRepository) *WebhookNotifier {
	return &WebhookNotifier{
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
				DialContext:     safedialer.NewSafeDialContext(5*time.Second, 30*time.Second),
			},
		},
		logger:         logger.With("component", "webhook_notifier"),
		signingSecrets: signingSecrets,
	}
}

// Notify sends a webhook POST if the job has a webhook URL configured.
// Errors are logged but never returned — webhook delivery is best-effort.
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

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, *job.WebhookURL, bytes.NewReader(body))
	if err != nil {
		n.logger.ErrorContext(ctx, "build webhook request", "job_id", job.ID, "error", err)
		metrics.WebhookDeliveriesTotal.WithLabelValues("failure").Inc()
		return
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply user-defined webhook headers.
	for k, v := range job.WebhookHeaders {
		req.Header.Set(k, v)
	}

	// Sign the webhook request if the user has a signing secret.
	secret, secretErr := n.signingSecrets.GetActive(ctx, job.UserID)
	if secretErr != nil && !errors.Is(secretErr, domain.ErrSigningSecretNotFound) {
		n.logger.WarnContext(ctx, "fetch signing secret for webhook failed, proceeding unsigned",
			"job_id", job.ID, "error", secretErr)
	} else if secretErr == nil {
		ts, sig := signRequest(secret.Secret, http.MethodPost, *job.WebhookURL, body, time.Now())
		req.Header.Set("X-Fliq-Timestamp", ts)
		req.Header.Set("X-Fliq-Signature", sig)
	}

	resp, err := n.client.Do(req)
	duration := time.Since(start)
	metrics.WebhookDuration.Observe(duration.Seconds())

	if err != nil {
		metrics.WebhookDeliveriesTotal.WithLabelValues("failure").Inc()
		n.logger.WarnContext(ctx, "webhook delivery failed",
			"job_id", job.ID,
			"webhook_url", *job.WebhookURL,
			"error", err,
			"duration", duration,
		)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		metrics.WebhookDeliveriesTotal.WithLabelValues("success").Inc()
		n.logger.InfoContext(ctx, "webhook delivered",
			"job_id", job.ID,
			"webhook_url", *job.WebhookURL,
			"status_code", resp.StatusCode,
			"duration", duration,
		)
	} else {
		metrics.WebhookDeliveriesTotal.WithLabelValues("failure").Inc()
		n.logger.WarnContext(ctx, "webhook delivery non-2xx",
			"job_id", job.ID,
			"webhook_url", *job.WebhookURL,
			"status_code", resp.StatusCode,
			"duration", duration,
		)
	}
}

// notifyAsync fires the webhook in a background goroutine so it never blocks the worker.
func (n *WebhookNotifier) notifyAsync(ctx context.Context, job *domain.Job, statusCode *int) {
	if job.WebhookURL == nil {
		return
	}
	// Detach from parent context deadline but keep cancellation for shutdown.
	go func() {
		notifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		// Update job status field so the payload reflects the terminal state.
		n.Notify(notifyCtx, job, statusCode)
	}()
}

