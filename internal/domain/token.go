package domain

import (
	"errors"
	"time"
)

var ErrTokenNotFound = errors.New("api token not found")

type APIToken struct {
	ID         string
	UserID     string
	Name       string
	Prefix     string // "fliq_sk_XXXXXXXX" — for display only
	LastUsedAt *time.Time
	CreatedAt  time.Time
}
