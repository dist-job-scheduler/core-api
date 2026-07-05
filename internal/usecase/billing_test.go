package usecase_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/testutil"
	"github.com/ErlanBelekov/dist-job-scheduler/internal/usecase"
)

func newBillingUsecase(credits *testutil.MockCreditRepository) *usecase.BillingUsecase {
	return usecase.NewBillingUsecase(
		credits,
		&testutil.MockStripeCustomerRepository{},
		&testutil.MockUserRepository{},
		nil, // stripe client — not used in these tests
		usecase.BillingConfig{CreditsPerDollar: 1000, SuccessURL: "https://example.com/ok", CancelURL: "https://example.com/cancel"},
		slog.Default(),
	)
}

func TestGetBalance_HappyPath(t *testing.T) {
	t.Parallel()

	want := &domain.CreditBalance{
		UserID:         "user-1",
		Balance:        50000,
		Plan:           domain.PlanFree,
		DailyFreeLimit: 100000,
		RefreshedAt:    time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	credits := &testutil.MockCreditRepository{
		GetBalanceFn: func(_ context.Context, userID string) (*domain.CreditBalance, error) {
			if userID == "user-1" {
				return want, nil
			}
			return nil, errors.New("not found")
		},
	}
	uc := newBillingUsecase(credits)

	got, err := uc.GetBalance(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Balance != want.Balance {
		t.Fatalf("expected balance %d, got %d", want.Balance, got.Balance)
	}
	if got.UserID != want.UserID {
		t.Fatalf("expected user ID %s, got %s", want.UserID, got.UserID)
	}
}

func TestGetBalance_Error(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("database connection refused")
	credits := &testutil.MockCreditRepository{
		GetBalanceFn: func(_ context.Context, _ string) (*domain.CreditBalance, error) {
			return nil, dbErr
		},
	}
	uc := newBillingUsecase(credits)

	_, err := uc.GetBalance(context.Background(), "user-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, dbErr) {
		t.Fatalf("expected wrapped dbErr, got %v", err)
	}
}

func TestCreateCheckoutSession_RejectsOversizedCredits(t *testing.T) {
	t.Parallel()

	uc := newBillingUsecase(&testutil.MockCreditRepository{})

	cases := []struct {
		name    string
		credits int64
	}{
		{"zero", 0},
		{"negative", -5},
		{"just over cap", 1_000_000_001},
		// The overflow exploit: credits*100 wraps int64 to a tiny amountCents, so
		// a $0.005 charge would otherwise mint ~4.6e18 credits. Must be rejected.
		{"int64 overflow value", 4611686018427437904},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// stripe client is nil; a value that slips past the guard would panic
			// on the customer lookup, so reaching an error return proves the guard
			// fired first.
			_, err := uc.CreateCheckoutSession(context.Background(), "user-1", tc.credits)
			if err == nil {
				t.Fatalf("expected rejection for credits=%d, got nil", tc.credits)
			}
		})
	}
}

func TestListTransactions_DefaultLimit(t *testing.T) {
	t.Parallel()

	var capturedLimit int
	credits := &testutil.MockCreditRepository{
		ListTransactionsFn: func(_ context.Context, _ string, _ string, limit int) ([]domain.CreditTransaction, string, error) {
			capturedLimit = limit
			return nil, "", nil
		},
	}
	uc := newBillingUsecase(credits)

	_, _, err := uc.ListTransactions(context.Background(), "user-1", "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedLimit != 50 {
		t.Fatalf("expected default limit 50, got %d", capturedLimit)
	}
}

func TestListTransactions_ClampLimit(t *testing.T) {
	t.Parallel()

	var capturedLimit int
	credits := &testutil.MockCreditRepository{
		ListTransactionsFn: func(_ context.Context, _ string, _ string, limit int) ([]domain.CreditTransaction, string, error) {
			capturedLimit = limit
			return nil, "", nil
		},
	}
	uc := newBillingUsecase(credits)

	_, _, err := uc.ListTransactions(context.Background(), "user-1", "", 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedLimit != 50 {
		t.Fatalf("expected clamped limit 50, got %d", capturedLimit)
	}
}
