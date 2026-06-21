package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/emailverify"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
)

func setupAlertRouter(repo *testutil.MockAlertChannelRepository) *gin.Engine {
	uc := usecase.NewAlertUsecase(repo, &testutil.FakeMailer{},
		emailverify.NewSigner([]byte("test-secret-32-chars-long-xxxxxx")), "https://api.fliq.test")
	h := handler.NewAlertHandler(uc, slog.Default())

	r := gin.New()
	r.GET("/alerts/verify", h.Verify)
	g := r.Group("/", injectUser(testUserID))
	g.POST("/alerts", h.Create)
	g.GET("/alerts", h.List)
	g.GET("/alerts/:id", h.GetByID)
	g.PATCH("/alerts/:id", h.Update)
	g.DELETE("/alerts/:id", h.Delete)
	return r
}

func TestCreateAlertChannel_HappyPath(t *testing.T) {
	repo := &testutil.MockAlertChannelRepository{
		CreateFn: func(_ context.Context, ch *domain.AlertChannel) (*domain.AlertChannel, error) {
			ch.ID = "alert-xyz"
			return ch, nil
		},
	}
	r := setupAlertRouter(repo)

	body := `{"type":"webhook","target":"https://example.com/hook","name":"ops"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/alerts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] != "alert-xyz" {
		t.Errorf("id = %v, want alert-xyz", resp["id"])
	}
	if resp["enabled"] != true {
		t.Errorf("enabled = %v, want true", resp["enabled"])
	}
}

func TestCreateAlertChannel_InvalidTypeRejectedByBinding(t *testing.T) {
	r := setupAlertRouter(&testutil.MockAlertChannelRepository{})

	body := `{"type":"carrier_pigeon","target":"coop-3"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/alerts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCreateAlertChannel_Email_AcceptedAndDisabled(t *testing.T) {
	var saved *domain.AlertChannel
	repo := &testutil.MockAlertChannelRepository{
		CreateFn: func(_ context.Context, ch *domain.AlertChannel) (*domain.AlertChannel, error) {
			ch.ID = "alert-email"
			saved = ch
			return ch, nil
		},
	}
	r := setupAlertRouter(repo)

	body := `{"type":"email","target":"ops@example.com"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/alerts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}
	if saved.Enabled || saved.Verified {
		t.Error("email channel should be created disabled + unverified")
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["enabled"] != false || resp["verified"] != false {
		t.Errorf("enabled=%v verified=%v, want false/false", resp["enabled"], resp["verified"])
	}
}

func TestCreateAlertChannel_EmailRejectsBadAddress(t *testing.T) {
	r := setupAlertRouter(&testutil.MockAlertChannelRepository{})

	body := `{"type":"email","target":"https://not-an-email.example.com"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/alerts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestVerifyAlertChannel_Endpoint(t *testing.T) {
	var verified string
	repo := &testutil.MockAlertChannelRepository{
		MarkVerifiedFn: func(_ context.Context, id string) error { verified = id; return nil },
	}
	r := setupAlertRouter(repo)

	token := emailverify.NewSigner([]byte("test-secret-32-chars-long-xxxxxx")).Sign("chan-1")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/alerts/verify?token="+token, nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if verified != "chan-1" {
		t.Errorf("verified = %q, want chan-1", verified)
	}
}

func TestVerifyAlertChannel_BadToken(t *testing.T) {
	r := setupAlertRouter(&testutil.MockAlertChannelRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/alerts/verify?token=garbage", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestCreateAlertChannel_NonURLTarget(t *testing.T) {
	r := setupAlertRouter(&testutil.MockAlertChannelRepository{})

	body := `{"type":"slack","target":"not-a-url"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/alerts", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestGetAlertChannel_NotFound(t *testing.T) {
	repo := &testutil.MockAlertChannelRepository{
		GetByIDFn: func(_ context.Context, _, _ string) (*domain.AlertChannel, error) {
			return nil, domain.ErrAlertChannelNotFound
		},
	}
	r := setupAlertRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/alerts/missing", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestUpdateAlertChannel_TogglesEnabled(t *testing.T) {
	var gotEnabled bool
	var called bool
	repo := &testutil.MockAlertChannelRepository{
		SetEnabledFn: func(_ context.Context, _, _ string, enabled bool) error {
			called = true
			gotEnabled = enabled
			return nil
		},
	}
	r := setupAlertRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/alerts/alert-1", strings.NewReader(`{"enabled":false}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if !called || gotEnabled != false {
		t.Errorf("SetEnabled called=%v enabled=%v, want true/false", called, gotEnabled)
	}
}

func TestUpdateAlertChannel_MissingEnabledField(t *testing.T) {
	r := setupAlertRouter(&testutil.MockAlertChannelRepository{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/alerts/alert-1", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDeleteAlertChannel_NotFound(t *testing.T) {
	repo := &testutil.MockAlertChannelRepository{
		DeleteFn: func(_ context.Context, _, _ string) error { return domain.ErrAlertChannelNotFound },
	}
	r := setupAlertRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/alerts/missing", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestListAlertChannels_HappyPath(t *testing.T) {
	repo := &testutil.MockAlertChannelRepository{
		ListFn: func(_ context.Context, _ string) ([]*domain.AlertChannel, error) {
			return []*domain.AlertChannel{
				{ID: "a", Type: domain.AlertChannelSlack, Target: "https://x", Enabled: true},
				{ID: "b", Type: domain.AlertChannelWebhook, Target: "https://y", Enabled: false},
			}, nil
		},
	}
	r := setupAlertRouter(repo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/alerts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp map[string][]map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp["channels"]) != 2 {
		t.Errorf("len(channels) = %d, want 2", len(resp["channels"]))
	}
}
