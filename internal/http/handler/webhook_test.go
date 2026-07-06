package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/gin-gonic/gin"
)

func setupWebhookRouter(repo *testutil.MockWebhookDeliveryRepository) *gin.Engine {
	h := handler.NewWebhookHandler(repo, slog.Default())
	r := gin.New()
	g := r.Group("/", injectUser(testUserID))
	g.GET("/webhooks/deliveries", h.ListDeliveries)
	return r
}

func TestListDeliveries_HappyPath(t *testing.T) {
	code := 200
	repo := &testutil.MockWebhookDeliveryRepository{
		ListByUserFn: func(_ context.Context, userID string, limit int, _ *time.Time, _ string) ([]*domain.WebhookDelivery, error) {
			if userID != testUserID {
				t.Errorf("userID = %q, want %q", userID, testUserID)
			}
			// Default page size is 25 → handler asks for 26 (limit+1).
			if limit != 26 {
				t.Errorf("limit passed to repo = %d, want 26", limit)
			}
			jobID := "job-1"
			return []*domain.WebhookDelivery{
				{ID: "d1", UserID: userID, JobID: &jobID, Event: domain.WebhookEventJobCompleted,
					URL: "https://x/hook", Status: domain.WebhookDeliveryDelivered, StatusCode: &code,
					Attempts: 1, MaxAttempts: 10, CreatedAt: time.Now()},
			}, nil
		},
	}
	r := setupWebhookRouter(repo)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/webhooks/deliveries", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		Deliveries []map[string]any `json:"deliveries"`
		NextCursor *string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v; body = %s", err, w.Body.String())
	}
	if len(resp.Deliveries) != 1 {
		t.Fatalf("deliveries = %d, want 1", len(resp.Deliveries))
	}
	if resp.NextCursor != nil {
		t.Errorf("next_cursor = %v, want null (only one page)", *resp.NextCursor)
	}
	if resp.Deliveries[0]["status"] != "delivered" || resp.Deliveries[0]["event"] != "job.completed" {
		t.Errorf("delivery fields wrong: %+v", resp.Deliveries[0])
	}
}

func TestListDeliveries_PaginationEmitsCursor(t *testing.T) {
	repo := &testutil.MockWebhookDeliveryRepository{
		ListByUserFn: func(_ context.Context, userID string, limit int, _ *time.Time, _ string) ([]*domain.WebhookDelivery, error) {
			if limit != 3 {
				t.Errorf("limit passed to repo = %d, want 3 (page size 2 + 1)", limit)
			}
			// Return limit rows to signal there IS another page.
			base := time.Now()
			return []*domain.WebhookDelivery{
				{ID: "d1", UserID: userID, CreatedAt: base},
				{ID: "d2", UserID: userID, CreatedAt: base.Add(-time.Second)},
				{ID: "d3", UserID: userID, CreatedAt: base.Add(-2 * time.Second)},
			}, nil
		},
	}
	r := setupWebhookRouter(repo)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/webhooks/deliveries?limit=2", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp struct {
		Deliveries []map[string]any `json:"deliveries"`
		NextCursor *string          `json:"next_cursor"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Deliveries) != 2 {
		t.Fatalf("deliveries = %d, want 2 (extra row trimmed)", len(resp.Deliveries))
	}
	if resp.NextCursor == nil || *resp.NextCursor == "" {
		t.Fatal("next_cursor missing despite a further page")
	}
}

func TestListDeliveries_InvalidCursor(t *testing.T) {
	repo := &testutil.MockWebhookDeliveryRepository{
		ListByUserFn: func(_ context.Context, _ string, _ int, _ *time.Time, _ string) ([]*domain.WebhookDelivery, error) {
			t.Fatal("repo queried despite invalid cursor")
			return nil, nil
		},
	}
	r := setupWebhookRouter(repo)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/webhooks/deliveries?cursor=not-a-valid-cursor!!", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
