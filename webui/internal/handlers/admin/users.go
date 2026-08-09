package admin

import (
	"context"
	cryptoRand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// effectiveBaseRoles returns the user's base roles for display on the users
// page. base_role is empty for fresh and pre-migration accounts (CreateUser
// does not set it, and the boot backfill only syncs package_role), so mirror
// applyRedemptionEntitlement's lazy init: base = current roles minus whatever
// the ACTIVE package grants. This keeps the badge split consistent with what
// the next package activation would compute.
func effectiveBaseRoles(u models.AdminUser) []string {
	base := parsePackageRoles(u.BaseRole)
	if u.BaseRole != "" {
		return base
	}
	pkg := parsePackageRoles(u.PackageRole)
	merged := models.ParseRoles(u.Role)
	out := make([]string, 0, len(merged))
	for _, r := range merged {
		if !containsRole(pkg, r) {
			out = append(out, r)
		}
	}
	return out
}

// getInstansiForOperator retrieves the instansi of the current operator user.
func getInstansiForOperator(ctx context.Context, pool *pgxpool.Pool, userID int) string {
	var instansi string
	err := pool.QueryRow(ctx,
		`SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&instansi)
	if err != nil {
		return ""
	}
	return instansi
}

// loadOperatorAccountQuota returns the school package's sub-account quota
// (maxUsers, 0 = unlimited) and the current number of accounts the operator
// may count against it (used, excluding the operator themself). The quota
// comes from the ACTIVE REDEMPTION's snapshot (captured at redeem time from
// package_settings or the custom voucher field) — NOT from the instansi label:
// a guru who redeemed a school voucher without yet setting a school instansi
// (instansi still the shared "personal" default) is still bound by the school
// package's max_users. Operators without an active redemption
// (legacy/imported) fall back to the package_settings row for their package
// label. For a real school instansi the used count is every account in that
// instansi; for the shared "personal" bucket only the operator's OWN
// sub-accounts count: scoped by created_by (the operator id recorded at
// creation), so several personal-bucket operators no longer count each
// other's sub-accounts. Legacy operator-created rows predating the created_by
// column (created_by IS NULL) cannot be attributed to any specific operator,
// so they keep the conservative shared-bucket fallback (counted against every
// personal operator) — strictly safer than silently ignoring them.
// Self-registered personal accounts are not sub-accounts and never consume a
// school quota. Shared by the CreateUser enforcement and the billing page
// display so the two can never disagree. (0, 0) when not applicable.
func loadOperatorAccountQuota(ctx context.Context, pool *pgxpool.Pool, userID int, isOperator bool, instansi string) (maxUsers, used int64) {
	instansi = strings.TrimSpace(instansi)
	if !isOperator || instansi == "" {
		return 0, 0
	}
	err := pool.QueryRow(ctx, `
		SELECT COALESCE(max_users, 0) FROM voucher_redemptions
		WHERE user_id = $1 AND is_active`, userID).Scan(&maxUsers)
	if err != nil {
		// Only an error (no active redemption) triggers the fallback, so an
		// active snapshot of 0 (intentional unlimited) is never overridden.
		if ferr := pool.QueryRow(ctx, `
			SELECT COALESCE(ps.max_users, 0)
			FROM package_settings ps
			JOIN admin_users u ON u.package = ps.pkg_key
			WHERE u.id = $1`, userID).Scan(&maxUsers); ferr != nil {
			// Fail open (0 = unlimited) but make the glitch visible: a
			// transient DB error must not silently bypass the quota.
			log.Printf("load operator account quota failed (redemption: %v, package: %v); treating as unlimited", err, ferr)
			maxUsers = 0
		}
	}
	if maxUsers <= 0 {
		return 0, 0
	}
	if strings.EqualFold(instansi, "personal") {
		// Shared "personal" bucket (operator redeemed a school voucher but has
		// not set a real school instansi yet): only sub-accounts the CURRENT
		// operator created (created_by = userID) are counted — the precise
		// per-operator attribution, so multiple personal-bucket operators no
		// longer share each other's sub-accounts. Legacy operator-created
		// rows predating the created_by column (operator_created = true,
		// created_by IS NULL) cannot be attributed to any specific operator,
		// so they keep the conservative shared-bucket fallback: counted
		// against every personal operator, never ignored. Self-registered
		// personal accounts (operator_created = false) are NOT sub-accounts
		// and must not consume the school quota — counting them would let
		// anyone starve a school's quota by registering personal accounts.
		_ = pool.QueryRow(ctx,
			`SELECT COUNT(*) FROM admin_users
			 WHERE instansi = $1 AND id <> $2
			   AND (created_by = $2 OR (created_by IS NULL AND operator_created))`,
			instansi, userID).Scan(&used)
		return maxUsers, used
	}
	_ = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_users WHERE instansi = $1 AND id <> $2`,
		instansi, userID).Scan(&used)
	return maxUsers, used
}

// ---------------------------------------------------------------------------
// 1. GET /admin/users — Render users management page
// ---------------------------------------------------------------------------

func UsersPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)

		adminInstansi := ""
		var operatorExpiresAt *string

		if isOperator(c) {
			instansi := getInstansiForOperator(c.Request.Context(), pool, userID)
			adminInstansi = instansi

			user, err := models.GetUserByID(c.Request.Context(), pool, userID)
			if err == nil && user.ExpiresAt != nil {
				s := user.ExpiresAt.Format("2006-01-02 15:04:05")
				operatorExpiresAt = &s
			}
		}

		freeMB := getFreeDiskSpace(getStoragePath(c)) / (1024 * 1024)
		// Preformatted "Sisa disk server: X GB/MB" supaya badge di header
		// Default Paket Pendaftaran langsung terisi saat render (tanpa flash
		// "memuat…"). Format disamakan dengan fmtStorageSize di admin.js.
		storageFreeDisplay := ""
		if freeMB > 0 {
			if freeMB >= 1024 {
				storageFreeDisplay = fmt.Sprintf("Sisa disk server: %.2f GB", freeMB/1024)
			} else {
				storageFreeDisplay = fmt.Sprintf("Sisa disk server: %.0f MB", freeMB)
			}
		}

		renderAdminPage(c, "admin/users.html", gin.H{
			"active_page":          "users",
			"admin_instansi":       adminInstansi,
			"operator_expires_at":  operatorExpiresAt,
			// Free space on the storage partition (MB), so the Tambah User &
			// Atur Limit forms can cap Maks Storage at what the server disk can
			// actually hold (0 = tidak dapat ditentukan).
			"storage_free_mb":      roundTo(freeMB, 2),
			"storage_free_display": storageFreeDisplay,
		})
	}
}

