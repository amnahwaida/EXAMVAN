package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// LimitBodySize limits the request body size to prevent DoS via large payloads.
func LimitBodySize(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}
