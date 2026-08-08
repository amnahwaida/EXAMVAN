// Package api provides Gin handler functions for EXAMVAN public API endpoints.
package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/models"
)

// Health returns a gin.HandlerFunc for GET /api/health.
// Reports server health, version, and the configured certificate fingerprint
// used by the Android app for TLS pinning.
// Now also includes server_time_utc for client time drift compensation.
func Health() gin.HandlerFunc {
	return func(c *gin.Context) {
		ver := config.DefaultVersion
		if cfg, exists := c.Get("cfg"); exists {
			ver = cfg.(*config.Config).Version
		}

		fingerprint := ""
		requiredAppVersion := models.DefaultSettings[models.SettingAndroidVersion]
		if poolVal, exists := c.Get("db"); exists {
			if pool, ok := poolVal.(*pgxpool.Pool); ok && pool != nil {
				ctx := c.Request.Context()
				fingerprint = models.GetSaasSettingWithDefault(ctx, pool,
					models.SettingCertificateFingerprint, "")

				// Required app version: prefer the highest-version Android
				// system_app (the official APK release in R2); fall back to
				// the manually-configured saas_setting android_version.
				requiredAppVersion = models.GetSaasSettingWithDefault(ctx, pool,
					models.SettingAndroidVersion, requiredAppVersion)
				if apps, err := models.GetAllSystemApps(ctx, pool); err == nil {
					if best := models.BestAndroidAppVersion(apps); best != "" {
						requiredAppVersion = best
					}
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  "healthy",
			"version": ver,
			// required_app_version lets the Android app compare its installed
			// version against the server's minimum before hitting protected
			// routes (which return HTTP 426 when outdated).
			"required_app_version":    requiredAppVersion,
			"certificate_fingerprint": fingerprint,
			"server_time_utc":         time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		})
	}
}
