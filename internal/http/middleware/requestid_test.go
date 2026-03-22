package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/middleware"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/requestid"
	"github.com/gin-gonic/gin"
)

func TestRequestID_GeneratesID_WhenMissing(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)

	got := w.Header().Get("X-Request-ID")
	if got == "" {
		t.Fatal("X-Request-ID header is empty, want a generated UUID")
	}
	// UUID v4 is 36 characters: 8-4-4-4-12
	if len(got) != 36 {
		t.Errorf("X-Request-ID length = %d, want 36 (UUID format)", len(got))
	}
}

func TestRequestID_PreservesExistingID(t *testing.T) {
	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Request-ID", "my-id-123")
	r.ServeHTTP(w, req)

	got := w.Header().Get("X-Request-ID")
	if got != "my-id-123" {
		t.Errorf("X-Request-ID = %q, want %q", got, "my-id-123")
	}
}

func TestRequestID_SetsInContext(t *testing.T) {
	var ctxID string

	r := gin.New()
	r.Use(middleware.RequestID())
	r.GET("/ping", func(c *gin.Context) {
		ctxID = requestid.FromContext(c.Request.Context())
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set("X-Request-ID", "ctx-test-456")
	r.ServeHTTP(w, req)

	if ctxID != "ctx-test-456" {
		t.Errorf("requestid.FromContext = %q, want %q", ctxID, "ctx-test-456")
	}

	// Also verify it matches the response header.
	if got := w.Header().Get("X-Request-ID"); got != ctxID {
		t.Errorf("response header = %q, context = %q, want them equal", got, ctxID)
	}
}
