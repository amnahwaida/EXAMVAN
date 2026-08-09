// Package middleware provides reusable Gin HTTP middleware for EXAMVAN webui.
//
// This file implements Android version checking for API routes that are
// consumed by the EXAMVAN Android app. The middleware compares the
// X-App-Version header against the configured android_version in SaaS settings
// and returns HTTP 426 Upgrade Required when the client is outdated.
package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

const (
	// HeaderAppVersion is the HTTP header key for the Android app version.
	HeaderAppVersion = "X-App-Version"
)

// AndroidVersionCheck returns middleware that validates the X-App-Version
// header by querying the android_version saas_setting. Routes that pass this
// check have their version info stored in context.
//
// Usage:
//
//	r.GET("/api/exams", middleware.AndroidVersionCheck(), api.ListExams())
//
// To skip version checking for specific routes, register them before this
// middleware group.
func AndroidVersionCheck() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool, exists := c.Get("db")
		if !exists || pool == nil {
			// No DB available — skip version check (allow request).
			c.Next()
			return
		}

		dbPool := pool.(*pgxpool.Pool)
		ctx := c.Request.Context()

		// No version header — skip check (allow request for web clients). Read
		// the header BEFORE resolving the required version so the (two) DB
		// queries behind it only run for actual Android clients, not for every
		// request that passes through this middleware.
		clientVer := strings.TrimSpace(c.GetHeader(HeaderAppVersion))
		if clientVer == "" {
			c.Next()
			return
		}

		requiredVer := getRequiredVersion(ctx, dbPool)
		if requiredVer == "" {
			// Nothing is available to download — skip enforcement (see
			// models.EffectiveAndroidRequiredVersion).
			c.Next()
			return
		}

		// Store version info in context for downstream handlers.
		c.Set("client_version", clientVer)
		c.Set("required_version", requiredVer)
		c.Set("version_checked", true)

		// Only enforce version check for Android client (when X-App-Version is present).
		if !isVersionCompatible(requiredVer, clientVer) {
			// Use the same format as the Python server.
			message := fmt.Sprintf(
				"Versi aplikasi Anda (%s) sudah tidak didukung. Silakan download versi terbaru (%s) dari halaman Download.",
				clientVer, requiredVer,
			)
			c.AbortWithStatusJSON(http.StatusUpgradeRequired, gin.H{
				"success": false,
				"message": message,
			})
			return
		}

		c.Next()
	}
}

// getRequiredVersion retrieves the effective required Android version from the
// publishable releases (system_apps/R2), clamped to the configured
// android_version saas_setting. Returns "" when nothing is available to
// download — AndroidVersionCheck then lets the request through, because
// blocking an outdated client with no newer APK to download would deadlock it
// (426 on every route, yet /download/apk has nothing to offer).
func getRequiredVersion(ctx context.Context, pool *pgxpool.Pool) string {
	return models.EffectiveAndroidRequiredVersion(ctx, pool)
}

// isVersionCompatible compares two version strings using simple semantic
// versioning (major.minor.patch). A client version is compatible when it
// is greater than or equal to the required version. Missing trailing parts
// are treated as 0, so a client on "2.4" is compatible with a required
// "2.4.0" — matching the Android client's UpdateManager (getOrElse { 0 }).
//
// Returns true when the client version is compatible.
func isVersionCompatible(required, client string) bool {
	reqParts := parseVersion(required)
	cliParts := parseVersion(client)

	for i := 0; i < len(reqParts) || i < len(cliParts); i++ {
		var reqPart, cliPart int
		if i < len(reqParts) {
			reqPart = reqParts[i]
		}
		if i < len(cliParts) {
			cliPart = cliParts[i]
		}
		if cliPart > reqPart {
			return true
		}
		if cliPart < reqPart {
			return false
		}
	}

	// All parts equal (to the widest version) → compatible.
	return true
}

// parseVersion splits a version string into integer parts.
// e.g. "2.2.0" → []int{2, 2, 0}; a segment like "x" contributes 0 (so
// "2.x.0" == "2.0.0"), mirroring models.CompareVersions and the Android
// client's UpdateManager parser.
func parseVersion(v string) []int {
	parts := strings.Split(v, ".")
	result := make([]int, 0, len(parts))
	for _, p := range parts {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil {
			n = 0
		}
		result = append(result, n)
	}
	return result
}
