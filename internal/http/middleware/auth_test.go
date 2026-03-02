package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/http/middleware"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

const testKey = "middleware-test-secret-32-chars!!"

func init() {
	gin.SetMode(gin.TestMode)
}

// newEngine builds a minimal gin engine with the Auth middleware protecting GET /protected.
// The handler writes the userID from context so we can assert it was set.
// tokenRepo is nil because these tests only exercise the JWT path.
func newEngine() *gin.Engine {
	r := gin.New()
	r.GET("/protected", middleware.Auth("", []byte(testKey), nil), func(c *gin.Context) {
		userID, _ := c.Get("userID")
		c.String(http.StatusOK, "%v", userID)
	})
	return r
}

func makeJWT(t *testing.T, key []byte, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	return s
}

func TestAuth_MissingHeader_Returns401(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	newEngine().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuth_NonBearerScheme_Returns401(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	newEngine().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuth_InvalidToken_Returns401(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer not.a.jwt")
	newEngine().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuth_ExpiredToken_Returns401(t *testing.T) {
	tok := makeJWT(t, []byte(testKey), jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(-time.Hour).Unix(),
		"iat": time.Now().Add(-2 * time.Hour).Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	newEngine().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuth_WrongSigningKey_Returns401(t *testing.T) {
	tok := makeJWT(t, []byte("different-key-that-is-32-chars!!"), jwt.MapClaims{
		"sub": "user-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	newEngine().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestAuth_ValidToken_PassesAndSetsUserID(t *testing.T) {
	const userID = "user-abc"
	tok := makeJWT(t, []byte(testKey), jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	newEngine().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != fmt.Sprintf("%v", userID) {
		t.Errorf("body = %q, want %q", got, userID)
	}
}
