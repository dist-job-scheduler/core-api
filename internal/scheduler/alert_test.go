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
	n := newAlertNotifier(slog.Default(), repo, srv.Client())

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
	n := NewAlertNotifier(slog.Default(), repo)
	// notifyFailure swallows the repo error (logged, not returned).
	n.notifyFailure(context.Background(), domain.AlertEvent{UserID: "user-1"})
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
