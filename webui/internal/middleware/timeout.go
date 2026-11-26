package middleware

import (
	"context"
	"path"
	"time"

	"github.com/gin-gonic/gin"
)

// DefaultRequestTimeout is the timeout applied to all API requests.
const DefaultRequestTimeout = 30 * time.Second

// TimeoutMiddleware wraps the request context with a deadline.
// WebSocket connections are excluded.
func TimeoutMiddleware(timeout time.Duration) gin.HandlerFunc {
	return TimeoutMiddlewareWithOverrides(timeout, nil)
}

// TimeoutMiddlewareWithOverrides is TimeoutMiddleware with per-path timeout
// overrides. Keys are matched with path.Match, so a pattern such as
// "/admin/api/exams/*/edit" covers dynamic IDs without listing every URL.
// Large uploads need a longer budget than normal API calls; everything else
// keeps the short default.
func TimeoutMiddlewareWithOverrides(timeout time.Duration, overrides map[string]time.Duration) gin.HandlerFunc {
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

		effective := timeout
		if d := matchTimeoutOverride(overrides, c.Request.URL.Path); d > 0 {
			effective = d
		}

		// Wrap context with timeout — downstream handlers that use
		// c.Request.Context() will automatically respect this deadline.
		ctx, cancel := context.WithTimeout(c.Request.Context(), effective)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// matchTimeoutOverride returns the override duration for requestPath, using
// the most specific matching pattern when several match.
func matchTimeoutOverride(overrides map[string]time.Duration, requestPath string) time.Duration {
	var best time.Duration
	bestLen := -1
	for pattern, d := range overrides {
		if d <= 0 {
			continue
		}
		ok, err := path.Match(pattern, requestPath)
		if err != nil || !ok {
			continue
		}
		if len(pattern) > bestLen {
			best, bestLen = d, len(pattern)
		}
	}
	return best
}
