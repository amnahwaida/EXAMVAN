package admin

import (
	"context"
	"fmt"
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
	defaultMaxStorageSize := parseIntSetting(settings[models.SettingDefaultMaxStorageSize], 52428800)
	defaultMaxConcurrentExams := parseIntSetting(settings[models.SettingDefaultMaxConcurrentExams], 2)
	defaultActiveDays := parseIntSetting(settings[models.SettingDefaultActiveDays], 14)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"settings": gin.H{
			"email_verification_enabled":  emailEnabled,
			"email_domain_whitelist":      settings[models.SettingEmailDomainWhitelist],
			"smtp_host":                   settings[models.SettingSMTPHost],
			"smtp_port":                   settings[models.SettingSMTPPort],
			"smtp_user":                   settings[models.SettingSMTPUser],
			"smtp_password":               smtpPassword,
			"smtp_sender_name":            settings[models.SettingSMTPSenderName],
			"default_max_exams":           defaultMaxExams,
			"default_max_pdf_size_mb":     roundTo(float64(defaultMaxPDFSize)/(1024*1024), 2),
			"default_max_storage_size_mb": roundTo(float64(defaultMaxStorageSize)/(1024*1024), 2),
			// Free space on the storage partition, so the Kelola User panel can
			// cap the Maks Storage input at what the server disk can actually
			// hold (0 = tidak dapat ditentukan, e.g. path tidak tersedia).
			"storage_free_mb":              roundTo(getFreeDiskSpace(getStoragePath(c))/(1024*1024), 2),
			"default_max_concurrent_exams": defaultMaxConcurrentExams,
			"default_active_days":          defaultActiveDays,
			"android_version":              settings[models.SettingAndroidVersion],
			"webapp_version":               settings[models.SettingWebappVersion],
			"certificate_fingerprint":      settings[models.SettingCertificateFingerprint],
			"seo_title":                    settings[models.SettingSEOTitle],
			"seo_description":              settings[models.SettingSEODescription],
			"seo_keywords":                 settings[models.SettingSEOKeywords],
			"seo_index":                    settings[models.SettingSEOIndex] == "1",

			// Footer teks yang tampil di semua halaman publik.
			"footer_text":    settings[models.SettingFooterText],
			"footer_tagline": settings[models.SettingFooterTagline],

			// Voucher redemption toggle (default enabled when unset).
			"voucher_redeem_enabled": settings[models.SettingVoucherRedeemEnabled] != "0",

			// Cloudflare Turnstile bot protection on registration (default off).
			// The secret key is masked on read, like the SMTP password.
			"turnstile_enabled":    settings[models.SettingTurnstileEnabled] == "1",
			"turnstile_site_key":   settings[models.SettingTurnstileSiteKey],
			"turnstile_secret_key": maskTokenSetting(settings[models.SettingTurnstileSecretKey]),

			// Per-IP registration cap (0 = unlimited), defense-in-depth on top
			// of Turnstile against mass-registration.
			"max_accounts_per_ip": parseIntSetting(settings[models.SettingMaxAccountsPerIP], 3),

			// Per-exam approved-device cap for server-side auto-approve
			// (0 = unlimited): when reached, further request-approval calls fall
			// back to the pending queue instead of auto-approving.
			"max_approvals_per_exam": parseIntSetting(settings[models.SettingMaxApprovalsPerExam], 500),

			// Approval-cleanup job tuning (see StartApprovalCleanupJob): purge
			// cadence in minutes, grace hours after an exam ends, and the TTL
			// hours for rows on inactive exams.
			"approval_cleanup_interval_minutes":   parseIntSetting(settings[models.SettingApprovalCleanupIntervalMinutes], 15),
			"approval_cleanup_ended_grace_hours":  parseIntSetting(settings[models.SettingApprovalCleanupEndedGraceHours], 1),
			"approval_cleanup_inactive_ttl_hours": parseIntSetting(settings[models.SettingApprovalCleanupInactiveTTLHours], 24),
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

// boolFlag maps a boolean to the "1"/"0" string used for SaaS toggle settings.
func boolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func handleSaasSettingsPost(c *gin.Context, pool *pgxpool.Pool, ctx context.Context) {
	var body struct {
		EmailVerificationEnabled  bool     `json:"email_verification_enabled"`
		EmailDomainWhitelist      string   `json:"email_domain_whitelist"`
		SMTPHost                  string   `json:"smtp_host"`
		SMTPPort                  string   `json:"smtp_port"`
		SMTPUser                  string   `json:"smtp_user"`
		SMTPPassword              string   `json:"smtp_password"`
		SMTPSenderName            string   `json:"smtp_sender_name"`
		DefaultMaxExams           int      `json:"default_max_exams"`
		DefaultMaxPDFSizeMB       *float64 `json:"default_max_pdf_size_mb"`
		DefaultMaxStorageSizeMB   *float64 `json:"default_max_storage_size_mb"`
		DefaultMaxConcurrentExams int      `json:"default_max_concurrent_exams"`
		DefaultActiveDays         int      `json:"default_active_days"`
		AndroidVersion            string   `json:"android_version"`
		WebappVersion             string   `json:"webapp_version"`
		CertificateFingerprint    string   `json:"certificate_fingerprint"`
		SEOTitle                  string   `json:"seo_title"`
		SEODescription            string   `json:"seo_description"`
		SEOKeywords               string   `json:"seo_keywords"`
		SEOIndex                  bool     `json:"seo_index"`
		FooterText                string   `json:"footer_text"`
		FooterTagline             string   `json:"footer_tagline"`
		VoucherRedeemEnabled      bool     `json:"voucher_redeem_enabled"`
		TurnstileEnabled          bool     `json:"turnstile_enabled"`
		TurnstileSiteKey          string   `json:"turnstile_site_key"`
		TurnstileSecretKey        string   `json:"turnstile_secret_key"`
		MaxAccountsPerIP          int      `json:"max_accounts_per_ip"`
		// Pointer: 0 is a MEANINGFUL value (unlimited), so an absent field — an
		// older cached UI saving without this control — must NOT silently reset
		// a configured cap to unlimited. Same guard as the storage quotas.
		MaxApprovalsPerExam       *int     `json:"max_approvals_per_exam"`

		// Approval-cleanup job tuning. All pointers for the same reason as
		// MaxApprovalsPerExam: an older cached UI that predates these controls
		// must not silently reset tuned purge cadence/windows to defaults.
		ApprovalCleanupIntervalMinutes  *int `json:"approval_cleanup_interval_minutes"`
		ApprovalCleanupEndedGraceHours  *int `json:"approval_cleanup_ended_grace_hours"`
		ApprovalCleanupInactiveTTLHours *int `json:"approval_cleanup_inactive_ttl_hours"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		errorResponse(c, http.StatusBadRequest, "Data tidak valid")
		return
	}

	// Default storage quota must be validated BEFORE any setting is written:
	// a rejected value must not leave earlier writes half-saved (same rule as
	// the Turnstile check below). 0 = tidak terbatas (no cap enforcement, so it
	// is always accepted); only a positive value is bounded by the server disk.
	if body.DefaultMaxStorageSizeMB != nil && *body.DefaultMaxStorageSizeMB < 0 {
		errorResponse(c, http.StatusBadRequest, "Maks Storage tidak boleh bernilai negatif.")
		return
	}
	if body.DefaultMaxStorageSizeMB != nil && *body.DefaultMaxStorageSizeMB > 0 {
		freeBytes := getFreeDiskSpace(getStoragePath(c))
		if freeBytes > 0 && *body.DefaultMaxStorageSizeMB*1024*1024 > freeBytes {
			freeGB := freeBytes / (1024 * 1024 * 1024)
			errorResponse(c, http.StatusBadRequest,
				fmt.Sprintf("Maks Storage (%.2f MB) melebihi sisa kapasitas disk server (%.2f GB).", *body.DefaultMaxStorageSizeMB, freeGB))
			return
		}
	}

	// Default PDF upload size follows the same disk cap as the other storage
	// editors (validatePDFQuota): negative rejected, and a positive value may
	// not exceed the free disk. The UI caps the input at min(free disk,
	// 100 MB) — the global maxFileSize — but the server keeps the pure disk
	// cap. Only validated when provided (pointer): a partial payload that
	// omits the field must not silently reset the limit. Rejected BEFORE any
	// setting is written (no partial-save).
	if body.DefaultMaxPDFSizeMB != nil {
		if msg := validatePDFQuota(c, *body.DefaultMaxPDFSizeMB); msg != "" {
			errorResponse(c, http.StatusBadRequest, msg)
			return
		}
	}

	reqCtx := c.Request.Context()

	// Turnstile validation must happen BEFORE any settings are written: enabling
	// Turnstile without keys would silently block ALL registrations (server-side
	// verification is fail-closed), and a late rejection here would leave the
	// earlier writes half-saved.
	if body.TurnstileEnabled {
		if strings.TrimSpace(body.TurnstileSiteKey) == "" {
			errorResponse(c, http.StatusBadRequest, "Site Key Turnstile wajib diisi untuk mengaktifkan Turnstile.")
			return
		}
		existingSecret, _ := models.GetSaasSetting(reqCtx, pool, models.SettingTurnstileSecretKey)
		secretProvided := strings.TrimSpace(body.TurnstileSecretKey) != "" && !strings.HasPrefix(strings.TrimSpace(body.TurnstileSecretKey), "****")
		if existingSecret == "" && !secretProvided {
			errorResponse(c, http.StatusBadRequest, "Secret Key Turnstile wajib diisi untuk mengaktifkan Turnstile.")
			return
		}
	}

	// Email settings
	emailEnabled := "0"
	if body.EmailVerificationEnabled {
		emailEnabled = "1"
	}
	if err := models.SetSaasSetting(reqCtx, pool, models.SettingEmailVerificationEnabled, emailEnabled); err != nil {
		log.Printf("save email_enabled error: %v", err)
	}

	// Email domain whitelist — normalize (lowercase, dedupe format) and store CSV.
	models.SetSaasSetting(reqCtx, pool, models.SettingEmailDomainWhitelist,
		strings.Join(models.ParseDomainList(body.EmailDomainWhitelist), ","))

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

	if body.DefaultMaxPDFSizeMB != nil {
		defaultPDFSize := int(math.Max(0, *body.DefaultMaxPDFSizeMB*1024*1024))
		if defaultPDFSize <= 0 {
			defaultPDFSize = 1048576 // minimum 1 MB; 0 tidak pernah tersimpan sebagai unlimited
		}
		models.SetSaasSetting(reqCtx, pool, models.SettingDefaultMaxPDFSize, strconv.Itoa(defaultPDFSize))
	}

	// Default storage quota (MB) for new registrations. 0 = tidak terbatas,
	// mirroring the per-user quota editor; nilai negatif di-clamp ke 0. This is
	// the ONLY numeric default saved conditionally (pointer): 0 is a meaningful
	// value (unlimited), so an absent field — e.g. an older cached UI during a
	// rollout — must NOT silently flip a configured quota to unlimited.
	if body.DefaultMaxStorageSizeMB != nil {
		defaultStorageSize := int64(math.Max(0, *body.DefaultMaxStorageSizeMB*1024*1024))
		models.SetSaasSetting(reqCtx, pool, models.SettingDefaultMaxStorageSize, strconv.FormatInt(defaultStorageSize, 10))
	}

	defaultConcurrent := body.DefaultMaxConcurrentExams
	if defaultConcurrent <= 0 {
		defaultConcurrent = 2
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingDefaultMaxConcurrentExams, strconv.Itoa(defaultConcurrent))

	defaultActiveDays := body.DefaultActiveDays
	if defaultActiveDays <= 0 {
		defaultActiveDays = 14
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

	// Save SEO settings
	models.SetSaasSetting(reqCtx, pool, models.SettingSEOTitle, strings.TrimSpace(body.SEOTitle))
	models.SetSaasSetting(reqCtx, pool, models.SettingSEODescription, strings.TrimSpace(body.SEODescription))
	models.SetSaasSetting(reqCtx, pool, models.SettingSEOKeywords, strings.TrimSpace(body.SEOKeywords))

	seoIndexVal := "0"
	if body.SEOIndex {
		seoIndexVal = "1"
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingSEOIndex, seoIndexVal)

	// Save footer settings
	models.SetSaasSetting(reqCtx, pool, models.SettingFooterText, strings.TrimSpace(body.FooterText))
	models.SetSaasSetting(reqCtx, pool, models.SettingFooterTagline, strings.TrimSpace(body.FooterTagline))

	// Voucher redemption toggle
	models.SetSaasSetting(reqCtx, pool, models.SettingVoucherRedeemEnabled, boolFlag(body.VoucherRedeemEnabled))

	// Cloudflare Turnstile bot protection. The secret key is stored only when
	// it is not the masked placeholder ("****...") — mirroring SMTP password
	// handling so an unchanged secret survives a save.
	models.SetSaasSetting(reqCtx, pool, models.SettingTurnstileEnabled, boolFlag(body.TurnstileEnabled))
	models.SetSaasSetting(reqCtx, pool, models.SettingTurnstileSiteKey, strings.TrimSpace(body.TurnstileSiteKey))
	turnstileSecret := strings.TrimSpace(body.TurnstileSecretKey)
	if turnstileSecret != "" && strings.HasPrefix(turnstileSecret, "****") {
		existing, _ := models.GetSaasSetting(reqCtx, pool, models.SettingTurnstileSecretKey)
		if existing != "" {
			turnstileSecret = existing
		}
	}
	if turnstileSecret != "" {
		if err := models.SetSaasSetting(reqCtx, pool, models.SettingTurnstileSecretKey, turnstileSecret); err != nil {
			log.Printf("save turnstile_secret_key error: %v", err)
		}
	}

	// Per-IP registration cap (0 = unlimited); clamp negatives to 0.
	maxPerIP := body.MaxAccountsPerIP
	if maxPerIP < 0 {
		maxPerIP = 0
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingMaxAccountsPerIP, strconv.Itoa(maxPerIP))

	// Per-exam approved-device cap for auto-approve. 0 = unlimited (a
	// meaningful value, preserved as-is); negatives are clamped to 0. Written
	// only when the field is present (pointer): an older cached UI that omits
	// it must not silently reset a configured cap to unlimited.
	if body.MaxApprovalsPerExam != nil {
		maxApprovals := *body.MaxApprovalsPerExam
		if maxApprovals < 0 {
			maxApprovals = 0
		}
		models.SetSaasSetting(reqCtx, pool, models.SettingMaxApprovalsPerExam, strconv.Itoa(maxApprovals))
	}

	// Approval-cleanup job tuning. Interval must stay >= 1 minute (a 0 would
	// make the background loop spin); grace/TTL clamp negatives to 0 (0 = no
	// grace / purge immediately, a deliberate choice). Written only when the
	// fields are present (pointers) so an older cached UI cannot reset them.
	if body.ApprovalCleanupIntervalMinutes != nil {
		interval := *body.ApprovalCleanupIntervalMinutes
		if interval < 1 {
			interval = 1
		}
		models.SetSaasSetting(reqCtx, pool, models.SettingApprovalCleanupIntervalMinutes, strconv.Itoa(interval))
	}
	if body.ApprovalCleanupEndedGraceHours != nil {
		grace := *body.ApprovalCleanupEndedGraceHours
		if grace < 0 {
			grace = 0
		}
		models.SetSaasSetting(reqCtx, pool, models.SettingApprovalCleanupEndedGraceHours, strconv.Itoa(grace))
	}
	if body.ApprovalCleanupInactiveTTLHours != nil {
		ttl := *body.ApprovalCleanupInactiveTTLHours
		if ttl < 0 {
			ttl = 0
		}
		models.SetSaasSetting(reqCtx, pool, models.SettingApprovalCleanupInactiveTTLHours, strconv.Itoa(ttl))
	}

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
