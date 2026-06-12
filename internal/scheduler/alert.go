package scheduler

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/metrics"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/safedialer"
)

// alertWebhookPayload is the JSON body POSTed to a 'webhook'-type alert channel.
type alertWebhookPayload struct {
	Event        string `json:"event"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	BufferID     string `json:"buffer_id,omitempty"`
	URL          string `json:"url"`
	Method       string `json:"method"`
	LastError    string `json:"last_error"`
	StatusCode   *int   `json:"status_code,omitempty"`
	Attempts     int    `json:"attempts"`
	FailedAt     string `json:"failed_at"`
}

// AlertNotifier delivers failure alerts to a user's configured channels. It is
// used by the scheduler (worker + buffer drainer) when a job or buffer item is
// permanently failed.
type AlertNotifier struct {
	client *http.Client
	repo   repository.AlertChannelRepository
	logger *slog.Logger
}

func NewAlertNotifier(logger *slog.Logger, repo repository.AlertChannelRepository) *AlertNotifier {
	return newAlertNotifier(logger, repo, &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext:     safedialer.NewSafeDialContext(5*time.Second, 30*time.Second),
		},
	})
}

// newAlertNotifier lets tests inject a client without the SSRF-safe dialer so
// they can deliver to a loopback httptest server.
func newAlertNotifier(logger *slog.Logger, repo repository.AlertChannelRepository, client *http.Client) *AlertNotifier {
	return &AlertNotifier{
		client: client,
		repo:   repo,
		logger: logger.With("component", "alert_notifier"),
	}
}

// NotifyFailureAsync loads the user's enabled alert channels and delivers the
// failure event to each in a background goroutine. Best-effort: errors are
// logged, never returned. Safe to call on a nil receiver, which makes alerting
// an opt-in dependency the worker can run without.
func (n *AlertNotifier) NotifyFailureAsync(ctx context.Context, event domain.AlertEvent) {
	if n == nil {
		return
	}
	go func() {
		// Detach from the per-job deadline but keep shutdown cancellation.
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		n.notifyFailure(dctx, event)
	}()
}

func (n *AlertNotifier) notifyFailure(ctx context.Context, event domain.AlertEvent) {
	channels, err := n.repo.ListEnabled(ctx, event.UserID)
	if err != nil {
		n.logger.WarnContext(ctx, "load alert channels failed", "user_id", event.UserID, "error", err)
		return
	}
	for _, ch := range channels {
		n.deliver(ctx, ch, event)
	}
}

func (n *AlertNotifier) deliver(ctx context.Context, ch *domain.AlertChannel, event domain.AlertEvent) {
	body, err := buildAlertBody(ch.Type, event)
	if err != nil {
		n.logger.ErrorContext(ctx, "build alert body", "channel_id", ch.ID, "error", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ch.Target, bytes.NewReader(body))
	if err != nil {
		metrics.AlertDeliveriesTotal.WithLabelValues(string(ch.Type), "failure").Inc()
		n.logger.ErrorContext(ctx, "build alert request", "channel_id", ch.ID, "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		metrics.AlertDeliveriesTotal.WithLabelValues(string(ch.Type), "failure").Inc()
		n.logger.WarnContext(ctx, "alert delivery failed",
			"channel_id", ch.ID, "type", ch.Type, "error", err)
		return
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		metrics.AlertDeliveriesTotal.WithLabelValues(string(ch.Type), "success").Inc()
		n.logger.InfoContext(ctx, "alert delivered",
			"channel_id", ch.ID, "type", ch.Type, "resource_id", event.ResourceID, "status_code", resp.StatusCode)
		return
	}
	metrics.AlertDeliveriesTotal.WithLabelValues(string(ch.Type), "failure").Inc()
	n.logger.WarnContext(ctx, "alert delivery non-2xx",
		"channel_id", ch.ID, "type", ch.Type, "status_code", resp.StatusCode)
}

// buildAlertBody renders the event into the body shape expected by the channel
// type: a Slack incoming-webhook message, or a structured JSON webhook payload.
func buildAlertBody(t domain.AlertChannelType, event domain.AlertEvent) ([]byte, error) {
	if t == domain.AlertChannelSlack {
		return json.Marshal(map[string]string{"text": alertMessage(event)})
	}
	return json.Marshal(alertWebhookPayload{
		Event:        "failure",
		ResourceType: string(event.ResourceType),
		ResourceID:   event.ResourceID,
		BufferID:     event.BufferID,
		URL:          event.URL,
		Method:       event.Method,
		LastError:    event.LastError,
		StatusCode:   event.StatusCode,
		Attempts:     event.Attempts,
		FailedAt:     event.FailedAt.UTC().Format(time.RFC3339),
	})
}

// alertMessage formats a human-readable one-liner for Slack-type channels.
func alertMessage(e domain.AlertEvent) string {
	noun := "Job"
	if e.ResourceType == domain.AlertResourceBufferItem {
		noun = "Buffer item"
	}
	status := ""
	if e.StatusCode != nil {
		status = fmt.Sprintf(" (HTTP %d)", *e.StatusCode)
	}
	return fmt.Sprintf("🔴 Fliq: %s %s failed permanently after %d attempt(s)%s — %s %s\nLast error: %s",
		noun, e.ResourceID, e.Attempts, status, e.Method, e.URL, e.LastError)
}
