package repository

import (
	"context"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

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
	Deduct(ctx context.Context, userID, jobID string) error

	// TopUp adds credits and records a stripe_topup transaction. Called by the
	// Stripe webhook handler on checkout.session.completed.
	TopUp(ctx context.Context, userID string, amount int64, stripePaymentIntentID string) error

	// UpdatePlan upgrades or downgrades a user's plan.
	UpdatePlan(ctx context.Context, userID string, plan domain.Plan) error
}

// StripeCustomerRepository persists the user ↔ Stripe customer mapping.
type StripeCustomerRepository interface {
	// FindByUserID returns the Stripe customer ID for a user, or "" if none exists.
	FindByUserID(ctx context.Context, userID string) (string, error)

	// Save stores the mapping. Safe to call if a mapping already exists (upsert).
	Save(ctx context.Context, userID, stripeCustomerID string) error
}
