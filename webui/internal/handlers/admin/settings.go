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
	defaultMaxConcurrentExams := parseIntSetting(settings[models.SettingDefaultMaxConcurrentExams], 2)
	defaultMaxDraftSize := parseIntSetting(settings[models.SettingDefaultMaxDraftSize], 1048576)
	defaultActiveDays := parseIntSetting(settings[models.SettingDefaultActiveDays], 1)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"settings": gin.H{
			"email_verification_enabled":   emailEnabled,
			"email_domain_whitelist":       settings[models.SettingEmailDomainWhitelist],
			"smtp_host":                    settings[models.SettingSMTPHost],
			"smtp_port":                    settings[models.SettingSMTPPort],
			"smtp_user":                    settings[models.SettingSMTPUser],
			"smtp_password":                smtpPassword,
			"smtp_sender_name":             settings[models.SettingSMTPSenderName],
			"default_max_exams":            defaultMaxExams,
			"default_max_pdf_size_mb":      roundTo(float64(defaultMaxPDFSize)/(1024*1024), 2),
			"default_max_drafts":           defaultMaxDrafts,
			"default_max_concurrent_exams": defaultMaxConcurrentExams,
			"default_max_draft_size_mb":    roundTo(float64(defaultMaxDraftSize)/(1024*1024), 2),
			"default_active_days":          defaultActiveDays,
			"android_version":              settings[models.SettingAndroidVersion],
			"webapp_version":               settings[models.SettingWebappVersion],
			"certificate_fingerprint":      settings[models.SettingCertificateFingerprint],
			"seo_title":                    settings[models.SettingSEOTitle],
			"seo_description":              settings[models.SettingSEODescription],
			"seo_keywords":                 settings[models.SettingSEOKeywords],
			"seo_index":                    settings[models.SettingSEOIndex] == "1",
			"doku_payment_methods":         settings[models.SettingDokuPaymentMethods],

			// Monetization toggles (default enabled when unset).
			"doku_payment_enabled":   settings[models.SettingDokuPaymentEnabled] != "0",
			"pricing_page_enabled":   settings[models.SettingPricingPageEnabled] != "0",
			"voucher_redeem_enabled": settings[models.SettingVoucherRedeemEnabled] != "0",

			// Pricing values
			"price_guru_bulanan":              parseIntSetting(settings[models.SettingPriceGuruBulanan], 25000),
			"price_guru_semester":             parseIntSetting(settings[models.SettingPriceGuruSemester], 125000),
			"price_guru_tahunan":              parseIntSetting(settings[models.SettingPriceGuruTahunan], 225000),
			"price_individu_bulanan":          parseIntSetting(settings[models.SettingPriceIndividuBulanan], 50000),
			"price_individu_semester":         parseIntSetting(settings[models.SettingPriceIndividuSemester], 250000),
			"price_individu_tahunan":          parseIntSetting(settings[models.SettingPriceIndividuTahunan], 450000),
			"price_sekolah_kecil_bulanan":     parseIntSetting(settings[models.SettingPriceSekolahKecilBulanan], 75000),
			"price_sekolah_kecil_semester":    parseIntSetting(settings[models.SettingPriceSekolahKecilSemester], 375000),
			"price_sekolah_kecil_tahunan":     parseIntSetting(settings[models.SettingPriceSekolahKecilTahunan], 675000),
			"price_sekolah_menengah_bulanan":  parseIntSetting(settings[models.SettingPriceSekolahMenengahBulanan], 175000),
			"price_sekolah_menengah_semester": parseIntSetting(settings[models.SettingPriceSekolahMenengahSemester], 875000),
			"price_sekolah_menengah_tahunan":  parseIntSetting(settings[models.SettingPriceSekolahMenengahTahunan], 1575000),
			"price_sekolah_besar_bulanan":     parseIntSetting(settings[models.SettingPriceSekolahBesarBulanan], 375000),
			"price_sekolah_besar_semester":    parseIntSetting(settings[models.SettingPriceSekolahBesarSemester], 1875000),
			"price_sekolah_besar_tahunan":     parseIntSetting(settings[models.SettingPriceSekolahBesarTahunan], 3375000),
			"price_sekolah_unggulan_bulanan":  parseIntSetting(settings[models.SettingPriceSekolahUnggulanBulanan], 750000),
			"price_sekolah_unggulan_semester": parseIntSetting(settings[models.SettingPriceSekolahUnggulanSemester], 3750000),
			"price_sekolah_unggulan_tahunan":  parseIntSetting(settings[models.SettingPriceSekolahUnggulanTahunan], 6750000),
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
		EmailVerificationEnabled  bool    `json:"email_verification_enabled"`
		EmailDomainWhitelist      string  `json:"email_domain_whitelist"`
		SMTPHost                  string  `json:"smtp_host"`
		SMTPPort                  string  `json:"smtp_port"`
		SMTPUser                  string  `json:"smtp_user"`
		SMTPPassword              string  `json:"smtp_password"`
		SMTPSenderName            string  `json:"smtp_sender_name"`
		DefaultMaxExams           int     `json:"default_max_exams"`
		DefaultMaxPDFSizeMB       float64 `json:"default_max_pdf_size_mb"`
		DefaultMaxDrafts          int     `json:"default_max_drafts"`
		DefaultMaxConcurrentExams int     `json:"default_max_concurrent_exams"`
		DefaultMaxDraftSizeMB     float64 `json:"default_max_draft_size_mb"`
		DefaultActiveDays         int     `json:"default_active_days"`
		AndroidVersion            string  `json:"android_version"`
		WebappVersion             string  `json:"webapp_version"`
		CertificateFingerprint    string  `json:"certificate_fingerprint"`
		SEOTitle                  string  `json:"seo_title"`
		SEODescription            string  `json:"seo_description"`
		SEOKeywords               string  `json:"seo_keywords"`
		SEOIndex                  bool    `json:"seo_index"`
		DokuPaymentMethods        string  `json:"doku_payment_methods"`

		// Monetization toggles
		DokuPaymentEnabled   bool `json:"doku_payment_enabled"`
		PricingPageEnabled   bool `json:"pricing_page_enabled"`
		VoucherRedeemEnabled bool `json:"voucher_redeem_enabled"`

		// Price Settings
		PriceGuruBulanan             int `json:"price_guru_bulanan"`
		PriceGuruSemester            int `json:"price_guru_semester"`
		PriceGuruTahunan             int `json:"price_guru_tahunan"`
		PriceIndividuBulanan         int `json:"price_individu_bulanan"`
		PriceIndividuSemester        int `json:"price_individu_semester"`
		PriceIndividuTahunan         int `json:"price_individu_tahunan"`
		PriceSekolahKecilBulanan     int `json:"price_sekolah_kecil_bulanan"`
		PriceSekolahKecilSemester    int `json:"price_sekolah_kecil_semester"`
		PriceSekolahKecilTahunan     int `json:"price_sekolah_kecil_tahunan"`
		PriceSekolahMenengahBulanan  int `json:"price_sekolah_menengah_bulanan"`
		PriceSekolahMenengahSemester int `json:"price_sekolah_menengah_semester"`
		PriceSekolahMenengahTahunan  int `json:"price_sekolah_menengah_tahunan"`
		PriceSekolahBesarBulanan     int `json:"price_sekolah_besar_bulanan"`
		PriceSekolahBesarSemester    int `json:"price_sekolah_besar_semester"`
		PriceSekolahBesarTahunan     int `json:"price_sekolah_besar_tahunan"`
		PriceSekolahUnggulanBulanan  int `json:"price_sekolah_unggulan_bulanan"`
		PriceSekolahUnggulanSemester int `json:"price_sekolah_unggulan_semester"`
		PriceSekolahUnggulanTahunan  int `json:"price_sekolah_unggulan_tahunan"`
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

	defaultConcurrent := body.DefaultMaxConcurrentExams
	if defaultConcurrent <= 0 {
		defaultConcurrent = 2
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingDefaultMaxConcurrentExams, strconv.Itoa(defaultConcurrent))

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

	// Save SEO settings
	models.SetSaasSetting(reqCtx, pool, models.SettingSEOTitle, strings.TrimSpace(body.SEOTitle))
	models.SetSaasSetting(reqCtx, pool, models.SettingSEODescription, strings.TrimSpace(body.SEODescription))
	models.SetSaasSetting(reqCtx, pool, models.SettingSEOKeywords, strings.TrimSpace(body.SEOKeywords))

	seoIndexVal := "0"
	if body.SEOIndex {
		seoIndexVal = "1"
	}
	models.SetSaasSetting(reqCtx, pool, models.SettingSEOIndex, seoIndexVal)

	// DOKU Payment Methods
	dokuMethods := strings.TrimSpace(body.DokuPaymentMethods)
	if dokuMethods != "" {
		models.SetSaasSetting(reqCtx, pool, models.SettingDokuPaymentMethods, dokuMethods)
	}

	// Monetization toggles
	models.SetSaasSetting(reqCtx, pool, models.SettingDokuPaymentEnabled, boolFlag(body.DokuPaymentEnabled))
	models.SetSaasSetting(reqCtx, pool, models.SettingPricingPageEnabled, boolFlag(body.PricingPageEnabled))
	models.SetSaasSetting(reqCtx, pool, models.SettingVoucherRedeemEnabled, boolFlag(body.VoucherRedeemEnabled))

	// Save Pricing Settings
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceGuruBulanan, strconv.Itoa(body.PriceGuruBulanan))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceGuruSemester, strconv.Itoa(body.PriceGuruSemester))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceGuruTahunan, strconv.Itoa(body.PriceGuruTahunan))

	models.SetSaasSetting(reqCtx, pool, models.SettingPriceIndividuBulanan, strconv.Itoa(body.PriceIndividuBulanan))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceIndividuSemester, strconv.Itoa(body.PriceIndividuSemester))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceIndividuTahunan, strconv.Itoa(body.PriceIndividuTahunan))

	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahKecilBulanan, strconv.Itoa(body.PriceSekolahKecilBulanan))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahKecilSemester, strconv.Itoa(body.PriceSekolahKecilSemester))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahKecilTahunan, strconv.Itoa(body.PriceSekolahKecilTahunan))

	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahMenengahBulanan, strconv.Itoa(body.PriceSekolahMenengahBulanan))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahMenengahSemester, strconv.Itoa(body.PriceSekolahMenengahSemester))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahMenengahTahunan, strconv.Itoa(body.PriceSekolahMenengahTahunan))

	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahBesarBulanan, strconv.Itoa(body.PriceSekolahBesarBulanan))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahBesarSemester, strconv.Itoa(body.PriceSekolahBesarSemester))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahBesarTahunan, strconv.Itoa(body.PriceSekolahBesarTahunan))

	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahUnggulanBulanan, strconv.Itoa(body.PriceSekolahUnggulanBulanan))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahUnggulanSemester, strconv.Itoa(body.PriceSekolahUnggulanSemester))
	models.SetSaasSetting(reqCtx, pool, models.SettingPriceSekolahUnggulanTahunan, strconv.Itoa(body.PriceSekolahUnggulanTahunan))

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
