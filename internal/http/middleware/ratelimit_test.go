package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/middleware"
	"github.com/gin-gonic/gin"
)

func init() { gin.SetMode(gin.TestMode) }

func setupRateLimitRouter(rps float64, burst int) (*gin.Engine, *middleware.RateLimiter) {
	rl := middleware.NewRateLimiter(rps, burst)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("userID", "test-user")
		c.Next()
	})
	r.Use(rl.Middleware())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	return r, rl
}

func TestRateLimit_AllowsBurst(t *testing.T) {
	r, rl := setupRateLimitRouter(1, 5) // 1 rps, burst of 5
	defer rl.Stop()

	for i := range 5 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d, want %d", i, w.Code, http.StatusOK)
		}
	}
}

func TestRateLimit_RejectsAfterBurst(t *testing.T) {
	r, rl := setupRateLimitRouter(1, 3) // 1 rps, burst of 3
	defer rl.Stop()

	// Exhaust burst.
	for range 3 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		r.ServeHTTP(w, req)
	}

	// Next request should be rate-limited.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("got %d, want %d", w.Code, http.StatusTooManyRequests)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("missing Retry-After header")
	}
}

func TestRateLimit_IsolatesUsers(t *testing.T) {
	rl := middleware.NewRateLimiter(1, 2) // 1 rps, burst of 2
	defer rl.Stop()

	r := gin.New()
	r.Use(rl.Middleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	// User A exhausts burst.
	for range 2 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		c.Set("userID", "user-a")
		rl.Middleware()(c)
	}

	// User B should still get through (separate limiter).
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("userID", "user-b")
	rl.Middleware()(c)

	if w.Code == http.StatusTooManyRequests {
		t.Error("user-b should not be rate-limited by user-a's usage")
	}
}
