// Package emailverify mints and validates the signed confirm tokens that gate
// email alert channels. A token binds an alert-channel ID to an expiry and is
// signed with an HMAC secret (the same JWT_SECRET used for local auth), so the
// verify endpoint can trust a link it handed out without a DB round-trip to
// look the token up.
package emailverify

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/domain"
)

// DefaultTTL is how long a confirm link stays valid.
const DefaultTTL = 48 * time.Hour

// Signer mints and validates confirm tokens with an HMAC-SHA256 secret.
type Signer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewSigner returns a Signer keyed by secret (e.g. the JWT_SECRET bytes).
func NewSigner(secret []byte) *Signer {
	return &Signer{secret: secret, ttl: DefaultTTL, now: time.Now}
}

// Sign returns a confirm token binding channelID to an expiry DefaultTTL from
// now. Format: base64url(channelID) "." base64url(expiryUnix) "." base64url(mac).
func (s *Signer) Sign(channelID string) string {
	exp := s.now().Add(s.ttl).Unix()
	idPart := b64(channelID)
	expPart := b64(strconv.FormatInt(exp, 10))
	payload := idPart + "." + expPart
	return payload + "." + b64Bytes(s.mac(payload))
}

// Verify checks the token's signature and expiry and returns the channel ID it
// was issued for. It returns domain.ErrInvalidVerificationToken for any
// malformed, tampered, or expired token.
func (s *Signer) Verify(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", domain.ErrInvalidVerificationToken
	}
	payload := parts[0] + "." + parts[1]

	gotMAC, err := unb64(parts[2])
	if err != nil {
		return "", domain.ErrInvalidVerificationToken
	}
	if subtle.ConstantTimeCompare(gotMAC, s.mac(payload)) != 1 {
		return "", domain.ErrInvalidVerificationToken
	}

	expBytes, err := unb64(parts[1])
	if err != nil {
		return "", domain.ErrInvalidVerificationToken
	}
	exp, err := strconv.ParseInt(string(expBytes), 10, 64)
	if err != nil {
		return "", domain.ErrInvalidVerificationToken
	}
	if s.now().After(time.Unix(exp, 0)) {
		return "", domain.ErrInvalidVerificationToken
	}

	idBytes, err := unb64(parts[0])
	if err != nil {
		return "", domain.ErrInvalidVerificationToken
	}
	return string(idBytes), nil
}

func (s *Signer) mac(payload string) []byte {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(payload))
	return h.Sum(nil)
}

func b64(s string) string          { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func b64Bytes(b []byte) string     { return base64.RawURLEncoding.EncodeToString(b) }
func unb64(s string) ([]byte, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode token part: %w", err)
	}
	return b, nil
}
