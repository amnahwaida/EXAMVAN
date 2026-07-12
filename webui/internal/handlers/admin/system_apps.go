package admin

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
	r2client "github.com/examvan/webui/internal/handlers/r2"
)

// SystemAppsPage renders the page for superadmin to upload and manage system apps.
func SystemAppsPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		data := middleware.TemplateData(c)
		pool := getPool(c)
		ctx := c.Request.Context()

		apps, err := models.GetAllSystemApps(ctx, pool)
		if err != nil {
			data["error"] = "Gagal memuat aplikasi sistem."
		} else {
			data["apps"] = apps
		}
		
		r2Val, exists := c.Get("r2")
		r2Enabled := exists && r2Val.(*r2client.Client) != nil && r2Val.(*r2client.Client).Enabled()
		data["r2_enabled"] = r2Enabled

		renderAdminPage(c, "admin/system_apps.html", data)
	}
}

// UploadSystemApp handles uploading an app file to R2 and storing metadata in DB.
func UploadSystemApp() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		r2Val, exists := c.Get("r2")
		if !exists || r2Val.(*r2client.Client) == nil || !r2Val.(*r2client.Client).Enabled() {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Cloudflare R2 tidak dikonfigurasi."})
			return
		}
		r2 := r2Val.(*r2client.Client)

		appName := c.PostForm("name")
		platform := c.PostForm("platform")
		version := c.PostForm("version")

		if appName == "" || platform == "" || version == "" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Semua field harus diisi."})
			return
		}

		// Check for duplicate app name + platform + version
		existsApp, err := models.CheckSystemAppExists(ctx, pool, appName, platform, version)
		if err == nil && existsApp {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Aplikasi dengan nama, platform, dan versi ini sudah ada."})
			return
		}

		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "File tidak ditemukan."})
			return
		}

		// Read file
		f, err := file.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal membaca file."})
			return
		}
		defer f.Close()

		// Validate magic bytes
		magicBuf := make([]byte, 4)
		if n, _ := f.Read(magicBuf); n >= 2 {
			valid := true
			switch platform {
			case "android":
				// ZIP/APK: PK\x03\x04
				if n < 4 || magicBuf[0] != 0x50 || magicBuf[1] != 0x4B || magicBuf[2] != 0x03 || magicBuf[3] != 0x04 {
					valid = false
				}
			case "windows":
				// PE/EXE: MZ
				if magicBuf[0] != 0x4D || magicBuf[1] != 0x5A {
					valid = false
				}
			case "linux":
				// ELF: \x7fELF
				if n < 4 || magicBuf[0] != 0x7F || magicBuf[1] != 0x45 || magicBuf[2] != 0x4C || magicBuf[3] != 0x46 {
					valid = false
				}
			}
			if !valid {
				c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Format file tidak valid untuk platform yang dipilih."})
				return
			}
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "File terlalu kecil atau kosong."})
			return
		}
		
		// Reset file pointer back to start
		f.Seek(0, 0)

		// Generate R2 Key (using UnixNano to avoid collisions)
		r2Key := fmt.Sprintf("apps/%s/%s/%s-%d", platform, version, file.Filename, time.Now().UnixNano())
		
		var contentType string
		switch platform {
		case "android":
			contentType = "application/vnd.android.package-archive"
		case "windows":
			contentType = "application/x-msdownload"
		case "linux":
			contentType = "application/x-executable"
		default:
			contentType = "application/octet-stream"
		}

		// Upload to R2
		if err := r2.UploadWithContentType(ctx, r2Key, f, contentType); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal mengunggah file ke R2."})
			return
		}

		// Save to DB
		app := &models.SystemApp{
			Name:      appName,
			Platform:  platform,
			Version:   version,
			FilePath:  r2Key,
			SizeBytes: file.Size,
		}
		
		if err := models.CreateSystemApp(ctx, pool, app); err != nil {
			// Try to cleanup R2
			_ = r2.Delete(ctx, r2Key)
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menyimpan metadata aplikasi."})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "message": "Aplikasi berhasil diunggah."})
	}
}

// DeleteSystemApp handles deleting a system app.
func DeleteSystemApp() gin.HandlerFunc {
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
		if exists && r2Val.(*r2client.Client) != nil && r2Val.(*r2client.Client).Enabled() {
			r2 := r2Val.(*r2client.Client)
			_ = r2.Delete(ctx, app.FilePath)
		}

		if err := models.DeleteSystemApp(ctx, pool, id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "Gagal menghapus aplikasi dari database."})
			return
		}

		c.JSON(http.StatusOK, gin.H{"success": true, "message": "Aplikasi berhasil dihapus."})
	}
}
