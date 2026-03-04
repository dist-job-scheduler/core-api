package httptransport

import (
	"log/slog"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/middleware"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/gin-gonic/gin"

	sloggin "github.com/samber/slog-gin"
)

func NewRouter(logger *slog.Logger, jobHandler *handler.JobHandler, scheduleHandler *handler.ScheduleHandler, tokenHandler *handler.TokenHandler, billingHandler *handler.BillingHandler, userRepo repository.UserRepository, creditRepo repository.CreditRepository, tokenRepo repository.APITokenRepository, jwksURL string, hmacKey []byte) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.RequestID())
	r.Use(middleware.Security())
	r.Use(sloggin.New(logger))
	r.Use(middleware.Metrics())

	authMW := middleware.Auth(jwksURL, hmacKey, tokenRepo)
	ensureUser := middleware.EnsureUser(userRepo, creditRepo, logger)

	// Protected job routes
	jobs := r.Group("/jobs", authMW, ensureUser)
	jobs.GET("", jobHandler.List)
	jobs.POST("", jobHandler.Create)
	jobs.GET("/:id", jobHandler.GetByID)
	jobs.DELETE("/:id", jobHandler.Cancel)
	jobs.GET("/:id/attempts", jobHandler.ListAttempts)

	// Protected schedule routes
	schedules := r.Group("/schedules", authMW, ensureUser)
	schedules.POST("", scheduleHandler.Create)
	schedules.GET("", scheduleHandler.List)
	schedules.GET("/:id", scheduleHandler.GetByID)
	schedules.POST("/:id/pause", scheduleHandler.Pause)
	schedules.POST("/:id/resume", scheduleHandler.Resume)
	schedules.DELETE("/:id", scheduleHandler.Delete)
	schedules.GET("/:id/jobs", scheduleHandler.ListJobs)

	// Protected token routes
	tokens := r.Group("/tokens", authMW, ensureUser)
	tokens.POST("", tokenHandler.Create)
	tokens.GET("", tokenHandler.List)
	tokens.DELETE("/:id", tokenHandler.Delete)

	// Protected billing routes
	billing := r.Group("/billing", authMW, ensureUser)
	billing.GET("/balance", billingHandler.GetBalance)
	billing.POST("/checkout", billingHandler.CreateCheckoutSession)

	// Webhook has no auth middleware — verified by Stripe signature
	r.POST("/billing/webhook", billingHandler.HandleWebhook)

	return r
}
