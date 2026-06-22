package emailverify

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

const secret = "test-secret-32-chars-long-aaaaaa"

// fixedSigner returns a Signer whose clock is pinned to t so expiry behaviour is
// deterministic. White-box (same package) so it can reach the unexported now.
func fixedSigner(t time.Time) *Signer {
	s := NewSigner([]byte(secret))
	s.now = func() time.Time { return t }
	return s
}

func TestSignVerify_RoundTrip(t *testing.T) {
	t.Parallel()

	s := NewSigner([]byte(secret))
	const channelID = "alert-channel-123"

	got, err := s.Verify(s.Sign(channelID))
	if err != nil {
		t.Fatalf("Verify after Sign: unexpected error: %v", err)
	}
	if got != channelID {
		t.Fatalf("Verify returned channel ID %q, want %q", got, channelID)
	}
}

func TestSign_TokenShape(t *testing.T) {
	t.Parallel()

	tok := NewSigner([]byte(secret)).Sign("c1")
	if n := strings.Count(tok, "."); n != 2 {
		t.Fatalf("token %q has %d dots, want 2 (id.exp.mac)", tok, n)
	}
	for i, part := range strings.Split(tok, ".") {
		if part == "" {
			t.Fatalf("token part %d is empty: %q", i, tok)
		}
	}
}

func TestVerify_RejectsTamperedToken(t *testing.T) {
	t.Parallel()

	s := NewSigner([]byte(secret))
	valid := s.Sign("alert-channel-123")
	parts := strings.Split(valid, ".")

	cases := []struct {
		name  string
		token string
	}{
		{"flipped id", b64("other-channel") + "." + parts[1] + "." + parts[2]},
		{"flipped expiry", parts[0] + "." + b64("9999999999") + "." + parts[2]},
		{"truncated mac", parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-2]},
		{"empty string", ""},
		{"too few parts", parts[0] + "." + parts[1]},
		{"too many parts", valid + ".extra"},
		{"non-base64 mac", parts[0] + "." + parts[1] + ".!!!notb64!!!"},
		{"non-base64 expiry", parts[0] + ".!!!." + parts[2]},
		{"non-numeric expiry", parts[0] + "." + b64("not-a-number") + "." + parts[2]},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := s.Verify(tc.token)
			if !errors.Is(err, domain.ErrInvalidVerificationToken) {
				t.Fatalf("Verify(%q) error = %v, want ErrInvalidVerificationToken", tc.token, err)
			}
		})
	}
}

func TestVerify_RejectsForeignSecret(t *testing.T) {
	t.Parallel()

	// A token minted with one secret must not validate under another — otherwise
	// anyone could forge a confirm link.
	minted := NewSigner([]byte(secret)).Sign("alert-channel-123")
	other := NewSigner([]byte("a-completely-different-secret-key"))

	if _, err := other.Verify(minted); !errors.Is(err, domain.ErrInvalidVerificationToken) {
		t.Fatalf("Verify under foreign secret error = %v, want ErrInvalidVerificationToken", err)
	}
}

func TestVerify_Expiry(t *testing.T) {
	t.Parallel()

	issued := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	tok := fixedSigner(issued).Sign("alert-channel-123")

	// Just before the TTL elapses: still valid.
	beforeExpiry := fixedSigner(issued.Add(DefaultTTL - time.Minute))
	if _, err := beforeExpiry.Verify(tok); err != nil {
		t.Fatalf("token within TTL rejected: %v", err)
	}

	// Past the TTL: rejected as invalid.
	afterExpiry := fixedSigner(issued.Add(DefaultTTL + time.Minute))
	if _, err := afterExpiry.Verify(tok); !errors.Is(err, domain.ErrInvalidVerificationToken) {
		t.Fatalf("expired token error = %v, want ErrInvalidVerificationToken", err)
	}
}

func TestSign_RoundTripsAwkwardChannelIDs(t *testing.T) {
	t.Parallel()

	// channelID is base64url-encoded into the payload, so dots, slashes, and
	// other delimiter-adjacent bytes must survive a round trip intact.
	s := NewSigner([]byte(secret))
	for _, id := range []string{"", "id.with.dots", "id/with+slashes", "üñïçødé-id"} {
		got, err := s.Verify(s.Sign(id))
		if err != nil {
			t.Fatalf("Verify(Sign(%q)): %v", id, err)
		}
		if got != id {
			t.Fatalf("round trip of %q gave %q", id, got)
		}
	}
}
