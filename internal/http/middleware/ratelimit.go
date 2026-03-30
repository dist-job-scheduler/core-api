package middleware

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/metrics"
	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter holds per-key token-bucket limiters with automatic cleanup.
type RateLimiter struct {
	mu       sync.Mutex
	limiters map[string]*limiterEntry
	rps      rate.Limit
	burst    int
	stopOnce sync.Once
	done     chan struct{}
}

// NewRateLimiter creates a rate limiter that allows rps requests/sec with the
// given burst size. It starts a background goroutine that evicts stale entries
// every 5 minutes. Call Stop() to release the goroutine.
func NewRateLimiter(rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		limiters: make(map[string]*limiterEntry),
		rps:      rate.Limit(rps),
		burst:    burst,
		done:     make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Stop terminates the background cleanup goroutine.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.done) })
}

func (rl *RateLimiter) getLimiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	entry, ok := rl.limiters[key]
	if !ok {
		entry = &limiterEntry{
			limiter: rate.NewLimiter(rl.rps, rl.burst),
		}
		rl.limiters[key] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-rl.done:
			return
		case <-ticker.C:
			rl.mu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for key, entry := range rl.limiters {
				if entry.lastSeen.Before(cutoff) {
					delete(rl.limiters, key)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// Middleware returns a Gin middleware that rate-limits requests. It uses
// userID from the Gin context (set by the Auth middleware) as the key.
// If userID is not set, it falls back to the client IP.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetString("userID")
		if key == "" {
			key = "ip:" + c.ClientIP()
		}

		limiter := rl.getLimiter(key)
		if !limiter.Allow() {
			metrics.RateLimitExceededTotal.Inc()

			retryAfter := math.Ceil(1 / float64(rl.rps))
			c.Header("Retry-After", strconv.FormatInt(int64(retryAfter), 10))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded",
			})
			return
		}

		c.Next()
	}
}
