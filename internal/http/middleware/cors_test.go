package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/middleware"
	"github.com/gin-gonic/gin"
)

func newCORSEngine(allowedOrigins string) *gin.Engine {
	r := gin.New()
	r.Use(middleware.CORS(allowedOrigins))
	r.GET("/api", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})
	// Gin requires a route for OPTIONS to reach the middleware; use NoRoute
	// or register an explicit OPTIONS handler. Since CORS middleware calls
	// c.AbortWithStatus(204) on OPTIONS, a catch-all is sufficient.
	r.OPTIONS("/api", func(c *gin.Context) {
		// The CORS middleware aborts before this runs.
		c.Status(http.StatusNoContent)
	})
	return r
}

func TestCORS_Preflight_AllowedOrigin(t *testing.T) {
	r := newCORSEngine("https://app.example.com,http://localhost:3000")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api", nil)
	req.Header.Set("Origin", "https://app.example.com")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}

	wantHeaders := map[string]string{
		"Access-Control-Allow-Origin":      "https://app.example.com",
		"Access-Control-Allow-Methods":     "GET, POST, PUT, PATCH, DELETE, OPTIONS",
		"Access-Control-Allow-Headers":     "Authorization, Content-Type, X-Idempotency-Key",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Max-Age":           "86400",
		"Vary":                             "Origin",
	}

	for header, expected := range wantHeaders {
		got := w.Header().Get(header)
		if got != expected {
			t.Errorf("%s = %q, want %q", header, got, expected)
		}
	}
}

func TestCORS_Preflight_DisallowedOrigin(t *testing.T) {
	r := newCORSEngine("https://app.example.com")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api", nil)
	req.Header.Set("Origin", "https://evil.com")
	r.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty (disallowed origin)", got)
	}
}

func TestCORS_NormalRequest_AllowedOrigin(t *testing.T) {
	r := newCORSEngine("http://localhost:3000")

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}

	got := w.Header().Get("Access-Control-Allow-Origin")
	if got != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, "http://localhost:3000")
	}
}
