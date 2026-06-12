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
	"github.com/ErlanBelekov/dist-job-scheduler/internal/mailer"
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
	mailer mailer.Provider
	repo   repository.AlertChannelRepository
	logger *slog.Logger
}

func NewAlertNotifier(logger *slog.Logger, repo repository.AlertChannelRepository, mail mailer.Provider) *AlertNotifier {
	return newAlertNotifier(logger, repo, mail, &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext:     safedialer.NewSafeDialContext(5*time.Second, 30*time.Second),
		},
	})
}

// newAlertNotifier lets tests inject a client without the SSRF-safe dialer so
// they can deliver to a loopback httptest server.
func newAlertNotifier(logger *slog.Logger, repo repository.AlertChannelRepository, mail mailer.Provider, client *http.Client) *AlertNotifier {
	return &AlertNotifier{
		client: client,
		mailer: mail,
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

// LowBalanceConfig carries the low-balance warning settings the worker and
// drainer pass through to the credit repo and the credit_low event. Threshold
// of 0 disables the warning entirely.
type LowBalanceConfig struct {
	Threshold int64
	TopUpURL  string
}

// fireCreditLow builds and fans out a credit_low event from a deduction's
// low-balance crossing. No-op when crossing or the notifier is nil.
func fireCreditLow(ctx context.Context, alerts *AlertNotifier, cfg LowBalanceConfig, userID string, crossing *repository.LowBalanceCrossing) {
	if crossing == nil {
		return
	}
	alerts.NotifyCreditLowAsync(ctx, domain.CreditLowEvent{
		UserID:     userID,
		Balance:    crossing.Balance,
		Threshold:  crossing.Threshold,
		RecentBurn: crossing.RecentBurn,
		TopUpURL:   cfg.TopUpURL,
		CrossedAt:  time.Now(),
	})
}

// NotifyCreditLowAsync loads the user's enabled channels and delivers a
// low-balance warning to each in a background goroutine. Best-effort, like
// NotifyFailureAsync. Safe to call on a nil receiver.
func (n *AlertNotifier) NotifyCreditLowAsync(ctx context.Context, event domain.CreditLowEvent) {
	if n == nil {
		return
	}
	go func() {
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		n.notifyCreditLow(dctx, event)
	}()
}

func (n *AlertNotifier) notifyCreditLow(ctx context.Context, event domain.CreditLowEvent) {
	channels, err := n.repo.ListEnabled(ctx, event.UserID)
	if err != nil {
		n.logger.WarnContext(ctx, "load alert channels failed", "user_id", event.UserID, "error", err)
		return
	}
	for _, ch := range channels {
		body, buildErr := buildCreditLowBody(ch.Type, event)
		if buildErr != nil {
			n.logger.ErrorContext(ctx, "build credit_low body", "channel_id", ch.ID, "error", buildErr)
			continue
		}
		n.dispatch(ctx, ch, body, creditLowSubject)
	}
}

func (n *AlertNotifier) notifyFailure(ctx context.Context, event domain.AlertEvent) {
	channels, err := n.repo.ListEnabled(ctx, event.UserID)
	if err != nil {
		n.logger.WarnContext(ctx, "load alert channels failed", "user_id", event.UserID, "error", err)
		return
	}
	for _, ch := range channels {
		body, buildErr := buildAlertBody(ch.Type, event)
		if buildErr != nil {
			n.logger.ErrorContext(ctx, "build alert body", "channel_id", ch.ID, "error", buildErr)
			continue
		}
		n.dispatch(ctx, ch, body, alertEmailSubject)
	}
}

// dispatch routes a prepared body to the channel's transport: email channels go
// through the mailer (the body is plain text), webhook/slack channels are POSTed
// to their target URL (the body is JSON).
func (n *AlertNotifier) dispatch(ctx context.Context, ch *domain.AlertChannel, body []byte, subject string) {
	if ch.Type == domain.AlertChannelEmail {
		n.deliverEmail(ctx, ch, subject, string(body))
		return
	}
	n.deliverHTTP(ctx, ch, body)
}

func (n *AlertNotifier) deliverEmail(ctx context.Context, ch *domain.AlertChannel, subject, text string) {
	if err := n.mailer.Send(ctx, mailer.Email{To: ch.Target, Subject: subject, Text: text}); err != nil {
		metrics.AlertDeliveriesTotal.WithLabelValues(string(ch.Type), "failure").Inc()
		n.logger.WarnContext(ctx, "alert email delivery failed", "channel_id", ch.ID, "error", err)
		return
	}
	metrics.AlertDeliveriesTotal.WithLabelValues(string(ch.Type), "success").Inc()
	n.logger.InfoContext(ctx, "alert email delivered", "channel_id", ch.ID, "type", ch.Type)
}

func (n *AlertNotifier) deliverHTTP(ctx context.Context, ch *domain.AlertChannel, body []byte) {
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
			"channel_id", ch.ID, "type", ch.Type, "status_code", resp.StatusCode)
		return
	}
	metrics.AlertDeliveriesTotal.WithLabelValues(string(ch.Type), "failure").Inc()
	n.logger.WarnContext(ctx, "alert delivery non-2xx",
		"channel_id", ch.ID, "type", ch.Type, "status_code", resp.StatusCode)
}

