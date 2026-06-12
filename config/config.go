package config

import (
	"fmt"
	"log/slog"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
)

type Config struct {
	Env  string `env:"ENV" envDefault:"local" validate:"required,oneof=local staging production"`
	Port string `env:"PORT" envDefault:"8080" validate:"required"`

	DatabaseURL        string `env:"DATABASE_URL,required" validate:"required"`
	WorkerCount        int    `env:"WORKER_COUNT" envDefault:"5" validate:"min=1,max=100"`
	PollIntervalSec    int    `env:"POLL_INTERVAL_SEC" envDefault:"1" validate:"min=1,max=60"`
	DispatchIntervalSec  int  `env:"DISPATCH_INTERVAL_SEC" envDefault:"5" validate:"min=1,max=60"`
	DrainerConcurrency   int  `env:"DRAINER_CONCURRENCY" envDefault:"10" validate:"min=1,max=100"`
	DrainerPollIntervalSec int `env:"DRAINER_POLL_INTERVAL_SEC" envDefault:"1" validate:"min=1,max=10"`

	MetricsPort string `env:"METRICS_PORT" envDefault:"9090"`
	LogLevel    string `env:"LOG_LEVEL" envDefault:"info" validate:"required,oneof=debug info warn error"`

	// ClerkJWKSURL is the JWKS endpoint for RS256 token verification (Clerk).
	// When set, it takes precedence over JWTSecret.
	ClerkJWKSURL string `env:"CLERK_JWKS_URL"`

	// JWTSecret is used for HS256 verification in local dev (when ClerkJWKSURL is empty).
	JWTSecret string `env:"JWT_SECRET"`

	// Stripe
	StripeSecretKey     string `env:"STRIPE_SECRET_KEY"`
	StripeWebhookSecret string `env:"STRIPE_WEBHOOK_SECRET"`

	// FreeCreditsPerDay is the number of credits granted daily to free-plan users.
	FreeCreditsPerDay int `env:"FREE_CREDITS_PER_DAY" envDefault:"100000"`

	// CreditsPerDollar controls the exchange rate (e.g. 100000 = 100k credits per $1).
	// Minimum purchasable amount is always $0.50 (Stripe floor).
	CreditsPerDollar int `env:"CREDITS_PER_DOLLAR" envDefault:"100000"`

	// CORS — comma-separated list of allowed origins (e.g. "https://fliq.sh,http://localhost:3000").
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS" envDefault:"https://fliq.sh,http://localhost:3000"`

	// Rate limiting — requests per second and burst size per user.
	RateLimitRPS   float64 `env:"RATE_LIMIT_RPS" envDefault:"50"`
	RateLimitBurst int     `env:"RATE_LIMIT_BURST" envDefault:"100" validate:"min=1"`

	// Billing URLs for Stripe Checkout redirect.
	BillingSuccessURL string `env:"BILLING_SUCCESS_URL" envDefault:"http://localhost:3000/app/billing/success"`
	BillingCancelURL  string `env:"BILLING_CANCEL_URL" envDefault:"http://localhost:3000/app/billing/cancel"`

	// LowBalanceThreshold is the credit floor that triggers a proactive
	// credit_low alert when a deduction crosses below it. 0 disables the warning.
	LowBalanceThreshold int64 `env:"LOW_BALANCE_THRESHOLD" envDefault:"10000"`

	// Email alerts (Resend). When ResendAPIKey is empty the mailer is a safe
	// no-op: email channels can still be created but nothing is sent, exactly
	// like a nil AlertNotifier. Set both to deliver live.
	ResendAPIKey   string `env:"RESEND_API_KEY"`
	AlertEmailFrom string `env:"ALERT_EMAIL_FROM" envDefault:"alerts@fliq.sh"`

	// AppBaseURL is the public base URL used to build email-verification confirm
	// links (e.g. https://api.fliq.sh). Falls back to localhost for local dev.
	AppBaseURL string `env:"APP_BASE_URL" envDefault:"http://localhost:8080"`
}

func Load() (*Config, error) {
	cfg := &Config{}

	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}

	if err := validator.New().Struct(cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// SlogLevel converts the LOG_LEVEL string to a slog.Level.
func (c *Config) SlogLevel() slog.Level {
	switch c.LogLevel {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
