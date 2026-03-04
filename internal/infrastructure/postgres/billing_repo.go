package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CreditRepo implements repository.CreditRepository.
type CreditRepo struct {
	pool *pgxpool.Pool
}

func NewCreditRepository(pool *pgxpool.Pool) *CreditRepo {
	return &CreditRepo{pool: pool}
}

func (r *CreditRepo) EnsureExists(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_credits (user_id, balance, plan, refreshed_at)
		 VALUES ($1, 5000, 'free', NOW())
		 ON CONFLICT (user_id) DO NOTHING`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("ensure credit row: %w", err)
	}
	return nil
}

func (r *CreditRepo) GetBalance(ctx context.Context, userID string) (*domain.CreditBalance, error) {
	var b domain.CreditBalance
	err := r.pool.QueryRow(ctx,
		`SELECT user_id, balance, plan, daily_free_limit, refreshed_at, updated_at
		 FROM user_credits WHERE user_id = $1`,
		userID,
	).Scan(&b.UserID, &b.Balance, &b.Plan, &b.DailyFreeLimit, &b.RefreshedAt, &b.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, fmt.Errorf("get balance: %w", err)
	}
	return &b, nil
}

// HasCredits applies a lazy daily refresh for free users, then returns balance > 0.
// This runs in a transaction so the refresh and the balance read are consistent.
func (r *CreditRepo) HasCredits(ctx context.Context, userID string) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// Lazy daily refresh: only for free-plan users whose last refresh was before today (UTC).
	var refreshed bool
	var dailyLimit int64
	tag, refreshErr := tx.Exec(ctx,
		`UPDATE user_credits
		 SET balance          = daily_free_limit,
		     refreshed_at     = NOW(),
		     updated_at       = NOW()
		 WHERE user_id = $1
		   AND plan    = 'free'
		   AND DATE(refreshed_at AT TIME ZONE 'UTC') < CURRENT_DATE`,
		userID,
	)
	if refreshErr != nil {
		err = refreshErr
		return false, fmt.Errorf("daily refresh: %w", err)
	}
	if tag.RowsAffected() > 0 {
		refreshed = true
		// Read back the daily limit so we can record it in the ledger.
		scanErr := tx.QueryRow(ctx,
			`SELECT daily_free_limit FROM user_credits WHERE user_id = $1`, userID,
		).Scan(&dailyLimit)
		if scanErr != nil {
			err = scanErr
			return false, fmt.Errorf("read daily limit: %w", err)
		}
	}

	// Record a daily_grant transaction if we just refreshed.
	if refreshed {
		_, insertErr := tx.Exec(ctx,
			`INSERT INTO credit_transactions (user_id, amount, type, description)
			 VALUES ($1, $2, $3, 'daily free credit refresh')`,
			userID, dailyLimit, domain.CreditTxDailyGrant,
		)
		if insertErr != nil {
			err = insertErr
			return false, fmt.Errorf("insert daily grant tx: %w", err)
		}
	}

	// Read current balance.
	var balance int64
	scanErr := tx.QueryRow(ctx,
		`SELECT balance FROM user_credits WHERE user_id = $1`, userID,
	).Scan(&balance)
	if scanErr != nil {
		err = scanErr
		return false, fmt.Errorf("read balance: %w", err)
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		err = commitErr
		return false, fmt.Errorf("commit: %w", err)
	}

	return balance > 0, nil
}

func (r *CreditRepo) Deduct(ctx context.Context, userID, jobID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	_, err = tx.Exec(ctx,
		`UPDATE user_credits SET balance = balance - 1, updated_at = NOW() WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return fmt.Errorf("deduct credit: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO credit_transactions (user_id, amount, type, job_id)
		 VALUES ($1, -1, $2, $3)`,
		userID, domain.CreditTxJobExecution, jobID,
	)
	if err != nil {
		return fmt.Errorf("insert deduct tx: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (r *CreditRepo) TopUp(ctx context.Context, userID string, amount int64, stripePaymentIntentID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	_, err = tx.Exec(ctx,
		`UPDATE user_credits SET balance = balance + $2, updated_at = NOW() WHERE user_id = $1`,
		userID, amount,
	)
	if err != nil {
		return fmt.Errorf("top up credits: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO credit_transactions (user_id, amount, type, stripe_payment_intent_id)
		 VALUES ($1, $2, $3, $4)`,
		userID, amount, domain.CreditTxStripeTopup, stripePaymentIntentID,
	)
	if err != nil {
		return fmt.Errorf("insert topup tx: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (r *CreditRepo) UpdatePlan(ctx context.Context, userID string, plan domain.Plan) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE user_credits SET plan = $2, updated_at = NOW() WHERE user_id = $1`,
		userID, plan,
	)
	if err != nil {
		return fmt.Errorf("update plan: %w", err)
	}
	return nil
}

// StripeCustomerRepo implements repository.StripeCustomerRepository.
type StripeCustomerRepo struct {
	pool *pgxpool.Pool
}

func NewStripeCustomerRepository(pool *pgxpool.Pool) *StripeCustomerRepo {
	return &StripeCustomerRepo{pool: pool}
}

func (r *StripeCustomerRepo) FindByUserID(ctx context.Context, userID string) (string, error) {
	var customerID string
	err := r.pool.QueryRow(ctx,
		`SELECT stripe_customer_id FROM stripe_customers WHERE user_id = $1`,
		userID,
	).Scan(&customerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("find stripe customer: %w", err)
	}
	return customerID, nil
}

func (r *StripeCustomerRepo) Save(ctx context.Context, userID, stripeCustomerID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO stripe_customers (user_id, stripe_customer_id)
		 VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET stripe_customer_id = EXCLUDED.stripe_customer_id`,
		userID, stripeCustomerID,
	)
	if err != nil {
		return fmt.Errorf("save stripe customer: %w", err)
	}
	return nil
}
