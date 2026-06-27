package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// DefaultRequestTimeout is the timeout applied to all API requests.
const DefaultRequestTimeout = 30 * time.Second

// TimeoutMiddleware wraps the request context with a deadline.
// WebSocket connections are excluded.
func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}
	return func(c *gin.Context) {
		// Skip for WebSocket upgrades and health checks
		if c.GetHeader("Upgrade") == "websocket" ||
			c.Request.URL.Path == "/api/health" ||
			c.Request.URL.Path == "/healthz" {
			c.Next()
			return
		}

		// Wrap context with timeout — downstream handlers that use
		// c.Request.Context() will automatically respect this deadline.
		ctx, cancel := context.WithTimeout(c.Request.Context(), timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