// ---------------------------------------------------------------------------
// 2. GET /admin/api/users — List users with search + pagination
// ---------------------------------------------------------------------------

func ListUsers() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		search := strings.TrimSpace(c.Query("search"))
		roleFilter := strings.TrimSpace(c.Query("role"))
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		if page < 1 {
			page = 1
		}
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "10"))
		if perPage < 5 {
			perPage = 5
		} else if perPage > 200 {
			perPage = 200
		}

		opts := models.ListUsersOpts{
			Page:              page,
			PerPage:           perPage,
			Search:            search,
			RoleFilter:        roleFilter,
			ExcludeSuperAdmin: !isSuperAdmin(c),
			ExcludeOperator:   isOp,
		}

		if isOp {
			instansi := getInstansiForOperator(ctx, pool, userID)
			opts.Instansi = instansi
		}

		result, err := models.ListUsers(ctx, pool, opts)
		if err != nil {
			log.Printf("list users error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar user")
			return
		}

		// Build user list
		type userItem struct {
			ID                 int      `json:"id"`
			Username           string   `json:"username"`
			Name               string   `json:"name"`
			WhatsappNumber     string   `json:"whatsapp_number"`
			Email              string   `json:"email"`
			Status             string   `json:"status"`
			MaxExams           int      `json:"max_exams"`
			MaxPDFSize         int      `json:"max_pdf_size"`
			MaxConcurrentExams int      `json:"max_concurrent_exams"`
			MaxStorageSize     int64    `json:"max_storage_size"`
			MaxStorageMB       int      `json:"max_storage_mb"`
			Instansi           string   `json:"instansi"`
			Roles              []string `json:"roles"`         // merged role list (base ∪ package)
			BaseRoles          []string `json:"base_roles"`    // roles held independently of packages
			PackageRoles       []string `json:"package_roles"` // roles granted by the active package
			Role               string   `json:"role"`
			ExpiresAt          string   `json:"expires_at"`
			ExamCount          int      `json:"exam_count"`
			CreatedAt          string   `json:"created_at"`
			Package            string   `json:"package"`
			// OperatorCreated exposes the origin flag to the Kelola Users page so
			// it can render the "Dibuat oleh Operator" badge on sub-accounts.
			OperatorCreated bool `json:"operator_created"`
		}

		users := make([]userItem, 0, len(result.Users))
		for _, u := range result.Users {
			expStr := ""
			if u.ExpiresAt != nil {
				expStr = u.ExpiresAt.Format("2006-01-02 15:04:05")
			}
			users = append(users, userItem{
				ID:                 u.ID,
				Username:           u.Username,
				Name:               u.Name,
				WhatsappNumber:     u.WhatsappNumber,
				Email:              u.Email,
				Status:             u.Status,
				MaxExams:           u.MaxExams,
				MaxPDFSize:         u.MaxPDFSize,
				MaxConcurrentExams: u.MaxConcurrentExams,
				MaxStorageSize:     u.MaxStorageSize,
				MaxStorageMB:       int(u.MaxStorageSize / (1024 * 1024)),
				Instansi:           u.Instansi,
				Roles:              models.ParseRoles(u.Role),
				BaseRoles:          effectiveBaseRoles(u.AdminUser),  // lazy-derived when base_role is empty
				PackageRoles:       parsePackageRoles(u.PackageRole), // empty-safe: '' means no package roles
				Role:               models.SerializeRoles(models.ParseRoles(u.Role)),
				ExpiresAt:          expStr,
				ExamCount:          u.ExamCount,
				CreatedAt:          formatISOUTC(u.CreatedAt),
				Package:            u.Package,
				OperatorCreated:    u.OperatorCreated,
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"users":   users,
			"pagination": gin.H{
				"page":        result.Page,
				"per_page":    result.PerPage,
				"total":       result.Total,
				"total_pages": result.TotalPages,
			},
		})
	}
}

// ---------------------------------------------------------------------------
// 3. POST /admin/api/users — Create new user
// ---------------------------------------------------------------------------

// validateStorageQuota returns an error message when the given per-user storage
// quota (MB) exceeds the free space of the server storage partition, or is
// negative. 0 = tidak terbatas (unlimited) is always allowed; an empty string
// means the value is acceptable. Mirrors the cap enforced on the SaaS "Maks
// Storage" default setting (settings.go). When free space cannot be determined
// (getFreeDiskSpace returns 0), the check is skipped (fail-open).
func validateStorageQuota(c *gin.Context, mb float64) string {
	return validateStorageQuotaFree(getFreeDiskSpace(getStoragePath(c)), mb)
}

// validateStorageQuotaFree is validateStorageQuota against a pre-computed
// freeBytes snapshot, so callers validating many values in one request (e.g.
// a package payload with several packages) reuse a single disk check instead
// of one statfs call per value.
func validateStorageQuotaFree(freeBytes float64, mb float64) string {
	return validateMBQuota("Maks Storage", freeBytes, mb)
}

// validatePDFQuota validates a max-PDF-size quota (MB) against the free disk
// space on the STORAGE_PATH partition, mirroring the storage rules: negative
// rejected, 0 = unlimited, fail-open when the free disk cannot be determined.
func validatePDFQuota(c *gin.Context, mb float64) string {
	return validatePDFQuotaFree(getFreeDiskSpace(getStoragePath(c)), mb)
}

// validatePDFQuotaFree is validatePDFQuota against a pre-computed freeBytes
// snapshot (single disk check for multi-value payloads).
func validatePDFQuotaFree(freeBytes float64, mb float64) string {
	return validateMBQuota("Maks Ukuran PDF", freeBytes, mb)
}

