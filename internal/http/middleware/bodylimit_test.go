package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/middleware"
	"github.com/gin-gonic/gin"
)

func newBodyLimitEngine(maxBytes int64) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.BodyLimit(maxBytes))
	r.POST("/echo", func(c *gin.Context) {
		// Fully read the body; MaxBytesReader errors here when over the cap.
		if _, err := io.ReadAll(c.Request.Body); err != nil {
			c.String(http.StatusRequestEntityTooLarge, "too large")
			return
		}
		c.String(http.StatusOK, "ok")
	})
	return r
}

func TestBodyLimit_UnderLimit(t *testing.T) {
	r := newBodyLimitEngine(1024)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(strings.Repeat("a", 512)))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", w.Code)
	}
}

func TestBodyLimit_OverLimit(t *testing.T) {
	r := newBodyLimitEngine(1024)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/echo", strings.NewReader(strings.Repeat("a", 4096)))
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d, want 413 (body over cap must be rejected)", w.Code)
	}
}
