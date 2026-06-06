//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedBufferItem inserts a buffer and a buffer item for the user and returns the
// buffer item ID, so the credit_transactions FK to buffer_items is satisfiable.
func seedBufferItem(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	ctx := context.Background()

	var bufferID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO buffers (user_id, name, url) VALUES ($1, 'test', 'https://example.com')
		 RETURNING id`,
		userID,
	).Scan(&bufferID); err != nil {
		t.Fatalf("seed buffer: %v", err)
	}

	var itemID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO buffer_items (buffer_id, user_id, url, method)
		 VALUES ($1, $2, 'https://example.com', 'POST')
		 RETURNING id`,
		bufferID, userID,
	).Scan(&itemID); err != nil {
		t.Fatalf("seed buffer item: %v", err)
	}
	return itemID
}

func TestBillingRepo_DeductForBufferItem(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewCreditRepository(pool)
	ctx := context.Background()

	itemID := seedBufferItem(t, pool, userID)

	balanceBefore, _ := repo.GetBalance(ctx, userID)
	if err := repo.DeductForBufferItem(ctx, userID, itemID); err != nil {
		t.Fatalf("deduct for buffer item: %v", err)
	}
	balanceAfter, _ := repo.GetBalance(ctx, userID)
	if balanceAfter.Balance != balanceBefore.Balance-1 {
		t.Fatalf("balance = %d, want %d", balanceAfter.Balance, balanceBefore.Balance-1)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM credit_transactions
		 WHERE user_id = $1 AND buffer_item_id = $2 AND type = $3 AND amount = -1`,
		userID, itemID, domain.CreditTxBufferExecution,
	).Scan(&count); err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("buffer_item_execution rows = %d, want 1", count)
	}
}

func TestBillingRepo_DeductForBufferItem_PerAttempt(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewCreditRepository(pool)
	ctx := context.Background()

	itemID := seedBufferItem(t, pool, userID)

	balanceBefore, _ := repo.GetBalance(ctx, userID)
	for i := 0; i < 2; i++ {
		if err := repo.DeductForBufferItem(ctx, userID, itemID); err != nil {
			t.Fatalf("deduct attempt %d: %v", i, err)
		}
	}

	balanceAfter, _ := repo.GetBalance(ctx, userID)
	if balanceAfter.Balance != balanceBefore.Balance-2 {
		t.Fatalf("balance = %d, want %d", balanceAfter.Balance, balanceBefore.Balance-2)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM credit_transactions WHERE buffer_item_id = $1`, itemID,
	).Scan(&count); err != nil {
		t.Fatalf("count ledger rows: %v", err)
	}
	if count != 2 {
		t.Fatalf("ledger rows = %d, want 2 (per-attempt billing)", count)
	}
}
