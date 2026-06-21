package scheduler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
)

func TestBuildAlertBody_Slack(t *testing.T) {
	code := 500
	event := domain.AlertEvent{
		ResourceType: domain.AlertResourceJob,
		ResourceID:   "job-1",
		URL:          "https://api.example.com",
		Method:       "POST",
		LastError:    "boom",
		StatusCode:   &code,
		Attempts:     4,
	}
	body, err := buildAlertBody(domain.AlertChannelSlack, event)
	if err != nil {
		t.Fatalf("buildAlertBody: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	text, ok := payload["text"]
	if !ok {
		t.Fatal("slack payload missing text field")
	}
	for _, want := range []string{"job-1", "boom", "4 attempt", "HTTP 500"} {
		if !strings.Contains(text, want) {
			t.Errorf("slack text %q missing %q", text, want)
		}
	}
}

func TestBuildAlertBody_Webhook(t *testing.T) {
	event := domain.AlertEvent{
		ResourceType: domain.AlertResourceBufferItem,
		ResourceID:   "item-1",
		BufferID:     "buf-1",
		URL:          "https://api.example.com",
		Method:       "POST",
		LastError:    "timeout",
		Attempts:     3,
		FailedAt:     time.Now(),
	}
	body, err := buildAlertBody(domain.AlertChannelWebhook, event)
	if err != nil {
		t.Fatalf("buildAlertBody: %v", err)
	}
	var payload alertWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Event != "failure" || payload.ResourceType != "buffer_item" || payload.ResourceID != "item-1" {
		t.Errorf("payload = %+v, want failure/buffer_item/item-1", payload)
	}
	if payload.BufferID != "buf-1" {
		t.Errorf("buffer_id = %q, want buf-1", payload.BufferID)
	}
	if payload.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", payload.Attempts)
	}
}

func TestNotifyFailureAsync_DeliversToEnabledChannels(t *testing.T) {
	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	repo := &testutil.MockAlertChannelRepository{
		ListEnabledFn: func(_ context.Context, _ string) ([]*domain.AlertChannel, error) {
			return []*domain.AlertChannel{
				{ID: "a", Type: domain.AlertChannelWebhook, Target: srv.URL, Enabled: true},
				{ID: "b", Type: domain.AlertChannelSlack, Target: srv.URL, Enabled: true},
			}, nil
		},
	}
	n := newAlertNotifier(slog.Default(), repo, &testutil.FakeMailer{}, srv.Client())

	n.NotifyFailureAsync(context.Background(), domain.AlertEvent{
		UserID: "user-1", ResourceType: domain.AlertResourceJob, ResourceID: "job-1",
	})

	waitFor(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return hits == 2
	})
}

func TestNotifyFailureAsync_NilReceiverSafe(t *testing.T) {
	var n *AlertNotifier
	// Must not panic — alerting is an opt-in dependency.
	n.NotifyFailureAsync(context.Background(), domain.AlertEvent{UserID: "user-1"})
}

func TestNotifyFailure_RepoErrorTolerated(t *testing.T) {
	repo := &testutil.MockAlertChannelRepository{
		ListEnabledFn: func(_ context.Context, _ string) ([]*domain.AlertChannel, error) {
			return nil, context.DeadlineExceeded
		},
	}
	n := NewAlertNotifier(slog.Default(), repo, &testutil.FakeMailer{})
	// notifyFailure swallows the repo error (logged, not returned).
	n.notifyFailure(context.Background(), domain.AlertEvent{UserID: "user-1"})
}

func TestBuildAlertBody_Email(t *testing.T) {
	event := domain.AlertEvent{
		ResourceType: domain.AlertResourceJob,
		ResourceID:   "job-9",
		URL:          "https://api.example.com",
		Method:       "POST",
		LastError:    "boom",
		Attempts:     4,
	}
	body, err := buildAlertBody(domain.AlertChannelEmail, event)
	if err != nil {
		t.Fatalf("buildAlertBody: %v", err)
	}
	// Email body is plain text, not JSON.
	text := string(body)
	for _, want := range []string{"job-9", "boom", "4 attempt"} {
		if !strings.Contains(text, want) {
			t.Errorf("email body %q missing %q", text, want)
		}
	}
}

