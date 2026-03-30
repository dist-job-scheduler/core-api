package httptransport

import (
	"log/slog"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/middleware"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/gin-gonic/gin"

	sloggin "github.com/samber/slog-gin"
)

func NewRouter(logger *slog.Logger, jobHandler *handler.JobHandler, scheduleHandler *handler.ScheduleHandler, tokenHandler *handler.TokenHandler, billingHandler *handler.BillingHandler, signingHandler *handler.SigningHandler, userRepo repository.UserRepository, creditRepo repository.CreditRepository, tokenRepo repository.APITokenRepository, jwksURL string, hmacKey []byte, corsAllowedOrigins string, rateLimiter *middleware.RateLimiter) *gin.Engine {
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

	// Webhook has no auth middleware — verified by Stripe signature
	r.POST("/billing/webhook", billingHandler.HandleWebhook)

	return r
}
