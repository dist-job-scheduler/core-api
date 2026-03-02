package middleware

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

const errUnauthorized = "Unauthorized"

// Auth validates a Bearer token and sets "userID" in the gin context.
//
// Two token formats are supported:
//   - fliq_sk_<64 hex chars>: API token — hashed and looked up in the DB via tokenRepo.
//   - JWT (eyJ...): validated as RS256 via JWKS endpoint (Clerk) when jwksURL is set,
//     or HS256 with hmacKey for local dev.
func Auth(jwksURL string, hmacKey []byte, tokenRepo repository.APITokenRepository) gin.HandlerFunc {
	var cache *jwk.Cache

	if jwksURL != "" {
		c := jwk.NewCache(context.Background())
		if err := c.Register(jwksURL, jwk.WithMinRefreshInterval(15*time.Minute)); err != nil {
			panic("jwk cache register: " + err.Error())
		}
		cache = c
	}

	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": errUnauthorized})
			return
		}

		rawToken := strings.TrimPrefix(header, "Bearer ")

		// API token path
		if strings.HasPrefix(rawToken, "fliq_sk_") {
			sum := sha256.Sum256([]byte(rawToken))
			hash := fmt.Sprintf("%x", sum)
			tok, err := tokenRepo.FindByTokenHash(c.Request.Context(), hash)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": errUnauthorized})
				return
			}
			go tokenRepo.UpdateLastUsed(context.Background(), tok.ID) //nolint:errcheck
			c.Set("userID", tok.UserID)
			c.Next()
			return
		}

		// JWT path
		var (
			tok jwt.Token
			err error
		)

		if cache != nil {
			keySet, fetchErr := cache.Get(c.Request.Context(), jwksURL)
			if fetchErr != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": errUnauthorized})
				return
			}
			tok, err = jwt.Parse([]byte(rawToken), jwt.WithKeySet(keySet), jwt.WithValidate(true))
		} else {
			tok, err = jwt.Parse([]byte(rawToken), jwt.WithKey(jwa.HS256, hmacKey), jwt.WithValidate(true))
		}

		if err != nil || tok == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": errUnauthorized})
			return
		}

		userID := tok.Subject()
		if userID == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": errUnauthorized})
			return
		}

		c.Set("userID", userID)
		c.Next()
	}
}