func TestBuildCreditLowBody_Webhook(t *testing.T) {
	event := domain.CreditLowEvent{
		Balance: 42, Threshold: 100, RecentBurn: 500,
		TopUpURL: "https://fliq.sh/billing", CrossedAt: time.Now(),
	}
	body, err := buildCreditLowBody(domain.AlertChannelWebhook, event)
	if err != nil {
		t.Fatalf("buildCreditLowBody: %v", err)
	}
	var payload creditLowWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if payload.Event != "credit_low" || payload.Balance != 42 || payload.Threshold != 100 || payload.RecentBurn != 500 {
		t.Errorf("payload = %+v, want credit_low/42/100/500", payload)
	}
}

func TestBuildCreditLowBody_SlackAndEmail(t *testing.T) {
	event := domain.CreditLowEvent{Balance: 42, Threshold: 100, RecentBurn: 500, TopUpURL: "https://fliq.sh/billing"}

	slackBody, err := buildCreditLowBody(domain.AlertChannelSlack, event)
	if err != nil {
		t.Fatalf("slack: %v", err)
	}
	var slack map[string]string
	if err = json.Unmarshal(slackBody, &slack); err != nil {
		t.Fatalf("unmarshal slack: %v", err)
	}
	if !strings.Contains(slack["text"], "42") || !strings.Contains(slack["text"], "billing") {
		t.Errorf("slack text missing balance/link: %q", slack["text"])
	}

	emailBody, err := buildCreditLowBody(domain.AlertChannelEmail, event)
	if err != nil {
		t.Fatalf("email: %v", err)
	}
	if !strings.Contains(string(emailBody), "42") {
		t.Errorf("email body missing balance: %q", emailBody)
	}
}

func TestNotifyFailure_EmailChannelUsesMailer(t *testing.T) {
	mail := &testutil.FakeMailer{}
	repo := &testutil.MockAlertChannelRepository{
		ListEnabledFn: func(_ context.Context, _ string) ([]*domain.AlertChannel, error) {
			return []*domain.AlertChannel{
				{ID: "e", Type: domain.AlertChannelEmail, Target: "ops@example.com", Enabled: true, Verified: true},
			}, nil
		},
	}
	n := newAlertNotifier(slog.Default(), repo, mail, http.DefaultClient)

	// Synchronous path — no goroutine, deterministic assertion.
	n.notifyFailure(context.Background(), domain.AlertEvent{
		UserID: "user-1", ResourceType: domain.AlertResourceJob, ResourceID: "job-1", LastError: "down",
	})

	msgs := mail.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 email, got %d", len(msgs))
	}
	if msgs[0].To != "ops@example.com" || !strings.Contains(msgs[0].Text, "job-1") {
		t.Errorf("email = %+v, want to ops@example.com mentioning job-1", msgs[0])
	}
}

func TestNotifyCreditLow_FansOutToEmailAndWebhook(t *testing.T) {
	mail := &testutil.FakeMailer{}
	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	repo := &testutil.MockAlertChannelRepository{
		ListEnabledFn: func(_ context.Context, _ string) ([]*domain.AlertChannel, error) {
			return []*domain.AlertChannel{
				{ID: "e", Type: domain.AlertChannelEmail, Target: "ops@example.com", Enabled: true, Verified: true},
				{ID: "w", Type: domain.AlertChannelWebhook, Target: srv.URL, Enabled: true, Verified: true},
			}, nil
		},
	}
	n := newAlertNotifier(slog.Default(), repo, mail, srv.Client())

	n.notifyCreditLow(context.Background(), domain.CreditLowEvent{
		UserID: "user-1", Balance: 10, Threshold: 100, RecentBurn: 1000, TopUpURL: "https://fliq.sh/billing",
	})

	if got := len(mail.Messages()); got != 1 {
		t.Errorf("email count = %d, want 1", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("webhook hits = %d, want 1", hits)
	}
}

func TestNotifyCreditLowAsync_NilReceiverSafe(t *testing.T) {
	var n *AlertNotifier
	n.NotifyCreditLowAsync(context.Background(), domain.CreditLowEvent{UserID: "user-1"})
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