// validateMBQuota is the shared core for MB-based quota caps against the free
// disk space: 0 = unlimited (always accepted), negative rejected, and positive
// values above the free disk rejected (fail-open when freeBytes <= 0, i.e. the
// free disk cannot be determined).
func validateMBQuota(label string, freeBytes float64, mb float64) string {
	if mb < 0 {
		return fmt.Sprintf("%s tidak boleh bernilai negatif.", label)
	}
	if mb == 0 {
		return ""
	}
	if freeBytes > 0 && mb*1024*1024 > freeBytes {
		freeGB := freeBytes / (1024 * 1024 * 1024)
		return fmt.Sprintf("%s (%.2f MB) melebihi sisa kapasitas disk server (%.2f GB).", label, mb, freeGB)
	}
	return ""
}

func CreateUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Username           string   `json:"username"`
			Name               string   `json:"name"`
			Password           string   `json:"password"`
			WhatsappNumber     string   `json:"whatsapp_number"`
			Email              string   `json:"email"`
			Roles              []string `json:"roles"`
			Role               string   `json:"role"` // fallback if Roles is empty
			Instansi           string   `json:"instansi"`
			MaxExams           int      `json:"max_exams"`
			MaxPDFSizeMB       float64  `json:"max_pdf_size_mb"`
			MaxConcurrentExams int      `json:"max_concurrent_exams"`
			MaxStorageSizeMB   float64  `json:"max_storage_size_mb"`
			ExpiresAt          string   `json:"expires_at"`
			Package            string   `json:"package"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		// Storage quota must be validated before the account is created (a
		// rejected quota must not leave a half-created user). Same cap as the
		// SaaS "Maks Storage" default.
		if msg := validateStorageQuota(c, body.MaxStorageSizeMB); msg != "" {
			errorResponse(c, http.StatusBadRequest, msg)
			return
		}

		// PDF upload quota follows the same disk cap (validatePDFQuota):
		// negative rejected, 0 = tidak terbatas, positive value bounded by the
		// free disk.
		if msg := validatePDFQuota(c, body.MaxPDFSizeMB); msg != "" {
			errorResponse(c, http.StatusBadRequest, msg)
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		username := strings.ToLower(strings.TrimSpace(body.Username))
		password := body.Password

		if username == "" || password == "" {
			errorResponse(c, http.StatusBadRequest, "Username dan password wajib diisi")
			return
		}

		if !models.IsValidUsername(username) {
			errorResponse(c, http.StatusBadRequest, "Username hanya boleh berisi huruf kecil, angka, titik, garis bawah, dan strip (3-32 karakter)")
			return
		}

		if username == models.SuperAdminUsername {
			errorResponse(c, http.StatusBadRequest,
				fmt.Sprintf("Username %s sudah terdaftar sebagai Super Admin", models.SuperAdminUsername))
			return
		}

		// Check uniqueness
		_, err := models.GetUserByUsername(ctx, pool, username)
		if err == nil {
			errorResponse(c, http.StatusBadRequest, "Username sudah digunakan")
			return
		}

		// Validate roles
		roles := body.Roles
		if len(roles) == 0 && body.Role != "" {
			roles = strings.Split(body.Role, ",")
		}
		if len(roles) == 0 {
			roles = []string{models.RoleGuru}
		}

		// Operator restrictions
		var expiresAtPtr *time.Time
		// inheritUnlimited marks an operator whose own expiry is NULL
		// (unlimited — e.g. an admin cleared it in the edit form): its
		// sub-accounts inherit the unlimited state instead of falling back to
		// the default_active_days trial, mirroring the "Force expiry: user
		// expiry = operator's expiry" rule for operators with a concrete expiry.
		inheritUnlimited := false
		instansi := strings.TrimSpace(body.Instansi)
		var opInstansiID *int
		var opInstansiCode string

		if isOp {
			// Operator cannot create operator accounts. Trim + case-insensitive
			// compare so a hand-crafted " operator" / "OPERATOR" / " Operator "
			// (variants that would otherwise fall through the exact-match
			// whitelist below and silently degrade to guru) is rejected with the
			// intended 400 — mirroring the EditUser guard. The whitelist filter
			// keeps storing only exact lowercase role names, so a non-canonical
			// spelling can never be persisted.
			for _, r := range roles {
				if strings.EqualFold(strings.TrimSpace(r), models.RoleOperator) {
					errorResponse(c, http.StatusBadRequest, "Operator tidak dapat membuat akun dengan role Operator")
					return
				}
			}
			opUser, opErr := models.GetUserByID(ctx, pool, userID)
			if opErr == nil {
				instansi = opUser.Instansi
				opInstansiID = opUser.InstansiID
				_ = pool.QueryRow(ctx, `SELECT COALESCE(instansi_code, '') FROM admin_users WHERE id = $1`, userID).Scan(&opInstansiCode)

				// Force email from operator's account
				if opUser.Email != "" {
					body.Email = opUser.Email
				}
				// Force expiry: user expiry = operator's expiry (termasuk status
				// unlimited — operator dengan expires_at NULL membuat akun sub
				// yang unlimited juga, bukan trial default).
				if opUser.ExpiresAt != nil {
					expiresAt := *opUser.ExpiresAt
					expiresAtPtr = &expiresAt
				} else {
					inheritUnlimited = true
				}
			}
		}

		if instansi == "" {
			instansi = "personal"
		}

		// School sub-account quota (max_users): an operator may only create
		// accounts while the ACTIVE package still has room (see
		// loadOperatorAccountQuota). 0 = unlimited; the count is all accounts in
		// the operator's instansi except the operator themself.
		if isOp {
			maxUsers, used := loadOperatorAccountQuota(ctx, pool, userID, true, instansi)
			if maxUsers > 0 && used >= maxUsers {
				errorResponse(c, http.StatusBadRequest,
					fmt.Sprintf("Kuota akun di instansi Anda telah mencapai batas paket (%d akun). Silakan hubungi administrator untuk menambah kuota.", maxUsers))
				return
			}
		}

		filteredRoles := make([]string, 0, len(roles))
		for _, r := range roles {
			switch r {
			case models.RoleGuru, models.RolePengawas, models.RoleOperator:
				filteredRoles = append(filteredRoles, r)
			}
		}
		if len(filteredRoles) == 0 {
			filteredRoles = []string{models.RoleGuru}
		}

		roleStr := models.SerializeRoles(filteredRoles)

		// Default expiry — only apply form value when operator didn't already set it.
		// Operator unlimited (inheritUnlimited) skips both the form value and the
		// default_active_days fallback so the sub-account stays unlimited.
		defaultDays := models.GetSaasSettingInt(ctx, pool,
			models.SettingDefaultActiveDays, 14)
		if expiresAtPtr == nil && !inheritUnlimited {
			expiresAtStr := strings.TrimSpace(body.ExpiresAt)
			if expiresAtStr != "" {
				t, err := time.Parse("2006-01-02 15:04:05", expiresAtStr)
				if err == nil {
					expiresAtPtr = &t
				}
			}
		}
		if expiresAtPtr == nil && !inheritUnlimited {
			t := time.Now().UTC().AddDate(0, 0, defaultDays)
			expiresAtPtr = &t
		}

		maxExams := body.MaxExams
		if maxExams <= 0 {
			maxExams = models.GetSaasSettingInt(ctx, pool,
				models.SettingDefaultMaxExams, 3)
		}

		maxPDFSize := int(body.MaxPDFSizeMB * 1024 * 1024)
		if maxPDFSize <= 0 {
			maxPDFSize = models.GetSaasSettingInt(ctx, pool,
				models.SettingDefaultMaxPDFSize, 1048576)
		}

		maxConcurrentExams := body.MaxConcurrentExams
		if maxConcurrentExams <= 0 {
			maxConcurrentExams = models.GetSaasSettingInt(ctx, pool,
				models.SettingDefaultMaxConcurrentExams, 2)
		}

		maxStorageSize := int64(body.MaxStorageSizeMB * 1024 * 1024)

		// Sub-account package policy: an operator may never choose the
		// subscription package of the accounts it creates. Accounts created by
		// an operator (operator_created sub-accounts) cannot run their own
		// package — they cannot redeem/activate vouchers, and their quota,
		// role and expiry all follow the operator's school package — so the
		// package label is forced to "free" (a tampered request can no longer
		// label a sub-account with a paid school package).
		pkg := strings.TrimSpace(body.Package)
		if pkg == "" || isOp {
			pkg = "free"
		}

		user := &models.AdminUser{
			Username:           username,
			Name:               strings.TrimSpace(body.Name),
			PasswordHash:       password, // will be hashed by CreateUser
			Status:             models.UserStatusActive,
			Instansi:           instansi,
			Role:               roleStr,
			MaxExams:           maxExams,
			MaxPDFSize:         maxPDFSize,
			MaxConcurrentExams: maxConcurrentExams,
			MaxStorageSize:     maxStorageSize,
			WhatsappNumber:     strings.TrimSpace(body.WhatsappNumber),
			Email:              strings.TrimSpace(body.Email),
			ExpiresAt:          expiresAtPtr,
			Package:            pkg,
			// Sub-account voucher policy: an account created BY an operator is
			// marked operator_created and can never claim/activate vouchers
			// (origin-based, immutable — see RedeemVoucherHandler).
			OperatorCreated: isOp,
			// CreatedBy records WHICH operator created the account (the precise
			// attribution behind the shared "personal" bucket — see
			// loadOperatorAccountQuota). Set once at creation, never changed.
		}
		if isOp {
			createdByID := userID
			user.CreatedBy = &createdByID
		}

		created, err := models.CreateUser(ctx, pool, user)
		if err != nil {
			log.Printf("create user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
			return
		}

		// Ensure created user explicitly inherits operator's instansi_id and instansi_code
		if isOp && created != nil {
			_, _ = pool.Exec(ctx, `UPDATE admin_users SET instansi_id = $1, instansi_code = $2 WHERE id = $3`, opInstansiID, opInstansiCode, created.ID)
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("User %s berhasil dibuat", created.Username),
			"user_id": created.ID,
		})
	}
}

// ---------------------------------------------------------------------------
// 4. POST /admin/api/users/:user_id/edit — Edit user
// ---------------------------------------------------------------------------

func EditUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		var body struct {
			Name               *string  `json:"name"`
			MaxExams           *int     `json:"max_exams"`
			MaxPDFSizeMB       *float64 `json:"max_pdf_size_mb"`
			WhatsappNumber     *string  `json:"whatsapp_number"`
			Email              *string  `json:"email"`
			Status             *string  `json:"status"`
			Password           *string  `json:"password"`
			Instansi           *string  `json:"instansi"`
			Role               *string  `json:"role"`
			Roles              []string `json:"roles"`
			MaxConcurrentExams *int     `json:"max_concurrent_exams"`
			MaxStorageSizeMB   *float64 `json:"max_storage_size_mb"`
			ExpiresAt          *string  `json:"expires_at"`
			Package            *string  `json:"package"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			errorResponse(c, http.StatusBadRequest, "Data tidak valid")
			return
		}

		// Storage quota must be validated before any update is applied (a
		// rejected quota must not leave other fields half-edited). Same cap as
		// the SaaS "Maks Storage" default.
		if body.MaxStorageSizeMB != nil {
			if msg := validateStorageQuota(c, *body.MaxStorageSizeMB); msg != "" {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}
		}

		// PDF upload quota follows the same disk cap (only when provided).
		if body.MaxPDFSizeMB != nil {
			if msg := validatePDFQuota(c, *body.MaxPDFSizeMB); msg != "" {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		// Fetch target user
		targetUser, err := models.GetUserByID(ctx, pool, targetID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
			return
		}

		isSuperAdminTarget := targetUser.Username == models.SuperAdminUsername

		// Operator restrictions
		if isOp {
			opInstansi := getInstansiForOperator(ctx, pool, userID)
			if targetUser.Instansi != opInstansi {
				errorResponse(c, http.StatusBadRequest, "Anda hanya dapat mengelola user dalam satu instansi yang sama")
				return
			}
			if targetUser.IsOperator() {
				errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengelola akun dengan role Operator")
				return
			}
			// Operator cannot grant operator role. Check BOTH the plural
			// `roles` field and the singular `role` fallback, resolved exactly
			// like the apply logic below — otherwise sending {"role":"operator"}
			// (no `roles`) would bypass this restriction and escalate a user.
			requestedRoles := body.Roles
			if len(requestedRoles) == 0 && body.Role != nil {
				requestedRoles = strings.Split(*body.Role, ",")
			}
			for _, r := range requestedRoles {
				if strings.EqualFold(strings.TrimSpace(r), models.RoleOperator) {
					errorResponse(c, http.StatusBadRequest, "Operator tidak dapat memberikan role Operator")
					return
				}
			}
		}

		updates := make(map[string]interface{})

		if body.Name != nil {
			updates["name"] = strings.TrimSpace(*body.Name)
		}

		if body.MaxExams != nil {
			updates["max_exams"] = *body.MaxExams
		}

		if body.MaxPDFSizeMB != nil {
			pdfSize := int(*body.MaxPDFSizeMB * 1024 * 1024)
			updates["max_pdf_size"] = pdfSize
		}

		if body.MaxConcurrentExams != nil {
			updates["max_concurrent_exams"] = *body.MaxConcurrentExams
		}

		if body.MaxStorageSizeMB != nil {
			storageSize := int64(*body.MaxStorageSizeMB * 1024 * 1024)
			updates["max_storage_size"] = storageSize
		}

		if body.WhatsappNumber != nil {
			updates["whatsapp_number"] = strings.TrimSpace(*body.WhatsappNumber)
		}

		if body.Email != nil {
			updates["email"] = strings.TrimSpace(*body.Email)
		}

		if body.Instansi != nil {
			instansi := strings.TrimSpace(*body.Instansi)

			// Operator tidak boleh mengubah instansi user
			if isOp {
				opInstansi := getInstansiForOperator(ctx, pool, userID)
				if instansi != "" && instansi != opInstansi {
					errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengubah instansi user")
					return
				}
				// Force instansi untuk operator — operator tidak boleh mengganti instansi
				if instansi == "" || instansi != opInstansi {
					updates["instansi"] = opInstansi
				} else {
					updates["instansi"] = instansi
				}
			} else {
				if instansi == "" {
					errorResponse(c, http.StatusBadRequest, "Instansi tidak boleh kosong")
					return
				}

				// Cascade instansi: if the target is an operator and their
				// instansi changed, update all users in the same instansi too.
				if targetUser.IsOperator() && targetUser.Instansi != instansi {
					oldInstansi := targetUser.Instansi
					_, err := pool.Exec(ctx,
						`UPDATE admin_users SET instansi = $1 WHERE instansi = $2 AND id != $3`,
						instansi, oldInstansi, targetID)
					if err != nil {
						log.Printf("cascade instansi update error: %v", err)
						// Non-fatal — operator's own instansi is still updated
					}
				}

				updates["instansi"] = instansi
			}
		}

		if !isSuperAdminTarget && body.Status != nil {
			status := *body.Status
			switch status {
			case models.UserStatusActive, models.UserStatusSuspended, models.UserStatusPendingOTP:
				updates["status"] = status
				if status == models.UserStatusSuspended {
					// Record when the suspension starts so reactivation can
					// freeze the account clock for the suspension period.
					updates["suspended_at"] = time.Now().UTC()
				}
			}
		}

		if !isSuperAdminTarget && (body.Role != nil || body.Roles != nil) {
			roles := body.Roles
			if len(roles) == 0 && body.Role != nil {
				roles = strings.Split(*body.Role, ",")
			}
			filtered := make([]string, 0, len(roles))
			for _, r := range roles {
				switch r {
				case models.RoleGuru, models.RolePengawas, models.RoleOperator:
					filtered = append(filtered, r)
				}
			}
			if len(filtered) > 0 {
				updates["role"] = models.SerializeRoles(filtered)
				// Keep base_role in sync so admin-granted roles survive package
				// switches: base = (new roles minus whatever the current package
				// grants) ∪ (roles already held independently that are still
				// listed). The second term preserves an admin-granted role that
				// coincides with a package grant — e.g. an admin-granted operator
				// on a user whose active sekolah package also grants operator —
				// while an explicit removal still drops it. The union never
				// exceeds the roles the admin listed, so no role can appear in
				// base that the admin did not choose.
				var pkgRole, curBaseRole string
				_ = pool.QueryRow(ctx,
					`SELECT COALESCE(package_role, ''), COALESCE(base_role, '') FROM admin_users WHERE id = $1`,
					targetID).Scan(&pkgRole, &curBaseRole)
				pkgRoles := parsePackageRoles(pkgRole)
				base := make([]string, 0, len(filtered))
				for _, r := range filtered {
					if !containsRole(pkgRoles, r) {
						base = append(base, r)
					}
				}
				for _, r := range parsePackageRoles(curBaseRole) {
					if containsRole(filtered, r) && !containsRole(base, r) {
						base = append(base, r)
					}
				}
				if len(base) == 0 {
					base = []string{models.RoleGuru}
				}
				updates["base_role"] = models.SerializeRoles(base)
			}
		}

		if !isSuperAdminTarget && body.Password != nil && *body.Password != "" {
			updates["password_hash"] = *body.Password
		}

		if body.ExpiresAt != nil {
			expVal := strings.TrimSpace(*body.ExpiresAt)
			if expVal == "" {
				updates["expires_at"] = nil
			} else {
				if !strings.Contains(expVal, " ") {
					expVal += " 23:59:59"
				}
				updates["expires_at"] = expVal
			}
		}

		// Sub-account package policy (mirror of CreateUser): an operator may
		// never change the subscription package of the accounts below it — a
		// sub-account cannot run its own package, so the label stays as the
		// SuperAdmin left it ("free" for accounts the operator created). Only
		// a SuperAdmin may assign a package.
		if body.Package != nil && !isOp {
			updates["package"] = strings.TrimSpace(*body.Package)
		}

		// cascadeMsg collects the operator-expiry cascade result (set expiry or
		// cleared to unlimited) so the final success message surfaces it.
		var cascadeMsg string

		// Cascade: if operator's expiry changed, sync to all users in same instansi
		if targetUser.IsOperator() && body.ExpiresAt != nil {
			expVal := strings.TrimSpace(*body.ExpiresAt)
			if expVal != "" {
				if !strings.Contains(expVal, " ") {
					expVal += " 23:59:59"
				}
				// Same guards as the unlimited branch below (and the other
				// expiry-alignment paths): sub-accounts that run their own
				// active package (NOT EXISTS), hold an operator role themselves,
				// or are already unlimited (NULL) keep their own state — only
				// accounts that follow the operator get the new expiry.
				if _, err := pool.Exec(ctx, `
					UPDATE admin_users
					SET expires_at = $1::timestamp
					WHERE instansi = $2 AND id <> $3
					  AND expires_at IS NOT NULL
					  AND NOT (role ILIKE '%"operator"%')
					  AND NOT EXISTS (
					      SELECT 1 FROM voucher_redemptions vr
					      WHERE vr.user_id = admin_users.id AND vr.is_active
					  )`, expVal, targetUser.Instansi, targetID); err != nil {
					log.Printf("cascade expiry for instansi %s error: %v", targetUser.Instansi, err)
				}
				// Align the instansi users' active packages with the new expiry too
				// (their expires_at was just rewritten above, so the shared helper
				// reads the authoritative per-account expiry).
				if err := models.SyncInstansiActiveRedemptionsToExpiry(ctx, pool, targetUser.Instansi, targetID); err != nil {
					log.Printf("cascade sync redemption expiry for instansi %s error: %v", targetUser.Instansi, err)
				}
			} else {
				// Operator cleared to unlimited (expires_at = "" → NULL): the
				// school's sub-accounts follow the operator's unlimited state —
				// mirror of the CreateUser inheritUnlimited rule, applied to
				// already-created accounts. Guards are consistent with the other
				// expiry-alignment paths: sub-accounts that run their own active
				// package (NOT EXISTS), hold an operator role themselves, or are
				// already unlimited (NULL) keep their own state untouched.
				tag, err := pool.Exec(ctx, `
					UPDATE admin_users
					SET expires_at = NULL
					WHERE instansi = $1 AND id <> $2
					  AND expires_at IS NOT NULL
					  AND NOT (role ILIKE '%"operator"%')
					  AND NOT EXISTS (
					      SELECT 1 FROM voucher_redemptions vr
					      WHERE vr.user_id = admin_users.id AND vr.is_active
					  )`, targetUser.Instansi, targetID)
				if err != nil {
					log.Printf("cascade unlimited for instansi %s error: %v", targetUser.Instansi, err)
				} else if n := tag.RowsAffected(); n > 0 {
					cascadeMsg = fmt.Sprintf(". %d akun di instansi %s ikut menjadi unlimited (masa aktif dihapus).", n, targetUser.Instansi)
				}
			}
		}

		if len(updates) > 0 {
			if err := models.UpdateUser(ctx, pool, targetID, updates); err != nil {
				log.Printf("edit user error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
				return
			}
		}

		// Keep the active package's clock in sync with an admin-set expiry so
		// the billing display and later pause computations always match the
		// account (the account expiry is the authoritative clock).
		if body.ExpiresAt != nil {
			expVal := strings.TrimSpace(*body.ExpiresAt)
			if expVal != "" {
				if !strings.Contains(expVal, " ") {
					expVal += " 23:59:59"
				}
				if t, err := time.Parse("2006-01-02 15:04:05", expVal); err == nil {
					if syncErr := models.SyncActiveRedemptionToExpiry(ctx, pool, targetID, t); syncErr != nil {
						log.Printf("edit user: sync redemption expiry error: %v", syncErr)
					}
				}
			}
		}

		// Reactivating via the edit form: apply the suspension freeze (extend
		// expiry by the suspension duration) so the package clock did not burn
		// while the account was suspended. When a fresh expiry is explicitly set
		// in the same request, that grant supersedes the freeze.
		var freezeMsg string
		if body.Status != nil && *body.Status == models.UserStatusActive && !isSuperAdminTarget && body.ExpiresAt == nil {
			if extendedExpiry, err := models.ResumeSuspendedAccountClock(ctx, pool, targetID); err != nil {
				log.Printf("edit user: resume suspended account clock error: %v", err)
			} else if extendedExpiry != nil {
				// The suspension clock was frozen: expires_at was extended by the
				// suspension duration — which may even resurrect an account whose
				// expiry had already passed. Surface the new expiry so the admin
				// is not surprised by the extended lifetime.
				freezeMsg = fmt.Sprintf(" Masa aktif diperpanjang sampai %s (jam dijeda selama suspend)",
					extendedExpiry.Format("2006-01-02 15:04:05"))
			}
		}

		msg := fmt.Sprintf("Pengaturan user %s berhasil diperbarui.", targetUser.Username)
		if cascadeMsg != "" {
			msg += cascadeMsg
		}
		if freezeMsg != "" {
			msg += freezeMsg
		}
		successMessage(c, msg)
	}
}

// ---------------------------------------------------------------------------
// 5. POST /admin/api/users/:user_id/toggle-status — Suspend / activate
// ---------------------------------------------------------------------------

func ToggleUserStatus() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		targetUser, err := models.GetUserByID(ctx, pool, targetID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
			return
		}

		if targetUser.Username == models.SuperAdminUsername {
			errorResponse(c, http.StatusBadRequest, "Status Super Admin tidak dapat diubah")
			return
		}

		if isOp {
			opInstansi := getInstansiForOperator(ctx, pool, userID)
			if targetUser.Instansi != opInstansi {
				errorResponse(c, http.StatusBadRequest, "Anda hanya dapat mengelola user dalam satu instansi yang sama")
				return
			}
			// An operator must not toggle another operator's status (mirrors
			// EditUser). Otherwise operator A could suspend peer operator B and
			// the cascade below would suspend the entire instansi.
			if targetUser.IsOperator() {
				errorResponse(c, http.StatusBadRequest, "Operator tidak dapat mengelola akun dengan role Operator")
				return
			}
		}

		newStatus, msg, err := models.ToggleUserStatus(ctx, pool, targetID)
		if err != nil {
			log.Printf("toggle user status error: %v", err)
			// A pending_otp account cannot be toggled — surface the specific
			// reason (it must be verified via OTP or the manual Verify action)
			// instead of a generic failure message.
			if errors.Is(err, models.ErrPendingOTPToggleBlocked) {
				errorResponse(c, http.StatusBadRequest, err.Error())
				return
			}
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status user")
			return
		}

		// Cascade: track suspension/activation of operator's instansi.
		if targetUser.IsOperator() {
			opInstansi := targetUser.Instansi
			if opInstansi != "" {
				if newStatus == models.UserStatusActive {
					// Restore all cascade-suspended users
					var count int
					pool.QueryRow(ctx,
						`SELECT COUNT(*) FROM admin_users WHERE instansi = $1 AND suspended_by_cascade = TRUE AND id != $2`,
						opInstansi, targetID).Scan(&count)
					if count > 0 {
						// Freeze the restored accounts' clocks for the suspension
						// period (expires_at extended by the suspension duration) and
						// realign their active packages with the frozen expiry. The
						// helper fails as a unit, so a partial failure (restore done
						// but the redemption sync failed) suppresses the confirmation
						// message below — the log records the failure.
						if err := models.RestoreCascadeSuspendedInstansi(ctx, pool, opInstansi, targetID); err != nil {
							log.Printf("cascade restore for instansi %s error: %v", opInstansi, err)
						} else {
							msg += fmt.Sprintf(". %d user di instansi %s juga diaktifkan kembali.", count, opInstansi)
						}
					}

					// Align the sub-accounts' expiry with the operator's renewed
					// expiry (mirror of the restore branch in
					// syncInstansiWithOperatorRole): sub-accounts that do not run
					// their own active package follow the operator's expiry — a
					// school operator's accounts follow the operator's expiry — so
					// reactivating an expired operator also re-opens its school's
					// accounts instead of leaving them feature-locked. GREATEST
					// only ever EXTENDS an existing expiry (never shortens a
					// sub-account whose own expiry is already later than the
					// operator's new one), and sub-accounts with their own active
					// package (NOT EXISTS guard) or their own operator role are
					// left on their own clocks.
					var opExpiresAt *time.Time
					_ = pool.QueryRow(ctx, `SELECT expires_at FROM admin_users WHERE id = $1`, targetID).Scan(&opExpiresAt)
					if opExpiresAt != nil && opExpiresAt.After(time.Now().UTC()) {
						tag, err := pool.Exec(ctx, `
							UPDATE admin_users u
							SET expires_at = GREATEST(COALESCE(u.expires_at, $2::timestamptz), $2::timestamptz)
							WHERE u.instansi = $1 AND u.id <> $3
							  AND u.expires_at IS NOT NULL -- unlimited (NULL) subs keep their admin-set unlimited state
							  AND NOT (u.role ILIKE '%"operator"%')
							  AND NOT EXISTS (
							      SELECT 1 FROM voucher_redemptions vr
							      WHERE vr.user_id = u.id AND vr.is_active
							  )`, opInstansi, *opExpiresAt, targetID)
						if err != nil {
							log.Printf("cascade expiry for instansi %s error: %v", opInstansi, err)
						} else if n := tag.RowsAffected(); n > 0 {
							// Realign the sub-accounts' active package clocks with
							// their new expiry (their rows were just rewritten above,
							// so the per-account expiry is authoritative).
							if err := models.SyncInstansiActiveRedemptionsToExpiry(ctx, pool, opInstansi, targetID); err != nil {
								log.Printf("cascade sync redemption expiry for instansi %s error: %v", opInstansi, err)
							}
							msg += fmt.Sprintf(". %d akun di instansi %s ikut diperpanjang mengikuti masa aktif operator.", n, opInstansi)
						}
					}
				} else if newStatus == models.UserStatusSuspended {
					// Suspend active users with cascade flag
					var count int
					pool.QueryRow(ctx,
						`SELECT COUNT(*) FROM admin_users WHERE instansi = $1 AND status = 'active' AND id != $2`,
						opInstansi, targetID).Scan(&count)
					if count > 0 {
						if _, err := pool.Exec(ctx,
							`UPDATE admin_users SET status = 'suspended', suspended_by_cascade = TRUE, suspended_at = now() WHERE instansi = $1 AND status = 'active' AND id != $2`,
							opInstansi, targetID); err != nil {
							log.Printf("cascade suspend for instansi %s error: %v", opInstansi, err)
						} else {
							msg += fmt.Sprintf(". %d user di instansi %s juga dinonaktifkan.", count, opInstansi)
						}
					}
					// Tombstone the school's unpublished exams (policy B,
					// mirroring the voucher switch): a manual suspension freezes
					// the whole school, so every active-but-unstarted exam in the
					// instansi goes inactive (spareOperatorRoleCreators=false —
					// even operator-role subs are suspended by this cascade).
					// Exams already running stay untouched so students can
					// finish, and the tombstone is not auto-reversed when the
					// operator is reactivated.
					if err := tombstoneUnstartedInstansiExams(ctx, pool, opInstansi, false); err != nil {
						log.Printf("cascade tombstone exams for instansi %s error: %v", opInstansi, err)
					}
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"message":    msg,
			"new_status": newStatus,
		})
	}
}

