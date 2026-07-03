// Package public provides Gin handler functions for EXAMVAN public routes.
package public

import (
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// getAPKPath returns the filesystem path to EXAMVAN.apk.
// It first checks the static/ directory relative to the application base
// (derived from StoragePath), then falls back to the working directory's
// static/ folder.
func getAPKPath(c *gin.Context) string {
	// Derive base directory from the configured storage path, e.g.
	// StoragePath="/app/storage" → base="/app", APK at "/app/static/EXAMVAN.apk".
	if cfg, exists := c.Get("cfg"); exists {
		baseDir := filepath.Dir(cfg.(*config.Config).StoragePath)
		cfgPath := filepath.Join(baseDir, "static", "EXAMVAN.apk")
		if _, err := os.Stat(cfgPath); err == nil {
			return cfgPath
		}
	}

	// Fallback: relative to working directory.
	wd, err := os.Getwd()
	if err == nil {
		wdPath := filepath.Join(wd, "static", "EXAMVAN.apk")
		if _, err := os.Stat(wdPath); err == nil {
			return wdPath
		}
	}

	// Last resort: bare relative path.
	return "static/EXAMVAN.apk"
}

// ---------------------------------------------------------------------------
// 1. GET /download — Render download page
// ---------------------------------------------------------------------------

// DownloadPage renders the download page showing app and web versions
// and the APK file size.
func DownloadPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		if pool == nil {
			c.HTML(http.StatusInternalServerError, "public/download.html", middleware.MergeTemplateData(c, gin.H{
				"error": "Database tidak tersedia.",
			}))
			return
		}
		ctx := c.Request.Context()

		androidVer := models.GetSaasSettingWithDefault(ctx, pool,
			models.SettingAndroidVersion, "2.1.0")
		webappVer := models.GetSaasSettingWithDefault(ctx, pool,
			models.SettingWebappVersion, "2.1.0")

		apkPath := getAPKPath(c)
		fileSizeMB := float64(0)
		if info, err := os.Stat(apkPath); err == nil {
			fileSizeMB = math.Round(float64(info.Size())/(1024*1024)*100) / 100
		}

		c.HTML(http.StatusOK, "public/download.html", middleware.MergeTemplateData(c, gin.H{
			"android_version": androidVer,
			"webapp_version":  webappVer,
			"file_size_mb":    fileSizeMB,
		}))
	}
}

// ---------------------------------------------------------------------------
// 2. GET /download/apk — Serve APK file
// ---------------------------------------------------------------------------

// DownloadAPK serves the EXAMVAN.apk file as a downloadable attachment.
// It supports '?flavor=student' (default) and '?flavor=kiosk'.
func DownloadAPK() gin.HandlerFunc {
	return func(c *gin.Context) {
		flavor := c.DefaultQuery("flavor", "student")
		var filename string
		if flavor == "kiosk" {
			filename = "EXAMVAN-kiosk.apk"
		} else {
			filename = "EXAMVAN-student.apk"
		}

		var apkPath string
		// First check configuration directory
		if cfg, exists := c.Get("cfg"); exists {
			baseDir := filepath.Dir(cfg.(*config.Config).StoragePath)
			cfgPath := filepath.Join(baseDir, "static", filename)
			if _, err := os.Stat(cfgPath); err == nil {
				apkPath = cfgPath
			}
		}

		// Fallback relative to working directory
		if apkPath == "" {
			wd, err := os.Getwd()
			if err == nil {
				wdPath := filepath.Join(wd, "static", filename)
				if _, err := os.Stat(wdPath); err == nil {
					apkPath = wdPath
				}
			}
		}

		// Last fallback: use the legacy EXAMVAN.apk path
		if apkPath == "" {
			apkPath = getAPKPath(c)
			filename = "EXAMVAN.apk"
		}

		if _, err := os.Stat(apkPath); os.IsNotExist(err) {
			log.Printf("APK file not found at %s", apkPath)
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "File APK tidak ditemukan.",
			})
			return
		}

		c.FileAttachment(apkPath, filename)
	}
}
