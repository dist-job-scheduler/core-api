package testutil

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TestHMACKey is the shared HS256 key used by test helpers and middleware under test.
const TestHMACKey = "testutil-hmac-secret-32-chars!!!"

// UserID returns a unique test user ID.
func UserID() string {
	return "user_test_" + uuid.New().String()[:8]
}

// AuthHeader returns a Bearer token header value with a valid HS256 JWT for the given user.
func AuthHeader(t *testing.T, userID string) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	s, err := token.SignedString([]byte(TestHMACKey))
	if err != nil {
		t.Fatalf("sign test JWT: %v", err)
	}
	return "Bearer " + s
}

// SetAuth sets the Authorization header on the request for the given user.
func SetAuth(t *testing.T, req *http.Request, userID string) {
	t.Helper()
	req.Header.Set("Authorization", AuthHeader(t, userID))
}
