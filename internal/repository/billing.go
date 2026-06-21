package repository

import (
	"context"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

// LowBalanceCrossing reports a downward crossing of the low-balance threshold,
// returned by Deduct/DeductForBufferItem when the deduction that just ran took
// the balance below the threshold for the first time since the last top-up.
type LowBalanceCrossing struct {
	// Balance is the balance after the deduction (at the moment of crossing).
	Balance int64
	// Threshold is the floor that was crossed.
	Threshold int64
	// RecentBurn is the credits spent over the trailing
	// domain.CreditLowBurnWindow, for the "you'll run dry in ~N" hint.
	RecentBurn int64
}

// CreditRepository manages per-user credit balances and the immutable audit ledger.
type CreditRepository interface {
	// EnsureExists inserts a default credit row for the user if one does not already exist.
	// Called on every authenticated request via EnsureUser middleware.
	EnsureExists(ctx context.Context, userID string) error

	// GetBalance returns the user's current credit balance.
	GetBalance(ctx context.Context, userID string) (*domain.CreditBalance, error)

	// HasCredits checks whether the user has a positive balance.
	// For free-plan users it first applies a lazy UTC-day refresh if the last
	// refresh was before today, granting daily_free_limit credits and recording
	// a daily_grant transaction. Returns true if balance > 0 after any refresh.
	HasCredits(ctx context.Context, userID string) (bool, error)

	// Deduct subtracts 1 credit and records a job_execution transaction.
	// Called after every execution attempt (success or failure). A brief
	// negative balance is acceptable; the creation gate (HasCredits) prevents
	// sustained overdraft.
	//
	// threshold is the low-balance warning floor (0 disables it). When this
	// deduction takes the balance from at-or-above threshold to below it — and
	// the user has not already been notified for this crossing — a non-nil
	// LowBalanceCrossing is returned so the caller can fan out a credit_low
	// alert. The hysteresis latch is updated atomically in the same transaction,
	// so concurrent workers fire at most once per crossing.
	Deduct(ctx context.Context, userID, jobID string, threshold int64) (*LowBalanceCrossing, error)

	// DeductForBufferItem subtracts 1 credit and records a buffer_item_execution
	// transaction. Like Deduct, it is called once per execution attempt (success
	// or failure), is intentionally not idempotent, and reports a low-balance
	// crossing under the same hysteresis rules.
	DeductForBufferItem(ctx context.Context, userID, bufferItemID string, threshold int64) (*LowBalanceCrossing, error)

	// TopUp adds credits and records a stripe_topup transaction. Called by the
	// Stripe webhook handler on checkout.session.completed.
	TopUp(ctx context.Context, userID string, amount int64, stripePaymentIntentID string) error

	// UpdatePlan upgrades or downgrades a user's plan.
	UpdatePlan(ctx context.Context, userID string, plan domain.Plan) error

	// ListTransactions returns credit transactions for the user with cursor-based pagination.
	// Ordered by (created_at DESC, id DESC). cursor is opaque; pass "" for first page.
	ListTransactions(ctx context.Context, userID string, cursor string, limit int) ([]domain.CreditTransaction, string, error)
}

// StripeCustomerRepository persists the user ↔ Stripe customer mapping.
type StripeCustomerRepository interface {
	// FindByUserID returns the Stripe customer ID for a user, or "" if none exists.
	FindByUserID(ctx context.Context, userID string) (string, error)

	// Save stores the mapping. Safe to call if a mapping already exists (upsert).
	Save(ctx context.Context, userID, stripeCustomerID string) error
}
