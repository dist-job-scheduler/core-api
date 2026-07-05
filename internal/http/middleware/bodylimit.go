package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// MaxRequestBodyBytes caps the size of any inbound request body. Job/buffer
// bodies and header maps are otherwise unbounded and read fully into memory
// before being persisted, so a client posting a multi-gigabyte body could drive
// the API into memory pressure. 10 MiB is far above any legitimate job payload
// while closing the DoS/DB-bloat vector.
const MaxRequestBodyBytes int64 = 10 << 20 // 10 MiB

// BodyLimit wraps each request body in an http.MaxBytesReader so reads beyond
// the limit fail (surfacing as a 400 from the JSON binder / a 413 on the
// underlying write). Applied globally; the Stripe webhook and all handlers read
// through the same capped reader.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}
