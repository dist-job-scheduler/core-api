package handler_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/gin-gonic/gin"
)

func setupTokenRouter(tokenRepo *testutil.MockAPITokenRepository) *gin.Engine {
	h := handler.NewTokenHandler(tokenRepo, slog.Default())

	r := gin.New()
	g := r.Group("/", injectUser(testUserID))
	g.POST("/tokens", h.Create)
	g.GET("/tokens", h.List)
	g.DELETE("/tokens/:id", h.Delete)
	return r
}

func TestCreateToken_HappyPath(t *testing.T) {
	tokenRepo := &testutil.MockAPITokenRepository{
		CreateFn: func(_ context.Context, userID, name, tokenHash, prefix string) (*domain.APIToken, error) {
			return &domain.APIToken{
				ID:        "tok-abc",
				UserID:    userID,
				Name:      name,
				Prefix:    prefix,
				CreatedAt: time.Now().UTC(),
			}, nil
		},
	}
	r := setupTokenRouter(tokenRepo)

	body := `{"name":"my-token"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["id"] != "tok-abc" {
		t.Errorf("id = %v, want %q", resp["id"], "tok-abc")
	}
	if resp["name"] != "my-token" {
		t.Errorf("name = %v, want %q", resp["name"], "my-token")
	}
	if _, ok := resp["prefix"]; !ok {
		t.Error("response missing prefix")
	}
	if _, ok := resp["token"]; !ok {
		t.Error("response missing token (raw token should be returned on create)")
	}
	// The raw token must start with fliq_sk_
	tok, _ := resp["token"].(string)
	if len(tok) < 8 || tok[:8] != "fliq_sk_" {
		t.Errorf("token prefix = %q, want fliq_sk_ prefix", tok[:8])
	}
}

func TestCreateToken_MissingName(t *testing.T) {
	r := setupTokenRouter(&testutil.MockAPITokenRepository{})

	body := `{}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tokens", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestListTokens_HappyPath(t *testing.T) {
	now := time.Now().UTC()
	tokenRepo := &testutil.MockAPITokenRepository{
		ListByUserIDFn: func(_ context.Context, _ string) ([]*domain.APIToken, error) {
			return []*domain.APIToken{
				{ID: "tok-1", UserID: testUserID, Name: "token-1", Prefix: "fliq_sk_aabb", CreatedAt: now},
				{ID: "tok-2", UserID: testUserID, Name: "token-2", Prefix: "fliq_sk_ccdd", CreatedAt: now},
			}, nil
		},
	}
	r := setupTokenRouter(tokenRepo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tokens", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 2 {
		t.Errorf("len(tokens) = %d, want 2", len(resp))
	}
}

func TestDeleteToken_HappyPath(t *testing.T) {
	tokenRepo := &testutil.MockAPITokenRepository{
		DeleteFn: func(_ context.Context, _, _ string) error { return nil },
	}
	r := setupTokenRouter(tokenRepo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/tokens/tok-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestDeleteToken_NotFound(t *testing.T) {
	tokenRepo := &testutil.MockAPITokenRepository{
		DeleteFn: func(_ context.Context, _, _ string) error {
			return domain.ErrTokenNotFound
		},
	}
	r := setupTokenRouter(tokenRepo)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/tokens/nonexistent", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
