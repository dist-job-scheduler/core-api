package domain

import (
	"errors"
	"time"
)

// ErrSigningSecretNotFound is returned when no active signing secret exists for a user.
var ErrSigningSecretNotFound = errors.New("signing secret not found")

// SigningSecret holds a per-user HMAC signing secret used to sign outgoing requests.
type SigningSecret struct {
	ID        string
	UserID    string
	Secret    string // raw whsec_* value — never hashed
	IsActive  bool
	CreatedAt time.Time
}
