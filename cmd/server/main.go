package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/config"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/health"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	ctxlog "github.com/ErlanBelekov/dist-job-scheduler/internal/log"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/metrics"
	httptransport "github.com/ErlanBelekov/dist-job-scheduler/internal/http"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/handler"
	stripeclient "github.com/ErlanBelekov/dist-job-scheduler/internal/stripe"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
	"github.com/gin-gonic/gin"
	"github.com/lmittmann/tint"
	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	logger := newLogger(cfg.Env, cfg.SlogLevel())

	if cfg.Env != "local" {
		gin.SetMode(gin.ReleaseMode)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		stop()
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	// Users
	userRepo := postgres.NewUserRepository(pool)

	// Billing
	creditRepo := postgres.NewCreditRepository(pool)
	stripeCustomerRepo := postgres.NewStripeCustomerRepository(pool)
	stripeClient := stripeclient.New(cfg.StripeSecretKey, cfg.StripeWebhookSecret)
	billingUsecase := usecase.NewBillingUsecase(
		creditRepo,
		stripeCustomerRepo,
		userRepo,
		stripeClient,
		usecase.BillingConfig{
			CreditsPerDollar: int64(cfg.CreditsPerDollar),
			SuccessURL:       cfg.BillingSuccessURL,
			CancelURL:        cfg.BillingCancelURL,
		},
		logger,
	)
	billingHandler := handler.NewBillingHandler(billingUsecase, logger)

	// Jobs
	jobRepo := postgres.NewJobRepository(pool)
	attemptRepo := postgres.NewAttemptRepository(pool)
	jobUsecase := usecase.NewJobUsecase(jobRepo, attemptRepo, creditRepo)
	jobHandler := handler.NewJobHandler(jobUsecase, logger)

	// Schedules
	scheduleRepo := postgres.NewScheduleRepository(pool, logger)
	scheduleUsecase := usecase.NewScheduleUsecase(scheduleRepo, jobRepo)
	scheduleHandler := handler.NewScheduleHandler(scheduleUsecase, logger)

	// API tokens
	tokenRepo := postgres.NewAPITokenRepository(pool)
	tokenHandler := handler.NewTokenHandler(tokenRepo, logger)

	metrics.Register()
	checker := health.NewChecker(pool, logger, prometheus.DefaultRegisterer)

	srv := http.Server{
		Addr:    ":" + cfg.Port,
		Handler: httptransport.NewRouter(logger, jobHandler, scheduleHandler, tokenHandler, billingHandler, userRepo, creditRepo, tokenRepo, cfg.ClerkJWKSURL, []byte(cfg.JWTSecret)),
	}

	metricsSrv := metrics.NewServer(":"+cfg.MetricsPort, checker)

	go func() {
		logger.Info("server started", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	go func() {
		logger.Info("metrics server started", "port", cfg.MetricsPort)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server", "error", err)
		}
	}()

	<-ctx.Done()
	stop()
	logger.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown", "error", err)
	}
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics server shutdown", "error", err)
	}
}

func newLogger(env string, level slog.Level) *slog.Logger {
	var inner slog.Handler
	if env == "local" {
		inner = tint.NewHandler(os.Stdout, &tint.Options{
			Level:      level,
			TimeFormat: time.Kitchen,
		})
	} else {
		inner = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: level,
		})
	}
	return slog.New(ctxlog.NewContextHandler(inner))
}
