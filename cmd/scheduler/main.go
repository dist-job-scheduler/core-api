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
	"github.com/ErlanBelekov/dist-job-scheduler/internal/mailer"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/metrics"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/scheduler"
	"github.com/lmittmann/tint"
	"github.com/prometheus/client_golang/prometheus"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	logger := newLogger(cfg.Env, cfg.SlogLevel())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		stop()
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	logger.Info("db connected")

	metrics.Register()
	checker := health.NewChecker(pool, logger, prometheus.DefaultRegisterer)

	jobRepo := postgres.NewJobRepository(pool)
	attemptRepo := postgres.NewAttemptRepository(pool)
	scheduleRepo := postgres.NewScheduleRepository(pool, logger)
	creditRepo := postgres.NewCreditRepository(pool)

	signingRepo := postgres.NewSigningSecretRepository(pool)
	alertRepo := postgres.NewAlertChannelRepository(pool)

	var mailProvider mailer.Provider
	if cfg.ResendAPIKey == "" {
		logger.Warn("RESEND_API_KEY unset — email alerts will be dropped (no-op mailer)")
		mailProvider = mailer.NewNoopProvider(logger)
	} else {
		mailProvider = mailer.NewResendProvider(cfg.ResendAPIKey, cfg.AlertEmailFrom)
	}

	notifier := scheduler.NewWebhookNotifier(logger, signingRepo)
	alertNotifier := scheduler.NewAlertNotifier(logger, alertRepo, mailProvider)

	lowBalance := scheduler.LowBalanceConfig{
		Threshold: cfg.LowBalanceThreshold,
		TopUpURL:  cfg.BillingSuccessURL,
	}

	worker := scheduler.NewWorker(
		jobRepo,
		attemptRepo,
		creditRepo,
		notifier,
		alertNotifier,
		lowBalance,
		logger,
		time.Duration(cfg.PollIntervalSec)*time.Second,
		cfg.WorkerCount,
		signingRepo,
	)
	go worker.Start(ctx)

	// heartbeat fires every 10s — 30s timeout means 3 missed beats before a job is stale
	reaper := scheduler.NewReaper(jobRepo, logger, 30*time.Second, 30*time.Second)
	go reaper.Start(ctx)

	dispatcher := scheduler.NewDispatcher(scheduleRepo, creditRepo, logger, time.Duration(cfg.DispatchIntervalSec)*time.Second)
	go dispatcher.Start(ctx)

	bufferRepo := postgres.NewBufferRepository(pool)
	drainer := scheduler.NewBufferDrainer(
		bufferRepo, creditRepo, signingRepo, notifier, alertNotifier, lowBalance, logger,
		time.Duration(cfg.DrainerPollIntervalSec)*time.Second,
		cfg.DrainerConcurrency,
	)
	go drainer.Start(ctx)

	bufferReaper := scheduler.NewBufferReaper(bufferRepo, logger, 30*time.Second, 30*time.Second)
	go bufferReaper.Start(ctx)

	metricsSrv := metrics.NewServer(":"+cfg.MetricsPort, checker)
	go func() {
		logger.Info("metrics server started", "port", cfg.MetricsPort)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server", "error", err)
		}
	}()

	<-ctx.Done()
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("metrics server shutdown", "error", err)
	}

	logger.Info("scheduler shut down")
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
