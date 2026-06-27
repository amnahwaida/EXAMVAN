// Package api provides Gin handler functions for EXAMVAN public API endpoints.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/models"
)

// Health returns a gin.HandlerFunc for GET /api/health.
// Reports server health, version, and the configured certificate fingerprint
// used by the Android app for TLS pinning.
func Health() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := c.MustGet("db").(*pgxpool.Pool)
		ctx := c.Request.Context()

		ver := config.DefaultVersion
		if cfg, exists := c.Get("cfg"); exists {
			ver = cfg.(*config.Config).Version
		}

		fingerprint := models.GetSaasSettingWithDefault(ctx, pool,
			models.SettingCertificateFingerprint, "")

		c.JSON(http.StatusOK, gin.H{
			"success":                true,
			"status":                 "healthy",
			"version":                ver,
			"certificate_fingerprint": fingerprint,
		})
	}
}
