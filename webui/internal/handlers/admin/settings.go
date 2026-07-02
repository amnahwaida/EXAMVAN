package admin

import (
	"context"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// SaaS Settings handler (GET + POST)
// ---------------------------------------------------------------------------

// SaasSettings handles both GET and POST for SaaS configuration.
// It is designed to be registered as:
//
//	router.GET("/admin/api/saas-settings", admin.SaasSettings())
//	router.POST("/admin/api/saas-settings", admin.SaasSettings())
//
// The handler inspects the request method internally.
func SaasSettings() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		switch c.Request.Method {
		case http.MethodGet:
			handleSaasSettingsGet(c, pool, ctx)
		case http.MethodPost:
			handleSaasSettingsPost(c, pool, ctx)
		default:
			errorResponse(c, http.StatusMethodNotAllowed, "Method tidak diizinkan")
		}
	}
}

func handleSaasSettingsGet(c *gin.Context, pool *pgxpool.Pool, ctx context.Context) {
	settings, err := models.GetAllSaasSettings(ctx, pool)
	if err != nil {
		log.Printf("get saas settings error: %v", err)
		errorResponse(c, http.StatusInternalServerError, "Gagal memuat pengaturan")
		return
	}

	// Format settings for response (matching Python's format)
	emailEnabled := settings[models.SettingEmailVerificationEnabled] == "1"
	smtpPassword := maskTokenSetting(settings[models.SettingSMTPPassword])

	defaultMaxExams := parseIntSetting(settings[models.SettingDefaultMaxExams], 3)
	defaultMaxPDFSize := parseIntSetting(settings[models.SettingDefaultMaxPDFSize], 1048576)
	defaultMaxDrafts := parseIntSetting(settings[models.SettingDefaultMaxDrafts], 2)
	defaultMaxDraftSize := parseIntSetting(settings[models.SettingDefaultMaxDraftSize], 1048576)
	defaultActiveDays := parseIntSetting(settings[models.SettingDefaultActiveDays], 1)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"settings": gin.H{
			"email_verification_enabled": emailEnabled,
			"smtp_host":                  settings[models.SettingSMTPHost],
			"smtp_port":                  settings[models.SettingSMTPPort],
			"smtp_user":                  settings[models.SettingSMTPUser],
			"smtp_password":              smtpPassword,
			"smtp_sender_name":           settings[models.SettingSMTPSenderName],
			"default_max_exams":          defaultMaxExams,
			"default_max_pdf_size_mb":    roundTo(float64(defaultMaxPDFSize)/(1024*1024), 2),
			"default_max_drafts":         defaultMaxDrafts,
			"default_max_draft_size_mb":  roundTo(float64(defaultMaxDraftSize)/(1024*1024), 2),
			"default_active_days":        defaultActiveDays,
			"android_version":            settings[models.SettingAndroidVersion],
			"webapp_version":             settings[models.SettingWebappVersion],
			"certificate_fingerprint":    settings[models.SettingCertificateFingerprint],
		},
	})
}

func maskTokenSetting(token string) string {
	if len(token) <= 8 {
		return token
	}
	return strings.Repeat("*", len(token)-4) + token[len(token)-4:]
}

func parseIntSetting(val string, defaultVal int) int {
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return i
}

