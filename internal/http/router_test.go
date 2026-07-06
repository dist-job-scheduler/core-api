package httptransport_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	httptransport "github.com/ErlanBelekov/dist-job-scheduler/internal/http"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/middleware"
	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

// buildRouter constructs the full production route table. Handlers are nil:
// forming a method value from a nil pointer receiver is legal in Go, and route
// registration never calls them — so this exercises the real routing tree
// (including conflict detection, which panics at registration) without a DB.
func buildRouter(t *testing.T) *gin.Engine {
	t.Helper()
	rl := middleware.NewRateLimiter(50, 100)
	t.Cleanup(rl.Stop)
	return httptransport.NewRouter(
		slog.Default(),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, // handlers
		nil, nil, nil, // userRepo, creditRepo, tokenRepo
		"", []byte("test-key"), "", rl,
	)
}

// TestRouter_RegistersWithoutConflict fails if any route (including the new
// replay/alerts/stats routes) collides — Gin panics on conflicting paths.
func TestRouter_RegistersWithoutConflict(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("router registration panicked (route conflict?): %v", r)
		}
	}()
	_ = buildRouter(t)
}

func TestRouter_HealthIsPublic(t *testing.T) {
	r := buildRouter(t)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	if w.Code != http.StatusOK {
		t.Errorf("GET /health = %d, want 200", w.Code)
	}
}

// The new protected routes must sit behind auth — no token yields 401, proving
// they were registered with the auth middleware (not left open).
func TestRouter_NewRoutesRequireAuth(t *testing.T) {
	r := buildRouter(t)
	cases := []struct {
		method, path string
	}{
		{http.MethodPost, "/jobs/abc/replay"},
		{http.MethodPost, "/buffers/abc/items/xyz/replay"},
		{http.MethodGet, "/buffers/abc/stats"},
		{http.MethodGet, "/stats/jobs"},
		{http.MethodGet, "/stats/usage"},
		{http.MethodPost, "/alerts"},
		{http.MethodGet, "/alerts"},
		{http.MethodPatch, "/alerts/abc"},
		{http.MethodDelete, "/alerts/abc"},
		{http.MethodGet, "/webhooks/deliveries"},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d, want 401 (unauthenticated)", c.method, c.path, w.Code)
		}
	}
}
