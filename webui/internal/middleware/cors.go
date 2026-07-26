package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS handles Cross-Origin Resource Sharing.
// It parses the comma-separated origins from corsOrigins. If empty, CORS is disabled.
func CORS(corsOrigins string) gin.HandlerFunc {
	var origins []string
	if corsOrigins != "" {
		for _, o := range strings.Split(corsOrigins, ",") {
			trimmed := strings.TrimSpace(o)
			if trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		// Check if the origin matches our allowed list. An exact match is
		// preferred over a "*" wildcard so we can safely send credentials.
		allowed := false
		wildcard := false
		for _, o := range origins {
			if o == origin {
				allowed = true
				wildcard = false
				break
			}
			if o == "*" {
				allowed = true
				wildcard = true
			}
		}

		if allowed {
			c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE, PATCH")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, Client-Id, Request-Id, Request-Timestamp, Signature")
			if wildcard {
				// Reflecting an arbitrary Origin together with
				// Allow-Credentials:true is the credentialed-wildcard flaw, so
				// with "*" we emit a literal wildcard and NO credentials.
				c.Header("Access-Control-Allow-Origin", "*")
			} else {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}

		if c.Request.Method == "OPTIONS" {
			if allowed {
				c.AbortWithStatus(204)
			} else {
				c.AbortWithStatus(403)
			}
			return
		}

		c.Next()
	}
}
