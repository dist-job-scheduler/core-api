package testutil

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SetupTestDB connects to the test database and returns a pool.
// It skips the test if TEST_DATABASE_URL is not set.
// The pool is closed when the test finishes.
func SetupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL or DATABASE_URL not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

// TruncateAll truncates all application tables to reset test state.
// Call this in TestMain or at the start of each integration test.
func TruncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use a single statement so missing tables don't cause failures.
	// TRUNCATE ... CASCADE handles FK ordering automatically.
	_, err := pool.Exec(ctx, `
		DO $$
		DECLARE
			tbl text;
		BEGIN
			FOR tbl IN
				SELECT tablename FROM pg_tables
				WHERE schemaname = 'public'
				  AND tablename IN (
					'credit_transactions','job_attempts','jobs','schedules',
					'signing_secrets','api_tokens','user_credits',
					'stripe_customers','users','magic_tokens'
				  )
			LOOP
				EXECUTE format('TRUNCATE %I CASCADE', tbl);
			END LOOP;
		END $$;
	`)
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
}

// SeedUser inserts a test user and ensures a credit row exists.
func SeedUser(t *testing.T, pool *pgxpool.Pool, userID string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := pool.Exec(ctx, `INSERT INTO users (id) VALUES ($1) ON CONFLICT DO NOTHING`, userID)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(ctx,
		`INSERT INTO user_credits (user_id, balance, plan, daily_free_limit, refreshed_at, updated_at)
		 VALUES ($1, 100000, 'free', 100000, NOW(), NOW())
		 ON CONFLICT DO NOTHING`, userID)
	if err != nil {
		t.Fatalf("seed user credits: %v", err)
	}
}