func handleSaasSettingsPost(c *gin.Context, pool *pgxpool.Pool, ctx context.Context) {
	var body struct {
		EmailVerificationEnabled bool    `json:"email_verification_enabled"`
		SMTPHost                 string  `json:"smtp_host"`
		SMTPPort                 string  `json:"smtp_port"`
		SMTPUser                 string  `json:"smtp_user"`
		SMTPPassword             string  `json:"smtp_password"`
		SMTPSenderName           string  `json:"smtp_sender_name"`
		DefaultMaxExams          int     `json:"default_max_exams"`
		DefaultMaxPDFSizeMB      float64 `json:"default_max_pdf_size_mb"`
		DefaultMaxDrafts         int     `json:"default_max_drafts"`
		DefaultMaxDraftSizeMB    float64 `json:"default_max_draft_size_mb"`
		DefaultActiveDays        int     `json:"default_active_days"`
		AndroidVersion           string  `json:"android_version"`
		WebappVersion            string  `json:"webapp_version"`
		CertificateFingerprint   string  `json:"certificate_fingerprint"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		errorResponse(c, http.StatusBadRequest, "Data tidak valid")
		return
	}

	reqCtx := c.Request.Context()

	// Email settings
	emailEnabled := "0"
	if body.EmailVerificationEnabled {
		emailEnabled = "1"
	}
	if err := models.SetSaasSetting(reqCtx, pool, models.SettingEmailVerificationEnabled, emailEnabled); err != nil {
		log.Printf("save email_enabled error: %v", err)
	}

	models.SetSaasSetting(reqCtx, pool, models.SettingSMTPHost, strings.TrimSpace(body.SMTPHost))
	models.SetSaasSetting(reqCtx, pool, models.SettingSMTPPort, strings.TrimSpace(body.SMTPPort))
	models.SetSaasSetting(reqCtx, pool, models.SettingSMTPUser, strings.TrimSpace(body.SMTPUser))
	models.SetSaasSetting(reqCtx, pool, models.SettingSMTPSenderName, strings.TrimSpace(body.SMTPSenderName))

	// SMTP Password — mask handling
	smtpPassword := strings.TrimSpace(body.SMTPPassword)
	if smtpPassword != "" && strings.HasPrefix(smtpPassword, "****") {
		existing, _ := models.GetSaasSetting(reqCtx, pool, models.SettingSMTPPassword)
		if existing != "" {
			smtpPassword = existing
		}
	}
	if smtpPassword != "" {
		if err := models.SetSaasSetting(reqCtx, pool, models.SettingSMTPPassword, smtpPassword); err != nil {
			log.Printf("save smtp_password error: %v", err)
		}
	}

	// Numerical settings
	defaultMaxExams := body.DefaultMaxExams
	if defaultMaxExams <= 0 {
		defaultMaxExams = 3
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingDefaultMaxExams, strconv.Itoa(defaultMaxExams))

	defaultPDFSize := int(math.Max(0, body.DefaultMaxPDFSizeMB*1024*1024))
	if defaultPDFSize <= 0 {
		defaultPDFSize = 1048576
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingDefaultMaxPDFSize, strconv.Itoa(defaultPDFSize))

	defaultDrafts := body.DefaultMaxDrafts
	if defaultDrafts <= 0 {
		defaultDrafts = 2
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingDefaultMaxDrafts, strconv.Itoa(defaultDrafts))

	defaultDraftSize := int(math.Max(0, body.DefaultMaxDraftSizeMB*1024*1024))
	if defaultDraftSize <= 0 {
		defaultDraftSize = 1048576
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingDefaultMaxDraftSize, strconv.Itoa(defaultDraftSize))

	defaultActiveDays := body.DefaultActiveDays
	if defaultActiveDays <= 0 {
		defaultActiveDays = 1
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingDefaultActiveDays, strconv.Itoa(defaultActiveDays))

	// App versions
	androidVersion := strings.TrimSpace(body.AndroidVersion)
	if androidVersion != "" {
		models.SetSaasSetting(reqCtx, pool, models.SettingAndroidVersion, androidVersion)
	}
	webappVersion := strings.TrimSpace(body.WebappVersion)
	if webappVersion != "" {
		models.SetSaasSetting(reqCtx, pool, models.SettingWebappVersion, webappVersion)
	}

	certFingerprint := strings.TrimSpace(body.CertificateFingerprint)
	models.SetSaasSetting(reqCtx, pool, models.SettingCertificateFingerprint, certFingerprint)

	successMessage(c, "Pengaturan SaaS berhasil diperbarui")
}

// ---------------------------------------------------------------------------
// Change Password
// ---------------------------------------------------------------------------

func ChangePassword() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		var body struct {
			CurrentPassword string `json:"current_password"`
			NewPassword     string `json:"new_password"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		if len(body.NewPassword) < 8 {
			errorResponse(c, http.StatusBadRequest, "Password baru minimal 8 karakter")
			return
		}

		if len(body.NewPassword) > 128 {
			errorResponse(c, http.StatusBadRequest, "Password baru maksimal 128 karakter")
			return
		}

		user, err := models.GetUserByID(ctx, pool, userID)
		if err != nil {
			errorResponse(c, http.StatusInternalServerError, "Terjadi kesalahan. Silakan coba lagi.")
			return
		}

		if !models.CheckPassword(body.CurrentPassword, user.PasswordHash) {
			errorResponse(c, http.StatusBadRequest, "Password saat ini salah")
			return
		}

		if models.CheckPassword(body.NewPassword, user.PasswordHash) {
			errorResponse(c, http.StatusBadRequest, "Password baru harus berbeda dari password saat ini")
			return
		}

		if err := models.UpdateUserField(ctx, pool, userID, "password_hash", body.NewPassword); err != nil {
			log.Printf("change password error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah password")
			return
		}

		successMessage(c, "Password berhasil diperbarui")
	}
}

// TestSMTPConnectionEndpoint tests SMTP connection configuration.
func TestSMTPConnectionEndpoint() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		var body struct {
			SMTPHost     string `json:"smtp_host"`
			SMTPPort     string `json:"smtp_port"`
			SMTPUser     string `json:"smtp_user"`
			SMTPPassword string `json:"smtp_password"`
		}

		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		smtpPassword := strings.TrimSpace(body.SMTPPassword)
		// Mask check: if it is masked or empty, get the existing password from DB
		if smtpPassword == "" || strings.HasPrefix(smtpPassword, "****") {
			existing, _ := models.GetSaasSetting(ctx, pool, models.SettingSMTPPassword)
			if existing != "" {
				smtpPassword = existing
			}
		}

		err := helpers.TestSMTPConnection(
			strings.TrimSpace(body.SMTPHost),
			strings.TrimSpace(body.SMTPPort),
			strings.TrimSpace(body.SMTPUser),
			smtpPassword,
		)
		if err != nil {
			log.Printf("test SMTP error: %v", err)
			errorResponse(c, http.StatusBadRequest, err.Error())
			return
		}

		successMessage(c, "Koneksi SMTP berhasil terhubung!")
	}
}
