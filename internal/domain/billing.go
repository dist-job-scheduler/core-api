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
	CreditTxJobExecution CreditTxType = "job_execution"
	CreditTxDailyGrant   CreditTxType = "daily_grant"
	CreditTxStripeTopup  CreditTxType = "stripe_topup"
)