// ---------------------------------------------------------------------------
// 6. POST /admin/api/users/:user_id/verify — Manually verify a pending user
// ---------------------------------------------------------------------------

func VerifyUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		ctx := c.Request.Context()

		targetUser, err := models.GetUserByID(ctx, pool, targetID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
			return
		}

		if isOp {
			opInstansi := getInstansiForOperator(ctx, pool, userID)
			if targetUser.Instansi != opInstansi {
				errorResponse(c, http.StatusBadRequest, "Anda hanya dapat mengelola user dalam satu instansi yang sama")
				return
			}
		}

		if err := models.VerifyUserManual(ctx, pool, targetID); err != nil {
			log.Printf("verify user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memverifikasi user")
			return
		}

		successMessage(c, fmt.Sprintf("User %s berhasil diaktifkan secara manual", targetUser.Username))
	}
}

// DeleteUser removes a user and cascades their exams.
func DeleteUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		targetID, err := strconv.Atoi(c.Param("user_id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID user tidak valid")
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		isOp := isOperator(c)
		isSuper := isSuperAdmin(c)
		ctx := c.Request.Context()

		if targetID == userID {
			errorResponse(c, http.StatusBadRequest, "Tidak dapat menghapus akun sendiri")
			return
		}

		targetUser, err := models.GetUserByID(ctx, pool, targetID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "User tidak ditemukan")
			return
		}

		if isSuper {
			// super admin can delete anyone
		} else if isOp {
			opInstansi := getInstansiForOperator(ctx, pool, userID)
			if targetUser.Instansi != opInstansi {
				errorResponse(c, http.StatusBadRequest, "Anda hanya dapat mengelola user dalam satu instansi yang sama")
				return
			}
		} else {
			errorResponse(c, http.StatusForbidden, "Tidak memiliki izin")
			return
		}

		paths, err := models.DeleteUser(ctx, pool, targetID)
		if err != nil {
			log.Printf("delete user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menghapus user")
			return
		}

		// Clean up associated files
		storageDir := getStoragePath(c)
		for _, p := range paths {
			if fp, err := helpers.SafeStoragePath(storageDir, p); err == nil {
				os.Remove(fp)
			}
		}

		msg := fmt.Sprintf("User %s berhasil dihapus", targetUser.Username)
		if targetUser.HasRole(models.RoleOperator) && targetUser.Instansi != "" {
			msg += fmt.Sprintf(" beserta semua user instansi %s", targetUser.Instansi)
		}
		successMessage(c, msg)
	}
}

