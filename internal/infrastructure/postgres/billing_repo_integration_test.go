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

func setupBillingRepo(t *testing.T) (*postgres.CreditRepo, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	return postgres.NewCreditRepository(pool), userID
}

func TestBillingRepo_EnsureExists_Idempotent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	repo := postgres.NewCreditRepository(pool)
	ctx := context.Background()

	userID := testutil.UserID()
	// Create user first (FK constraint)
	userRepo := postgres.NewUserRepository(pool)
	if err := userRepo.Upsert(ctx, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	if err := repo.EnsureExists(ctx, userID); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := repo.EnsureExists(ctx, userID); err != nil {
		t.Fatalf("second ensure (idempotent): %v", err)
	}

	balance, err := repo.GetBalance(ctx, userID)
	if err != nil {
		t.Fatalf("get balance: %v", err)
	}
	if balance.Balance != 100000 {
		t.Fatalf("balance = %d, want 100000", balance.Balance)
	}
	if balance.Plan != domain.PlanFree {
		t.Fatalf("plan = %s, want free", balance.Plan)
	}
}

func TestBillingRepo_HasCredits(t *testing.T) {
	repo, userID := setupBillingRepo(t)
	ctx := context.Background()

	ok, err := repo.HasCredits(ctx, userID)
	if err != nil {
		t.Fatalf("has credits: %v", err)
	}
	if !ok {
		t.Fatal("expected true, user has 100000 credits")
	}
}

func TestBillingRepo_Deduct(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewCreditRepository(pool)
	jobRepo := postgres.NewJobRepository(pool)
	ctx := context.Background()

	// Create a real job so FK constraint is satisfied
	job := testutil.NewJob(testutil.WithUserID(userID))
	created, err := jobRepo.Create(ctx, job)
	if err != nil {
		t.Fatalf("create job: %v", err)
	}

	balanceBefore, _ := repo.GetBalance(ctx, userID)
	if _, err := repo.Deduct(ctx, userID, created.ID, 0); err != nil {
		t.Fatalf("deduct: %v", err)
	}
	balanceAfter, _ := repo.GetBalance(ctx, userID)
	if balanceAfter.Balance != balanceBefore.Balance-1 {
		t.Fatalf("balance = %d, want %d", balanceAfter.Balance, balanceBefore.Balance-1)
	}

	// Verify transaction was recorded
	txs, _, err := repo.ListTransactions(ctx, userID, "", 10)
	if err != nil {
		t.Fatalf("list transactions: %v", err)
	}
	found := false
	for _, tx := range txs {
		if tx.Type == domain.CreditTxJobExecution && tx.Amount == -1 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected job_execution transaction")
	}
}

func TestBillingRepo_TopUp(t *testing.T) {
	repo, userID := setupBillingRepo(t)
	ctx := context.Background()

	balanceBefore, _ := repo.GetBalance(ctx, userID)
	if err := repo.TopUp(ctx, userID, 5000, "pi_test_123"); err != nil {
		t.Fatalf("top up: %v", err)
	}
	balanceAfter, _ := repo.GetBalance(ctx, userID)
	if balanceAfter.Balance != balanceBefore.Balance+5000 {
		t.Fatalf("balance = %d, want %d", balanceAfter.Balance, balanceBefore.Balance+5000)
	}
}

func TestBillingRepo_UpdatePlan(t *testing.T) {
	repo, userID := setupBillingRepo(t)
	ctx := context.Background()

	if err := repo.UpdatePlan(ctx, userID, domain.PlanPaid); err != nil {
		t.Fatalf("update plan: %v", err)
	}
	balance, _ := repo.GetBalance(ctx, userID)
	if balance.Plan != domain.PlanPaid {
		t.Fatalf("plan = %s, want paid", balance.Plan)
	}
}

