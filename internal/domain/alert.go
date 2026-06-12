package domain

import (
	"errors"
	"time"
)

var (
	ErrAlertChannelNotFound    = errors.New("alert channel not found")
	ErrInvalidAlertChannelType = errors.New("invalid alert channel type")
	// ErrAlertChannelNotVerified is returned when enabling an email channel whose
	// address has not yet confirmed ownership.
	ErrAlertChannelNotVerified = errors.New("alert channel not verified")
	// ErrInvalidVerificationToken is returned when an email-verification confirm
	// token is malformed, expired, or its signature does not validate.
	ErrInvalidVerificationToken = errors.New("invalid verification token")
)

// AlertChannelType is the delivery mechanism for an alert channel.
type AlertChannelType string

const (
	// AlertChannelWebhook POSTs a structured JSON payload to the target URL.
	AlertChannelWebhook AlertChannelType = "webhook"
	// AlertChannelSlack POSTs a Slack-incoming-webhook message ({"text": ...})
	// to the target URL.
	AlertChannelSlack AlertChannelType = "slack"
	// AlertChannelEmail sends a transactional email to the target address.
	// Email channels start disabled and require address verification (a signed
	// confirm token) before they are enabled and delivered to — this prevents
	// Fliq from being used to spam third-party addresses.
	AlertChannelEmail AlertChannelType = "email"
)

// ValidAlertChannelType reports whether t is a supported channel type.
func ValidAlertChannelType(t AlertChannelType) bool {
	switch t {
	case AlertChannelWebhook, AlertChannelSlack, AlertChannelEmail:
		return true
	default:
		return false
	}
}

// RequiresVerification reports whether a channel of this type must verify its
// target before it can be enabled. Only email channels do — webhook/slack
// targets are caller-owned URLs, an email address may belong to a third party.
func (t AlertChannelType) RequiresVerification() bool {
	return t == AlertChannelEmail
}

// AlertChannel is a user-configured destination notified when one of the user's
// jobs or buffer items exhausts its retries (permanent failure).
type AlertChannel struct {
	ID     string
	UserID string
	Type   AlertChannelType
	Target string
	Name   string
	// Enabled gates fan-out. Email channels are created disabled and only flip
	// to enabled once Verified is set via the confirm-token endpoint.
	Enabled bool
	// Verified is true once an email channel's target address has confirmed
	// ownership. Always true for webhook/slack channels (nothing to verify).
	Verified  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// AlertResourceType identifies what kind of resource failed.
type AlertResourceType string

const (
	AlertResourceJob        AlertResourceType = "job"
	AlertResourceBufferItem AlertResourceType = "buffer_item"
)

// CreditLowBurnWindow is the trailing window over which recent burn is summed
// for the low-balance hint.
const CreditLowBurnWindow = 24 * time.Hour

// CrossedLowBalance reports whether a deduction from prev to current crosses the
// low-balance threshold on the downward edge: prev was at or above the
// threshold and current is below it. This is the hysteresis guard — it fires at
// most once per crossing. A balance already below the threshold (prev already
// below) does not re-fire, and a top-up back above followed by another drop will
// fire again, which is the intended behaviour.
func CrossedLowBalance(prev, current, threshold int64) bool {
	if threshold <= 0 {
		return false
	}
	return prev >= threshold && current < threshold
}

// CreditLowEvent is fired once when a user's credit balance crosses below the
// low-balance threshold (downward edge only — hysteresis is enforced by the
// caller so a hovering balance does not spam). It is fanned out through the
// user's enabled channels like a failure alert.
type CreditLowEvent struct {
	UserID string
	// Balance is the credit balance at the moment of the crossing.
	Balance int64
	// Threshold is the floor the balance dropped below.
	Threshold int64
	// RecentBurn is the number of credits spent over the trailing window
	// (see CreditLowBurnWindow), used to hint how soon the user runs dry.
	RecentBurn int64
	// TopUpURL is where the user can add credits.
	TopUpURL string
	CrossedAt time.Time
}

// AlertEvent is the failure that triggers alert delivery. It is assembled by the
// scheduler at the moment a job or buffer item is marked permanently failed and
// passed to the alert notifier for fan-out across the user's enabled channels.
type AlertEvent struct {
	UserID       string
	ResourceType AlertResourceType
	ResourceID   string
	// BufferID is set only for buffer_item events.
	BufferID   string
	URL        string
	Method     string
	LastError  string
	StatusCode *int
	// Attempts is the total number of execution attempts made before giving up.
	Attempts int
	FailedAt time.Time
}
