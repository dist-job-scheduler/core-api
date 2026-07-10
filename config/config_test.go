package config

import (
	"log/slog"
	"testing"
)

// setMinimalEnv sets only the variables required for a valid config, so each
// test starts from a known-good baseline and overrides just what it exercises.
// DATABASE_URL is the only env-level `required` field; everything else has a
// default. t.Setenv restores the previous environment after the test and marks
// it as non-parallel, so the cases don't race on the shared process env.
func setMinimalEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db?sslmode=disable")
}

func TestLoad_Defaults(t *testing.T) {
	setMinimalEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	// Spot-check that envDefault tags are applied when the var is unset. These
	// are the operational defaults the binaries boot with; a regression that
	// silently changes one should fail here.
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"Env", cfg.Env, "local"},
		{"Port", cfg.Port, "8080"},
		{"WorkerCount", cfg.WorkerCount, 5},
		{"PollIntervalSec", cfg.PollIntervalSec, 1},
		{"DispatchIntervalSec", cfg.DispatchIntervalSec, 5},
		{"DrainerConcurrency", cfg.DrainerConcurrency, 10},
		{"DrainerPollIntervalSec", cfg.DrainerPollIntervalSec, 1},
		{"PartitionMaintainIntervalSec", cfg.PartitionMaintainIntervalSec, 21600},
		{"PartitionMonthsAhead", cfg.PartitionMonthsAhead, 3},
		{"JobAttemptRetentionMonths", cfg.JobAttemptRetentionMonths, 0},
		{"MetricsPort", cfg.MetricsPort, "9090"},
		{"LogLevel", cfg.LogLevel, "info"},
		// Billing defaults — the free grant and exchange rate the live system
		// boots with. Pinned because these are the source of truth for what a
		// free-plan user actually receives per day.
		{"FreeCreditsPerDay", cfg.FreeCreditsPerDay, 100000},
		{"CreditsPerDollar", cfg.CreditsPerDollar, 100000},
		{"RateLimitRPS", cfg.RateLimitRPS, float64(50)},
		{"RateLimitBurst", cfg.RateLimitBurst, 100},
		{"LowBalanceThreshold", cfg.LowBalanceThreshold, int64(10000)},
		{"AlertEmailFrom", cfg.AlertEmailFrom, "alerts@fliq.sh"},
		{"AppBaseURL", cfg.AppBaseURL, "http://localhost:8080"},
		{"CORSAllowedOrigins", cfg.CORSAllowedOrigins, "https://fliq.sh,http://localhost:3000"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLoad_OverridesParsed(t *testing.T) {
	setMinimalEnv(t)
	t.Setenv("ENV", "production")
	t.Setenv("PORT", "9000")
	t.Setenv("WORKER_COUNT", "20")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("FREE_CREDITS_PER_DAY", "5000")
	t.Setenv("RATE_LIMIT_RPS", "12.5")
	t.Setenv("LOW_BALANCE_THRESHOLD", "250000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if cfg.Env != "production" {
		t.Errorf("Env = %q, want %q", cfg.Env, "production")
	}
	if cfg.Port != "9000" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9000")
	}
	if cfg.WorkerCount != 20 {
		t.Errorf("WorkerCount = %d, want 20", cfg.WorkerCount)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.FreeCreditsPerDay != 5000 {
		t.Errorf("FreeCreditsPerDay = %d, want 5000", cfg.FreeCreditsPerDay)
	}
	if cfg.RateLimitRPS != 12.5 {
		t.Errorf("RateLimitRPS = %v, want 12.5", cfg.RateLimitRPS)
	}
	if cfg.LowBalanceThreshold != 250000 {
		t.Errorf("LowBalanceThreshold = %d, want 250000", cfg.LowBalanceThreshold)
	}
}

func TestLoad_RejectsInvalid(t *testing.T) {
	tests := []struct {
		name   string
		setenv map[string]string
	}{
		{"empty DATABASE_URL", map[string]string{"DATABASE_URL": ""}},
		{"unknown ENV", map[string]string{"ENV": "qa"}},
		{"unknown LOG_LEVEL", map[string]string{"LOG_LEVEL": "trace"}},
		{"WorkerCount below min", map[string]string{"WORKER_COUNT": "0"}},
		{"WorkerCount above max", map[string]string{"WORKER_COUNT": "101"}},
		{"PollIntervalSec above max", map[string]string{"POLL_INTERVAL_SEC": "61"}},
		{"PartitionMonthsAhead above max", map[string]string{"PARTITION_MONTHS_AHEAD": "25"}},
		{"JobAttemptRetentionMonths above max", map[string]string{"JOB_ATTEMPT_RETENTION_MONTHS": "121"}},
		{"RateLimitBurst below min", map[string]string{"RATE_LIMIT_BURST": "0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setMinimalEnv(t)
			for k, v := range tt.setenv {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatalf("Load() error = nil, want non-nil for %s", tt.name)
			}
		})
	}
}

func TestSlogLevel(t *testing.T) {
	tests := []struct {
		level string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},        // unset → default branch
		{"unknown", slog.LevelInfo}, // anything unrecognised → default branch
	}
	for _, tt := range tests {
		c := &Config{LogLevel: tt.level}
		if got := c.SlogLevel(); got != tt.want {
			t.Errorf("SlogLevel(%q) = %v, want %v", tt.level, got, tt.want)
		}
	}
}
