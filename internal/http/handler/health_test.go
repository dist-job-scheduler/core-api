package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/gin-gonic/gin"
)

func TestHealth_OK(t *testing.T) {
	r := gin.New()
	r.GET("/health", handler.Health)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Time    string `json:"time"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("expected status ok, got %q", body.Status)
	}
	if body.Version == "" {
		t.Error("expected non-empty version")
	}
	if body.Time == "" {
		t.Error("expected non-empty time")
	}
}