func TestBillingRepo_ListTransactions_Pagination(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewCreditRepository(pool)
	jobRepo := postgres.NewJobRepository(pool)
	ctx := context.Background()

	// Create 5 jobs and deduct for each
	for i := 0; i < 5; i++ {
		job := testutil.NewJob(testutil.WithUserID(userID))
		created, err := jobRepo.Create(ctx, job)
		if err != nil {
			t.Fatalf("create job %d: %v", i, err)
		}
		if _, err := repo.Deduct(ctx, userID, created.ID, 0); err != nil {
			t.Fatalf("deduct %d: %v", i, err)
		}
	}

	// First page of 3
	txs, cursor, err := repo.ListTransactions(ctx, userID, "", 3)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(txs) != 3 {
		t.Fatalf("page 1 got %d, want 3", len(txs))
	}
	if cursor == "" {
		t.Fatal("expected non-empty cursor for next page")
	}

	// Second page
	txs2, cursor2, err := repo.ListTransactions(ctx, userID, cursor, 3)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(txs2) != 2 {
		t.Fatalf("page 2 got %d, want 2", len(txs2))
	}
	if cursor2 != "" {
		t.Fatalf("expected empty cursor on last page, got %s", cursor2)
	}
}

// setBalance forces a user's credit balance for threshold-crossing tests.
func setBalance(t *testing.T, pool *pgxpool.Pool, userID string, balance int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE user_credits SET balance = $2 WHERE user_id = $1`, userID, balance,
	); err != nil {
		t.Fatalf("set balance: %v", err)
	}
}

// TestBillingRepo_Deduct_LowBalanceCrossing exercises the latch-based crossing
// detection in deduct(): a crossing fires exactly once on the downward edge,
// stays silent below the threshold, and re-arms after a top-up.
func TestBillingRepo_Deduct_LowBalanceCrossing(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewCreditRepository(pool)
	jobRepo := postgres.NewJobRepository(pool)
	ctx := context.Background()

	const threshold int64 = 5

	mkJob := func() string {
		t.Helper()
		created, err := jobRepo.Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
		if err != nil {
			t.Fatalf("create job: %v", err)
		}
		return created.ID
	}

	// Sitting exactly at the threshold; the next deduction crosses it.
	setBalance(t, pool, userID, threshold)

	crossing, err := repo.Deduct(ctx, userID, mkJob(), threshold)
	if err != nil {
		t.Fatalf("deduct (crossing): %v", err)
	}
	if crossing == nil {
		t.Fatal("expected a low-balance crossing, got nil")
	}
	if crossing.Balance != threshold-1 {
		t.Fatalf("crossing balance = %d, want %d", crossing.Balance, threshold-1)
	}
	if crossing.Threshold != threshold {
		t.Fatalf("crossing threshold = %d, want %d", crossing.Threshold, threshold)
	}
	if crossing.RecentBurn < 1 {
		t.Fatalf("crossing recent burn = %d, want >= 1", crossing.RecentBurn)
	}

	// Already below the threshold: the latch is set, so no further crossing.
	crossing2, err := repo.Deduct(ctx, userID, mkJob(), threshold)
	if err != nil {
		t.Fatalf("deduct (latched): %v", err)
	}
	if crossing2 != nil {
		t.Fatalf("expected no second crossing while latched, got %+v", crossing2)
	}

	// A top-up re-arms the latch; dropping back to the threshold crosses again.
	if err := repo.TopUp(ctx, userID, 100, "pi_test_lowbalance"); err != nil {
		t.Fatalf("top up: %v", err)
	}
	setBalance(t, pool, userID, threshold)

	crossing3, err := repo.Deduct(ctx, userID, mkJob(), threshold)
	if err != nil {
		t.Fatalf("deduct (re-armed): %v", err)
	}
	if crossing3 == nil {
		t.Fatal("expected crossing to re-fire after top-up, got nil")
	}
}

// TestBillingRepo_Deduct_ThresholdDisabled confirms a zero threshold never
// reports a crossing, even as the balance falls through small values.
func TestBillingRepo_Deduct_ThresholdDisabled(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	testutil.TruncateAll(t, pool)
	userID := testutil.UserID()
	testutil.SeedUser(t, pool, userID)
	repo := postgres.NewCreditRepository(pool)
	jobRepo := postgres.NewJobRepository(pool)
	ctx := context.Background()

	setBalance(t, pool, userID, 2)
	for i := 0; i < 2; i++ {
		created, err := jobRepo.Create(ctx, testutil.NewJob(testutil.WithUserID(userID)))
		if err != nil {
			t.Fatalf("create job: %v", err)
		}
		crossing, err := repo.Deduct(ctx, userID, created.ID, 0)
		if err != nil {
			t.Fatalf("deduct %d: %v", i, err)
		}
		if crossing != nil {
			t.Fatalf("threshold 0 must never cross, got %+v", crossing)
		}
	}
}
