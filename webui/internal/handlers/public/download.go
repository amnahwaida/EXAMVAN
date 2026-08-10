// Package public provides Gin handler functions for EXAMVAN public routes.
package public

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	r2client "github.com/examvan/webui/internal/handlers/r2"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// 1. GET /download — Render download page
// ---------------------------------------------------------------------------

// DownloadPage renders the download page showing app and web versions
// and the APK file size.
func DownloadPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		androidVer := models.DefaultSettings[models.SettingAndroidVersion]
		webappVer := models.DefaultSettings[models.SettingWebappVersion]
		var systemApps []models.SystemApp
		if pool != nil {
			androidVer = models.GetSaasSettingWithDefault(ctx, pool, models.SettingAndroidVersion, androidVer)
			webappVer = models.GetSaasSettingWithDefault(ctx, pool, models.SettingWebappVersion, webappVer)
			apps, err := models.GetAllSystemApps(ctx, pool)
			if err == nil {
				systemApps = apps
			}
		}

		// Official Android APK release: the highest-version system_app entry
		// with platform=android (served from Cloudflare R2). This is the
		// primary card on the page; static/ is not used in production.
		androidApp := models.BestAndroidApp(systemApps)
		if androidApp != nil {
			androidVer = androidApp.Version
		}

		c.HTML(http.StatusOK, "public/download.html", middleware.MergeTemplateData(c, gin.H{
			"android_version": androidVer,
			"webapp_version":  webappVer,
			"system_apps":     systemApps,
			"android_app":     androidApp,
		}))
	}
}

// ---------------------------------------------------------------------------
// 2. GET /download/apk — Serve APK file (from Cloudflare R2)
// ---------------------------------------------------------------------------

// DownloadAPK serves the EXAMVAN APK. It redirects to a signed R2 URL for the
// highest-version Android system_app entry.
// It supports '?flavor=student' (default) and '?flavor=kiosk'.
func DownloadAPK() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		var apps []models.SystemApp
		if pool != nil {
			apps, _ = models.GetAllSystemApps(ctx, pool)
		}
		app := models.BestAndroidApp(apps)
		if app == nil {
			log.Println("DownloadAPK: no Android system_app available")
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"message": "File APK belum tersedia saat ini.",
			})
			return
		}

		// flavor=kiosk prefers an entry whose name mentions kiosk; otherwise
		// both flavors fall back to the best Android entry (currently a single
		// "EXAMVAN" entry serves both).
		flavor := c.DefaultQuery("flavor", "student")
		if flavor == "kiosk" {
			for i := range apps {
				if apps[i].Platform == "android" &&
					strings.Contains(strings.ToLower(apps[i].Name), "kiosk") {
					app = &apps[i]
					break
				}
			}
		}

		r2Val, exists := c.Get("r2")
		r2 := r2client.FromContext(r2Val)
		if !exists || r2 == nil || !r2.Enabled() {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Cloudflare R2 tidak dikonfigurasi."})
			return
		}

		// Generate presigned URL for download valid for 5 minutes.
		url, err := r2.SignedURL(ctx, app.FilePath, 5*time.Minute)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menghasilkan URL unduhan."})
			return
		}

		c.Redirect(http.StatusFound, url)
	}
}

// ---------------------------------------------------------------------------
// 3. GET /download/app/:id — Serve System App from R2
// ---------------------------------------------------------------------------

func DownloadSystemApp() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		idStr := c.Param("id")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "ID tidak valid."})
			return
		}

		if pool == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Database tidak tersedia."})
			return
		}

		app, err := models.GetSystemAppByID(ctx, pool, id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Aplikasi tidak ditemukan."})
			return
		}

		r2Val, exists := c.Get("r2")
		r2 := r2client.FromContext(r2Val)
		if !exists || r2 == nil || !r2.Enabled() {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Cloudflare R2 tidak dikonfigurasi."})
			return
		}

		// Generate presigned URL for download valid for 5 minutes
		url, err := r2.SignedURL(ctx, app.FilePath, 5*time.Minute)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menghasilkan URL unduhan."})
			return
		}

		c.Redirect(http.StatusFound, url)
	}
}