// ---------------------------------------------------------------------------
// 8. POST /admin/api/instansi/update — Update Operator Instansi (management
// level: SuperAdmin & Operator, enforced at route registration)
// ---------------------------------------------------------------------------

func generateInstansiCode() string {
	b := make([]byte, 4)
	_, _ = cryptoRand.Read(b)
	hexStr := strings.ToUpper(hex.EncodeToString(b))
	return fmt.Sprintf("SCH-%s-%s", hexStr[:4], hexStr[4:])
}

// UpdateInstansi handles POST /admin/api/instansi/update (route is registered
// under AdminManagementRequired — only SuperAdmin & Operator may rename a
// school instansi, since the rename applies to every account sharing the
// instansi_id).
func UpdateInstansi() gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Instansi string `json:"instansi" form:"instansi"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			_ = c.ShouldBind(&body)
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		newInstansi := strings.TrimSpace(body.Instansi)
		if newInstansi == "" || strings.ToLower(newInstansi) == "personal" || len(newInstansi) < 3 {
			errorResponse(c, http.StatusBadRequest, "Nama instansi/sekolah tidak valid (min. 3 karakter)")
			return
		}

		user, err := models.GetUserByID(ctx, pool, userID)
		if err != nil {
			errorResponse(c, http.StatusNotFound, "Pengguna tidak ditemukan")
			return
		}

		// If user already has instansi_id, update name across all linked users
		var instansiID int
		var instansiCode string

		if user.InstansiID != nil && *user.InstansiID > 0 {
			instansiID = *user.InstansiID
			_ = pool.QueryRow(ctx, `SELECT COALESCE(code, '') FROM instansi WHERE id = $1`, instansiID).Scan(&instansiCode)
			if instansiCode == "" {
				instansiCode = generateInstansiCode()
				_, _ = pool.Exec(ctx, `UPDATE instansi SET code = $1 WHERE id = $2`, instansiCode, instansiID)
			}
			_, _ = pool.Exec(ctx, `UPDATE instansi SET name = $1 WHERE id = $2`, newInstansi, instansiID)
			_, err = pool.Exec(ctx, `UPDATE admin_users SET instansi = $1, instansi_code = $2 WHERE instansi_id = $3`, newInstansi, instansiCode, instansiID)
		} else {
			codeCandidate := generateInstansiCode()
			err = pool.QueryRow(ctx, `
				INSERT INTO instansi (name, code)
				VALUES ($1, $2)
				RETURNING id, code`, newInstansi, codeCandidate).Scan(&instansiID, &instansiCode)
			if err != nil {
				codeCandidate = generateInstansiCode()
				_ = pool.QueryRow(ctx, `
					INSERT INTO instansi (name, code)
					VALUES ($1, $2)
					RETURNING id, code`, newInstansi, codeCandidate).Scan(&instansiID, &instansiCode)
			}

			_, err = pool.Exec(ctx, `
				UPDATE admin_users 
				SET instansi = $1, instansi_id = $2, instansi_code = $3 
				WHERE id = $4`, newInstansi, instansiID, instansiCode, userID)
		}
		if err != nil {
			log.Printf("failed to update instansi for user %d: %v", userID, err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui instansi")
			return
		}

		// Sub-account migration: an operator who created sub-accounts while
		// still in the shared "personal" bucket (school instansi not yet set —
		// see loadOperatorAccountQuota) has those sub-accounts moved to the new
		// school instansi (instansi + instansi_id + instansi_code). Without
		// this, the subs keep their "personal" label and stop counting toward
		// the school quota (which counts by the operator's instansi label),
		// letting the operator create max_users more on top of the ones already
		// created. Scoped by created_by = the operator, so only sub-accounts
		// THIS operator created move — the precise per-operator attribution
		// that replaces the old shared-bucket approximation (where one personal
		// operator claiming a school would sweep another operator's personal
		// sub-accounts along). Legacy rows with created_by IS NULL cannot be
		// attributed and are deliberately left in "personal" (their quota
		// still counts via the loadOperatorAccountQuota legacy fallback).
		// Self-registered accounts (operator_created=false) are never touched.
		if user.IsOperator() && strings.EqualFold(strings.TrimSpace(user.Instansi), "personal") {
			if _, err := pool.Exec(ctx, `
				UPDATE admin_users
				SET instansi = $1, instansi_id = $2, instansi_code = $3
				WHERE created_by = $4 AND LOWER(instansi) = 'personal' AND id <> $4`,
				newInstansi, instansiID, instansiCode, userID); err != nil {
				log.Printf("migrate personal-bucket sub-accounts for user %d error: %v", userID, err)
			}
		}

		session := sessions.Default(c)
		session.Set(middleware.SessionKeyInstansi, newInstansi)
		_ = session.Save()

		c.JSON(http.StatusOK, gin.H{
			"success":       true,
			"message":       fmt.Sprintf("Nama Instansi '%s' berhasil disimpan! (Kode Unik: %s)", newInstansi, instansiCode),
			"instansi_code": instansiCode,
		})
	}
}
