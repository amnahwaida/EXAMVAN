// Package public provides Gin handler functions for EXAMVAN public routes.
package public

import (
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/config"
	r2client "github.com/examvan/webui/internal/handlers/r2"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// getAPKPath returns the filesystem path to EXAMVAN.apk.
// It first checks the static/ directory relative to the application base
// (derived from StoragePath), then falls back to the working directory's
// static/ folder.
func getAPKPath(c *gin.Context) string {
	return findAPKPath(c, "EXAMVAN.apk")
}

func findAPKPath(c *gin.Context, filename string) string {
	// Derive base directory from the configured storage path, e.g.
	// StoragePath="/app/storage" → base="/app", APK at "/app/static/<file>".
	if cfg, exists := c.Get("cfg"); exists {
		baseDir := filepath.Dir(cfg.(*config.Config).StoragePath)
		cfgPath := filepath.Join(baseDir, "static", filename)
		if _, err := os.Stat(cfgPath); err == nil {
			return cfgPath
		}
	}

	// Fallback: relative to working directory.
	wd, err := os.Getwd()
	if err == nil {
		wdPath := filepath.Join(wd, "static", filename)
		if _, err := os.Stat(wdPath); err == nil {
			return wdPath
		}
	}

	// Last resort: bare relative path.
	return filepath.Join("static", filename)
}

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
		if pool != nil {
			androidVer = models.GetSaasSettingWithDefault(ctx, pool, models.SettingAndroidVersion, androidVer)
			webappVer = models.GetSaasSettingWithDefault(ctx, pool, models.SettingWebappVersion, webappVer)
		}

		studentAPKPath := findAPKPath(c, "EXAMVAN-student.apk")
		kioskAPKPath := findAPKPath(c, "EXAMVAN-kiosk.apk")
		legacyAPKPath := getAPKPath(c)
		activeAPKPath := ""
		if _, err := os.Stat(studentAPKPath); err == nil {
			activeAPKPath = studentAPKPath
		} else if _, err := os.Stat(kioskAPKPath); err == nil {
			activeAPKPath = kioskAPKPath
		} else if _, err := os.Stat(legacyAPKPath); err == nil {
			activeAPKPath = legacyAPKPath
		}

		fileSizeMB := float64(0)
		if activeAPKPath != "" {
			if info, err := os.Stat(activeAPKPath); err == nil {
				fileSizeMB = math.Round(float64(info.Size())/(1024*1024)*100) / 100
			}
		}

		var systemApps []models.SystemApp
		if pool != nil {
			apps, err := models.GetAllSystemApps(ctx, pool)
			if err == nil {
				systemApps = apps
			}
		}

		c.HTML(http.StatusOK, "public/download.html", middleware.MergeTemplateData(c, gin.H{
			"android_version":       androidVer,
			"webapp_version":        webappVer,
			"file_size_mb":          fileSizeMB,
			"student_apk_available": studentAPKPath != "" && studentAPKPath != "static/EXAMVAN-student.apk",
			"kiosk_apk_available":   kioskAPKPath != "" && kioskAPKPath != "static/EXAMVAN-kiosk.apk",
			"apk_available":         activeAPKPath != "",
			"download_notice":       "Unduhan APK belum tersedia di environment ini.",
			"system_apps":           systemApps,
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
		filename := "EXAMVAN-student.apk"
		if flavor == "kiosk" {
			filename = "EXAMVAN-kiosk.apk"
		}

		apkPath := findAPKPath(c, filename)
		if _, err := os.Stat(apkPath); os.IsNotExist(err) {
			log.Printf("APK file not found at %s", apkPath)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"message": "File APK belum tersedia saat ini.",
			})
			return
		}

		c.FileAttachment(apkPath, filename)
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

		app, err := models.GetSystemAppByID(ctx, pool, id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "Aplikasi tidak ditemukan."})
			return
		}

		r2Val, exists := c.Get("r2")
		if !exists || r2Val.(*r2client.Client) == nil || !r2Val.(*r2client.Client).Enabled() {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Cloudflare R2 tidak dikonfigurasi."})
			return
		}
		r2 := r2Val.(*r2client.Client)

		// Generate presigned URL for download valid for 5 minutes
		url, err := r2.SignedURL(ctx, app.FilePath, 5*time.Minute)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menghasilkan URL unduhan."})
			return
		}

		c.Redirect(http.StatusFound, url)
	}
}
