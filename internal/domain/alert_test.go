package domain_test

import (
	"testing"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

func TestCrossedLowBalance(t *testing.T) {
	t.Parallel()

	const threshold = 100

	cases := []struct {
		name          string
		prev, current int64
		threshold     int64
		want          bool
	}{
		{"downward crossing fires", 150, 50, threshold, true},
		{"exact boundary down fires", threshold, threshold - 1, threshold, true},
		{"lands exactly on threshold does not fire", 150, threshold, threshold, false},
		{"already below does not re-fire", 50, 10, threshold, false},
		{"stays above does not fire", 200, 150, threshold, false},
		{"upward movement does not fire", 50, 150, threshold, false},
		{"no change above", 200, 200, threshold, false},
		{"no change below", 50, 50, threshold, false},
		{"crossing to zero fires", 150, 0, threshold, true},
		{"zero threshold disabled", 150, 0, 0, false},
		{"negative threshold disabled", 150, 50, -1, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := domain.CrossedLowBalance(tc.prev, tc.current, tc.threshold)
			if got != tc.want {
				t.Fatalf("CrossedLowBalance(prev=%d, current=%d, threshold=%d) = %v, want %v",
					tc.prev, tc.current, tc.threshold, got, tc.want)
			}
		})
	}
}

// TestCrossedLowBalance_Hysteresis walks a balance through drop → hover → top-up
// → drop to assert the latch fires once per downward edge, not while hovering.
func TestCrossedLowBalance_Hysteresis(t *testing.T) {
	t.Parallel()

	const threshold = 100
	balances := []int64{200, 120, 90, 80, 95, 60, 150, 70}
	// edges marks the steps where a fire is expected (downward crossings).
	wantFire := map[int]bool{2: true, 7: true}

	for i := 1; i < len(balances); i++ {
		got := domain.CrossedLowBalance(balances[i-1], balances[i], threshold)
		if got != wantFire[i] {
			t.Fatalf("step %d (%d→%d): fired=%v, want %v",
				i, balances[i-1], balances[i], got, wantFire[i])
		}
	}
}

func TestValidAlertChannelType(t *testing.T) {
	t.Parallel()

	valid := []domain.AlertChannelType{
		domain.AlertChannelWebhook,
		domain.AlertChannelSlack,
		domain.AlertChannelEmail,
	}
	for _, ty := range valid {
		if !domain.ValidAlertChannelType(ty) {
			t.Errorf("ValidAlertChannelType(%q) = false, want true", ty)
		}
	}

	invalid := []domain.AlertChannelType{"", "sms", "Email", "WEBHOOK", "telegram"}
	for _, ty := range invalid {
		if domain.ValidAlertChannelType(ty) {
			t.Errorf("ValidAlertChannelType(%q) = true, want false", ty)
		}
	}
}

func TestRequiresVerification(t *testing.T) {
	t.Parallel()

	if !domain.AlertChannelEmail.RequiresVerification() {
		t.Error("email channel should require verification")
	}
	for _, ty := range []domain.AlertChannelType{domain.AlertChannelWebhook, domain.AlertChannelSlack} {
		if ty.RequiresVerification() {
			t.Errorf("%q should not require verification", ty)
		}
	}
}
