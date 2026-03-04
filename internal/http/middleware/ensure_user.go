package middleware

import (
	"log/slog"
	"net/http"

	"github.com/ErlanBelekov/dist-job-scheduler/internal/repository"
	"github.com/gin-gonic/gin"
)

// EnsureUser runs after Auth. It upserts the Clerk user ID into the users
// table so that jobs/schedules FK constraints are always satisfied, then
// ensures a credit row exists for the user.
func EnsureUser(repo repository.UserRepository, credits repository.CreditRepository, logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("userID")
		ctx := c.Request.Context()

		if err := repo.Upsert(ctx, userID); err != nil {
			logger.ErrorContext(ctx, "ensure user upsert", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError,
				gin.H{"error": "Internal server error"})
			return
		}

		if err := credits.EnsureExists(ctx, userID); err != nil {
			logger.ErrorContext(ctx, "ensure credit row", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError,
				gin.H{"error": "Internal server error"})
			return
		}

		c.Next()
	}
}
