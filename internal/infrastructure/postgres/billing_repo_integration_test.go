//go:build integration

package postgres_test

import (
	"context"
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/infrastructure/postgres"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
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
	if err := repo.Deduct(ctx, userID, created.ID); err != nil {
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
		if err := repo.Deduct(ctx, userID, created.ID); err != nil {
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
