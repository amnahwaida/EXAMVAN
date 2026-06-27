package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ServerTime returns a gin.HandlerFunc for GET /api/time.
// Returns the current server UTC time for client-side time synchronization.
func ServerTime() gin.HandlerFunc {
	return func(c *gin.Context) {
		now := time.Now().UTC()

		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"server_time": now.Format("2006-01-02T15:04:05Z"),
			"timezone":    "UTC",
		})
	}
}
