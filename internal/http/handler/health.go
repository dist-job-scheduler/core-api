package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Version is the build version, overridable at link time via
// -ldflags "-X .../handler.Version=<sha>". Defaults to "dev".
var Version = "dev"

// Health is a public, unauthenticated liveness endpoint. It takes no
// dependencies (it does not touch Postgres) so it answers even while the
// scheduler or DB is degraded — it reports that the API process is serving.
// The public uptime widget and external monitors poll this.
//
// GET /health
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"version": Version,
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}
