package domain

import (
	"errors"
	"time"
)

var ErrInsufficientCredits = errors.New("insufficient credits")

type Plan string

const (
	PlanFree Plan = "free"
	PlanPaid Plan = "paid"
)

type CreditBalance struct {
	UserID         string
	Balance        int64
	Plan           Plan
	DailyFreeLimit int
	RefreshedAt    time.Time
	UpdatedAt      time.Time
}

type CreditTxType string

const (
	CreditTxJobExecution    CreditTxType = "job_execution"
	CreditTxBufferExecution CreditTxType = "buffer_item_execution"
	CreditTxDailyGrant      CreditTxType = "daily_grant"
	CreditTxStripeTopup     CreditTxType = "stripe_topup"
)

type CreditTransaction struct {
	ID                    string       `json:"id"`
	UserID                string       `json:"-"`
	Amount                int64        `json:"amount"`
	Type                  CreditTxType `json:"type"`
	JobID                 *string      `json:"job_id"`
	StripePaymentIntentID *string      `json:"stripe_payment_intent_id"`
	Description           *string      `json:"description"`
	CreatedAt             time.Time    `json:"created_at"`
}
