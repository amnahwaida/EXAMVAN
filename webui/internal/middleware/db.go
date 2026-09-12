package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ---------------------------------------------------------------------------
// Database middleware
// ---------------------------------------------------------------------------

// DatabaseMiddleware injects a *pgxpool.Pool into the Gin context under the
// "db" key for downstream defensive checks (RequireDB).
//
// Usage:
//
//	pool := database.Connect(cfg)
//	r.Use(middleware.DatabaseMiddleware(pool))
func DatabaseMiddleware(pool *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("db", pool)
		c.Next()
	}
}

// RequireDB is a defensive middleware that aborts with a 500 JSON response
// when no *pgxpool.Pool is available in the context, rather than panicking.
// Use it as a safety net on routes that absolutely require a database
// connection, especially during startup or error-recovery scenarios.
func RequireDB() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool, exists := c.Get("db")
		if !exists || pool == nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Database tidak tersedia.",
			})
			return
		}
		if _, ok := pool.(*pgxpool.Pool); !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "Koneksi database tidak valid.",
			})
			return
		}
		c.Next()
	}
}