// Email subjects for the two event kinds.
const (
	alertEmailSubject = "🔴 Fliq: an endpoint failed permanently"
	creditLowSubject  = "⚠️ Fliq: your credit balance is low"
)

// creditLowWebhookPayload is the JSON body POSTed to a webhook channel for a
// low-balance warning.
type creditLowWebhookPayload struct {
	Event      string `json:"event"`
	Balance    int64  `json:"balance"`
	Threshold  int64  `json:"threshold"`
	RecentBurn int64  `json:"recent_burn"`
	TopUpURL   string `json:"top_up_url,omitempty"`
	CrossedAt  string `json:"crossed_at"`
}

// buildAlertBody renders a failure event into the body shape expected by the
// channel type: a plain-text message for email, a Slack incoming-webhook
// message, or a structured JSON webhook payload.
func buildAlertBody(t domain.AlertChannelType, event domain.AlertEvent) ([]byte, error) {
	switch t {
	case domain.AlertChannelEmail:
		return []byte(alertMessage(event)), nil
	case domain.AlertChannelSlack:
		return json.Marshal(map[string]string{"text": alertMessage(event)})
	case domain.AlertChannelWebhook:
		fallthrough
	default:
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
}

// buildCreditLowBody renders a low-balance warning into the channel's body
// shape. Same dispatch rules as buildAlertBody.
func buildCreditLowBody(t domain.AlertChannelType, event domain.CreditLowEvent) ([]byte, error) {
	switch t {
	case domain.AlertChannelEmail:
		return []byte(creditLowMessage(event)), nil
	case domain.AlertChannelSlack:
		return json.Marshal(map[string]string{"text": creditLowMessage(event)})
	case domain.AlertChannelWebhook:
		fallthrough
	default:
		return json.Marshal(creditLowWebhookPayload{
			Event:      "credit_low",
			Balance:    event.Balance,
			Threshold:  event.Threshold,
			RecentBurn: event.RecentBurn,
			TopUpURL:   event.TopUpURL,
			CrossedAt:  event.CrossedAt.UTC().Format(time.RFC3339),
		})
	}
}

// alertMessage formats a human-readable one-liner for Slack/email failure alerts.
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

// creditLowMessage formats a human-readable low-balance warning for Slack/email.
func creditLowMessage(e domain.CreditLowEvent) string {
	msg := fmt.Sprintf(
		"⚠️ Fliq: your credit balance is low — %d credits left (warning threshold %d).\n"+
			"You've burned %d credits in the last %s.",
		e.Balance, e.Threshold, e.RecentBurn, domain.CreditLowBurnWindow)
	if e.TopUpURL != "" {
		msg += "\nTop up to avoid dropped executions: " + e.TopUpURL
	}
	return msg
}
