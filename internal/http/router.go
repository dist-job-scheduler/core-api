package httptransport

import (
	"log/slog"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/middleware"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/gin-gonic/gin"

	sloggin "github.com/samber/slog-gin"
)

func NewRouter(logger *slog.Logger, jobHandler *handler.JobHandler, scheduleHandler *handler.ScheduleHandler, tokenHandler *handler.TokenHandler, billingHandler *handler.BillingHandler, signingHandler *handler.SigningHandler, bufferHandler *handler.BufferHandler, alertHandler *handler.AlertHandler, statsHandler *handler.StatsHandler, webhookHandler *handler.WebhookHandler, userRepo repository.UserRepository, creditRepo repository.CreditRepository, tokenRepo repository.APITokenRepository, jwksURL string, hmacKey []byte, corsAllowedOrigins string, rateLimiter *middleware.RateLimiter) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.CORS(corsAllowedOrigins))
	r.Use(middleware.Security())
	r.Use(sloggin.New(logger))
	r.Use(middleware.Metrics())

	authMW := middleware.Auth(jwksURL, hmacKey, tokenRepo)
	ensureUser := middleware.EnsureUser(userRepo, creditRepo, logger)

	// Protected job routes
	jobs := r.Group("/jobs", authMW, ensureUser, rateLimiter.Middleware())
	jobs.GET("", jobHandler.List)
	jobs.POST("", jobHandler.Create)
	jobs.GET("/:id", jobHandler.GetByID)
	jobs.DELETE("/:id", jobHandler.Cancel)
	jobs.POST("/:id/replay", jobHandler.Replay)
	jobs.GET("/:id/attempts", jobHandler.ListAttempts)

	// Protected schedule routes
	schedules := r.Group("/schedules", authMW, ensureUser, rateLimiter.Middleware())
	schedules.POST("", scheduleHandler.Create)
	schedules.GET("", scheduleHandler.List)
	schedules.GET("/:id", scheduleHandler.GetByID)
	schedules.POST("/:id/pause", scheduleHandler.Pause)
	schedules.POST("/:id/resume", scheduleHandler.Resume)
	schedules.DELETE("/:id", scheduleHandler.Delete)
	schedules.GET("/:id/jobs", scheduleHandler.ListJobs)

	// Protected buffer routes
	buffers := r.Group("/buffers", authMW, ensureUser, rateLimiter.Middleware())
	buffers.POST("", bufferHandler.Create)
	buffers.GET("", bufferHandler.List)
	buffers.GET("/:id", bufferHandler.GetByID)
	buffers.GET("/:id/stats", bufferHandler.Stats)
	buffers.POST("/:id/pause", bufferHandler.Pause)
	buffers.POST("/:id/resume", bufferHandler.Resume)
	buffers.DELETE("/:id", bufferHandler.Delete)
	buffers.POST("/:id/items", bufferHandler.PushItem)
	buffers.GET("/:id/items", bufferHandler.ListItems)
	buffers.GET("/:id/items/:itemId", bufferHandler.GetItem)
	buffers.POST("/:id/items/:itemId/replay", bufferHandler.ReplayItem)

	// Public email-verification confirm endpoint. No auth: the signed token in
	// the query string is the credential. Registered before the param route so
	// "verify" is matched as a static segment, not as an :id.
	r.GET("/alerts/verify", alertHandler.Verify)

	// Protected alert channel routes
	alerts := r.Group("/alerts", authMW, ensureUser, rateLimiter.Middleware())
	alerts.POST("", alertHandler.Create)
	alerts.GET("", alertHandler.List)
	alerts.GET("/:id", alertHandler.GetByID)
	alerts.PATCH("/:id", alertHandler.Update)
	alerts.DELETE("/:id", alertHandler.Delete)

	// Protected analytics routes
	stats := r.Group("/stats", authMW, ensureUser, rateLimiter.Middleware())
	stats.GET("/usage", statsHandler.Usage)
	stats.GET("/jobs", statsHandler.Jobs)

	// Protected token routes
	tokens := r.Group("/tokens", authMW, ensureUser, rateLimiter.Middleware())
	tokens.POST("", tokenHandler.Create)
	tokens.GET("", tokenHandler.List)
	tokens.DELETE("/:id", tokenHandler.Delete)

	// Protected billing routes
	billing := r.Group("/billing", authMW, ensureUser, rateLimiter.Middleware())
	billing.GET("/balance", billingHandler.GetBalance)
	billing.POST("/checkout", billingHandler.CreateCheckoutSession)
	billing.GET("/transactions", billingHandler.ListTransactions)

	// Protected signing secret routes
	signing := r.Group("/signing-secret", authMW, ensureUser, rateLimiter.Middleware())
	signing.GET("", signingHandler.Get)
	signing.POST("/rotate", signingHandler.Rotate)

	// Protected webhook delivery log (read-only; the scheduler produces the rows)
	webhooks := r.Group("/webhooks", authMW, ensureUser, rateLimiter.Middleware())
	webhooks.GET("/deliveries", webhookHandler.ListDeliveries)

	// Webhook has no auth middleware — verified by Stripe signature
	r.POST("/billing/webhook", billingHandler.HandleWebhook)

	// Public, unauthenticated liveness endpoint. Polled by the marketing
	// site's uptime widget and external monitors. No auth, no rate limit,
	// no DB — reports that the API process is serving.
	r.GET("/health", handler.Health)

	return r
}
