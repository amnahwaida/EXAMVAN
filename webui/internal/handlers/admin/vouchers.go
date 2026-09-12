package admin

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// voucherInvalidMsg is the single, generic message returned for every
// code-level voucher failure (unknown, inactive, expired, quota full, already
// redeemed). Returning distinct messages would act as an oracle that lets an
// attacker distinguish a valid-but-exhausted code from a bogus one.
const voucherInvalidMsg = "Kode voucher tidak valid atau sudah tidak dapat digunakan."

// maxVoucherCodeLen caps the redeem code input. Generated codes are 12-15
// characters (EV-XXXX-XXXX or prefix-XXXX-XXXX), so the cap only rejects
// nonsense-sized input before it reaches the SQL UPPER/TRIM (harmless but
// wasteful). A length failure is input validation, NOT a code lookup, so a
// distinct message is safe — unlike the code-level failures that must stay
// generic (the voucherInvalidMsg oracle rule).
const maxVoucherCodeLen = 64

// subAccountRedeemBlockedMsg is returned whenever an account CREATED BY AN
// OPERATOR (admin_users.operator_created = true, a school sub-account) tries
// to claim or activate a voucher. Such accounts get their package, quota and
// expiry exclusively from the school package the operator manages; letting
// them self-service vouchers would let them bypass those controls (e.g.
// self-upgrade to an operator role via a sekolah voucher, or extend their own
// expiry independently of the school's). Origin-based: the block sticks even
// if the account's role changes later.
const subAccountRedeemBlockedMsg = "Akun yang dibuat oleh Operator tidak dapat menukar kode voucher. Kuota dan masa aktif akun ini dikelola oleh Operator/Super Admin."

// rejectOperatorCreatedAccount answers 403 when the current user is an
// operator-created sub-account and returns true; otherwise it returns false
// and the handler continues. It must be called BEFORE any voucher/redemption
// lookup so a blocked account can never use the response to distinguish a
// valid voucher code from a bogus one (the voucherInvalidMsg oracle rule):
// every claim attempt by a sub-account is answered with the same message.
func rejectOperatorCreatedAccount(c *gin.Context, pool *pgxpool.Pool, userID int, dbErrMsg string) bool {
	var operatorCreated bool
	if err := pool.QueryRow(c.Request.Context(),
		`SELECT COALESCE(operator_created, FALSE) FROM admin_users WHERE id = $1`, userID).Scan(&operatorCreated); err != nil {
		log.Printf("fetch operator_created error: %v", err)
		errorResponse(c, http.StatusInternalServerError, dbErrMsg)
		return true
	}
	if operatorCreated {
		errorResponse(c, http.StatusForbidden, subAccountRedeemBlockedMsg)
		return true
	}
	return false
}

// ListVouchers handles GET /admin/api/vouchers (SuperAdmin only).
func ListVouchers() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
		search := c.Query("search")
		// L51 (review_ui_halaman_web_2026-09-12.md): header kolom tabel voucher
		// kini sortable seperti tab Users — sort_by dibatasi whitelist di
		// models.voucherSortExprs; nilai asing senyap kembali ke urutan default.
		sortBy := strings.ToLower(strings.TrimSpace(c.Query("sort_by")))
		sortDir := strings.ToUpper(strings.TrimSpace(c.Query("sort_dir")))

		result, err := models.ListVouchers(ctx, pool, models.ListVouchersOpts{
			Page:    page,
			PerPage: perPage,
			Search:  search,
			SortBy:  sortBy,
			SortDir: sortDir,
		})
		if err != nil {
			log.Printf("list vouchers error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat daftar voucher")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"vouchers": result.Vouchers,
			"pagination": gin.H{
				"page":        result.Page,
				"per_page":    result.PerPage,
				"total":       result.Total,
				"total_pages": result.TotalPages,
			},
		})
	}
}

// voucherAtoiDefault parses a positive integer form value, or returns def.
func voucherAtoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n >= 0 {
		return n
	}
	return def
}

// parseCustomVoucherInto reads the custom-* form fields into v (used when the
// selected package is "custom"). Returns a non-empty error message on invalid
// input, otherwise "".
func parseCustomVoucherInto(c *gin.Context, v *models.Voucher) string {
	role := strings.TrimSpace(c.PostForm("custom_role"))
	switch role {
	case "", models.RoleGuru, models.RolePengawas, models.RoleOperator:
		// allowed (empty = do not change role)
	default:
		return "Role kustom tidak valid"
	}

	label := strings.TrimSpace(c.PostForm("custom_label"))
	if label == "" {
		label = "custom"
	}

	v.IsCustom = true
	v.CustomLabel = label
	v.Package = label // shown in listings / recorded in history
	v.CustomMaxExams = voucherAtoiDefault(c.PostForm("custom_max_exams"), 1)
	v.CustomMaxConcurrentExams = voucherAtoiDefault(c.PostForm("custom_max_concurrent_exams"), 1)

	// Maks ukuran PDF kustom tunduk pada batas kapasitas disk server yang sama
	// dengan editor storage lainnya (validatePDFQuota): nilai negatif ditolak
	// eksplisit, input tidak valid jatuh ke default 1 MB, lalu dicek terhadap
	// sisa disk. 0 = tidak terbatas.
	pdfMB := 1.0
	if s, err := strconv.ParseFloat(strings.TrimSpace(c.PostForm("custom_max_pdf_size_mb")), 64); err == nil {
		if s < 0 {
			return "Maks Ukuran PDF tidak boleh bernilai negatif."
		}
		pdfMB = s
	}
	// Disk check FIRST (its message wins when the disk is determinable), then
	// the hard ceiling as the final overflow guard for the fail-open case
	// (disk unknown → quota check skipped): a huge float64 flowing into
	// int64(mb*1024*1024) would silently overflow into a NEGATIVE quota.
	if msg := validatePDFQuota(c, pdfMB); msg != "" {
		return msg
	}
	if pdfMB > maxQuotaMB {
		return fmt.Sprintf("Maks Ukuran PDF terlalu besar (maksimal %.0f MB / 10 TB).", maxQuotaMB)
	}
	v.CustomMaxPDFSize = int64(pdfMB * 1024 * 1024)

	// Storage quota kustom tunduk pada batas kapasitas disk server yang sama
	// dengan editor storage lainnya (validateStorageQuota): nilai negatif
	// ditolak eksplisit, input tidak valid jatuh ke default 100 MB, lalu dicek
	// terhadap sisa disk. 0 = tidak terbatas.
	storageMB := 100.0
	if s, err := strconv.ParseFloat(strings.TrimSpace(c.PostForm("custom_max_storage_size_mb")), 64); err == nil {
		if s < 0 {
			return "Maks Storage tidak boleh bernilai negatif."
		}
		storageMB = s
	}
	// Disk check FIRST (its message wins when the disk is determinable), then
	// the hard ceiling as the final overflow guard for the fail-open case.
	if msg := validateStorageQuota(c, storageMB); msg != "" {
		return msg
	}
	if storageMB > maxQuotaMB {
		return fmt.Sprintf("Maks Storage terlalu besar (maksimal %.0f MB / 10 TB).", maxQuotaMB)
	}
	v.CustomMaxStorageSize = int64(storageMB * 1024 * 1024)
	v.CustomMaxUsers = int64(voucherAtoiDefault(c.PostForm("custom_max_users"), 0))
	v.CustomRole = role
	return ""
}

// maxVoucherDurationDays caps the custom voucher duration (hari): the day
// count is multiplied into seconds at redemption (durationDays × 86400) and
// later converted into a time.Duration in the entitlement math — values
// beyond the duration range would wrap the expiry computation and poison
// every redeemed account. 3650 hari = 10 tahun is far beyond any legitimate
// package lifetime while staying far inside the duration range.
const maxVoucherDurationDays = 3650

// maxQuotaMB caps the MB quota inputs of a custom voucher (PDF size and
// storage): a huge float64 flowing into int64(mb*1024*1024) would silently
// overflow into a NEGATIVE quota (MinInt64) that poisons every redeemed
// account. The ceiling applies independently of the free-disk check, which
// FAILS OPEN when the disk cannot be determined — so the hard cap is the
// overflow guard, the disk check is the capacity guard.
// 10 TB in MB = 10.000.000.
const maxQuotaMB = 10000000.0

// parseCustomDurationDays validates the form's custom_days: a positive day
// count within maxVoucherDurationDays becomes the numeric duration type,
// anything else yields a user-facing rejection.
func parseCustomDurationDays(customDaysStr string) (string, string) {
	d, err := strconv.Atoi(strings.TrimSpace(customDaysStr))
	if err != nil || d <= 0 {
		return "", "Jumlah hari durasi kustom harus berupa angka positif"
	}
	if d > maxVoucherDurationDays {
		return "", fmt.Sprintf("Jumlah hari durasi kustom maksimal %d hari (10 tahun)", maxVoucherDurationDays)
	}
	return strconv.Itoa(d), ""
}

// CreateVoucherHandler handles POST /admin/api/vouchers (SuperAdmin only).
func CreateVoucherHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		code := strings.TrimSpace(c.PostForm("code"))
		pkg := strings.TrimSpace(c.PostForm("package"))
		durationType := strings.TrimSpace(c.PostForm("duration_type"))
		maxUsageStr := strings.TrimSpace(c.PostForm("max_usage"))
		expiresAtStr := strings.TrimSpace(c.PostForm("expires_at"))
		notes := strings.TrimSpace(c.PostForm("notes"))

		if pkg == "" || durationType == "" {
			errorResponse(c, http.StatusBadRequest, "Paket dan tipe durasi wajib diisi")
			return
		}

		if durationType == "custom" {
			customDaysStr := strings.TrimSpace(c.PostForm("custom_days"))
			parsed, msg := parseCustomDurationDays(customDaysStr)
			if msg != "" {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}
			durationType = parsed
		}

		maxUsage := 1
		if maxUsageStr != "" {
			if m, err := strconv.Atoi(maxUsageStr); err == nil && m > 0 {
				maxUsage = m
			}
		}

		var expiresAt *time.Time
		if expiresAtStr != "" {
			if t, err := time.Parse("2006-01-02", expiresAtStr); err == nil {
				// End of specified day (23:59:59 UTC)
				tEnd := t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
				expiresAt = &tEnd
			}
		}

		v := &models.Voucher{
			Code:         code,
			Package:      pkg,
			DurationType: durationType,
			MaxUsage:     maxUsage,
			ExpiresAt:    expiresAt,
			IsActive:     true,
			Notes:        notes,
			CreatedByID:  &userID,
		}

		// Custom package: SuperAdmin defines the entitlement directly.
		if pkg == "custom" {
			if msg := parseCustomVoucherInto(c, v); msg != "" {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}
		}

		created, err := models.CreateVoucher(ctx, pool, v)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
				errorResponse(c, http.StatusBadRequest, "Kode voucher sudah digunakan. Gunakan kode lain.")
				return
			}
			log.Printf("create voucher error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal membuat voucher")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": fmt.Sprintf("Voucher %s berhasil dibuat", created.Code),
			"voucher": created,
		})
	}
}

// CreateBatchVouchersHandler handles POST /admin/api/vouchers/batch (SuperAdmin only).
func CreateBatchVouchersHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		ctx := c.Request.Context()

		prefix := strings.TrimSpace(c.PostForm("prefix"))
		pkg := strings.TrimSpace(c.PostForm("package"))
		durationType := strings.TrimSpace(c.PostForm("duration_type"))
		countStr := strings.TrimSpace(c.PostForm("count"))
		maxUsageStr := strings.TrimSpace(c.PostForm("max_usage"))
		expiresAtStr := strings.TrimSpace(c.PostForm("expires_at"))
		notes := strings.TrimSpace(c.PostForm("notes"))

		if pkg == "" || durationType == "" {
			errorResponse(c, http.StatusBadRequest, "Paket dan tipe durasi wajib diisi")
			return
		}

		if durationType == "custom" {
			customDaysStr := strings.TrimSpace(c.PostForm("custom_days"))
			parsed, msg := parseCustomDurationDays(customDaysStr)
			if msg != "" {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}
			durationType = parsed
		}

		count := 5
		if countStr != "" {
			if cnt, err := strconv.Atoi(countStr); err == nil && cnt > 0 {
				count = cnt
			}
		}

		maxUsage := 1
		if maxUsageStr != "" {
			if m, err := strconv.Atoi(maxUsageStr); err == nil && m > 0 {
				maxUsage = m
			}
		}

		var expiresAt *time.Time
		if expiresAtStr != "" {
			if t, err := time.Parse("2006-01-02", expiresAtStr); err == nil {
				tEnd := t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
				expiresAt = &tEnd
			}
		}

		tmpl := models.Voucher{
			Package:      pkg,
			DurationType: durationType,
			MaxUsage:     maxUsage,
			ExpiresAt:    expiresAt,
			Notes:        notes,
			CreatedByID:  &userID,
		}

		// Custom package: SuperAdmin defines the entitlement directly (applied
		// to every code in the batch).
		if pkg == "custom" {
			if msg := parseCustomVoucherInto(c, &tmpl); msg != "" {
				errorResponse(c, http.StatusBadRequest, msg)
				return
			}
		}

		createdList, err := models.CreateBatchVouchers(ctx, pool, prefix, count, tmpl)
		if err != nil {
			// A partial batch is surfaced, not hidden: the admin asked for N
			// codes and must know exactly how many exist — a silent shortfall
			// would leave printed codes that fail at the redeem point.
			log.Printf("batch create vouchers error: %v", err)
			errorResponse(c, http.StatusInternalServerError, err.Error())
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"message":  fmt.Sprintf("Berhasil membuat %d voucher", len(createdList)),
			"vouchers": createdList,
		})
	}
}

// ToggleVoucherStatusHandler handles POST /admin/api/vouchers/:id/toggle (SuperAdmin only).
func ToggleVoucherStatusHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID voucher tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		newStatus, err := models.ToggleVoucherStatus(ctx, pool, id)
		if err != nil {
			log.Printf("toggle voucher error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengubah status voucher")
			return
		}

		msg := "Voucher telah dinonaktifkan"
		if newStatus {
			msg = "Voucher telah diaktifkan kembali"
		}

		c.JSON(http.StatusOK, gin.H{
			"success":   true,
			"message":   msg,
			"is_active": newStatus,
		})
	}
}

// DeleteVoucherHandler handles POST /admin/api/vouchers/:id/delete (SuperAdmin only).
func DeleteVoucherHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID voucher tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		if err := models.DeleteVoucher(ctx, pool, id); err != nil {
			log.Printf("delete voucher error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menghapus voucher")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Voucher berhasil dihapus",
		})
	}
}

// ListVoucherRedemptionsHandler handles GET /admin/api/vouchers/:id/redemptions (SuperAdmin only).
func ListVoucherRedemptionsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.Atoi(c.Param("id"))
		if err != nil {
			errorResponse(c, http.StatusBadRequest, "ID voucher tidak valid")
			return
		}

		pool := getPool(c)
		ctx := c.Request.Context()

		redemptions, err := models.ListVoucherRedemptions(ctx, pool, id)
		if err != nil {
			log.Printf("list voucher redemptions error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat data riwayat penggunaan voucher")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"redemptions": redemptions,
		})
	}
}

// RedeemVoucherHandler handles POST /admin/api/vouchers/redeem (Available to any logged-in user).
func RedeemVoucherHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Voucher/promo redemption can be disabled by SuperAdmin. Scoped to NEW
		// code redemption only: ActivateVoucherHandler (resuming an
		// already-claimed package) is deliberately NOT gated by this setting — it
		// consumes no code and must stay consistent with the expiry-job
		// auto-fallback, which re-activates usable claimed packages so a user is
		// never locked out while one is on hand.
		if !models.GetSaasSettingBool(c.Request.Context(), getPool(c), models.SettingVoucherRedeemEnabled, true) {
			errorResponse(c, http.StatusForbidden, "Penukaran kode promo sedang tidak tersedia untuk saat ini.")
			return
		}

		code := strings.ToUpper(strings.TrimSpace(c.PostForm("code")))
		if code == "" {
			code = strings.ToUpper(strings.TrimSpace(c.Query("code")))
		}
		if code == "" {
			var body struct {
				Code string `json:"code"`
			}
			_ = c.ShouldBindJSON(&body)
			code = strings.ToUpper(strings.TrimSpace(body.Code))
		}

		if code == "" {
			errorResponse(c, http.StatusBadRequest, "Silakan masukkan kode voucher")
			return
		}
		// Input-size cap: generated codes are far below this bound, so a
		// longer input can only be junk. Checked before the tx/voucher lookup
		// (it is input validation, not a code probe).
		if len(code) > maxVoucherCodeLen {
			errorResponse(c, http.StatusBadRequest, fmt.Sprintf("Kode voucher terlalu panjang (maksimal %d karakter)", maxVoucherCodeLen))
			return
		}

		pool := getPool(c)
		userID := getCurrentUserID(c)
		if userID == 0 {
			errorResponse(c, http.StatusUnauthorized, "Sesi tidak valid. Silakan login kembali.")
			return
		}
		if getCurrentUserRole(c) == models.RoleSuperAdmin {
			errorResponse(c, http.StatusForbidden, "Akun SuperAdmin tidak dapat menukar kode voucher")
			return
		}
		// Sub-account voucher policy: an account created by an operator can
		// never claim any voucher. Checked before the tx and any voucher
		// lookup so the block is airtight and leaks no code-validity oracle.
		if rejectOperatorCreatedAccount(c, pool, userID, "Gagal memproses klaim voucher") {
			return
		}
		ctx := c.Request.Context()

		dbTx, err := pool.Begin(ctx)
		if err != nil {
			log.Printf("redeem voucher begin tx error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses klaim voucher")
			return
		}
		defer func() {
			_ = dbTx.Rollback(ctx)
		}()

		// 1. Lock and fetch voucher (case-insensitive & space-trimmed match)
		var v models.Voucher
		err = dbTx.QueryRow(ctx, `
		SELECT id, code, package, duration_type, max_usage, used_count, expires_at, is_active,
		       is_custom, COALESCE(custom_label, ''), COALESCE(custom_max_exams, 0),
		       COALESCE(custom_max_pdf_size, 0),
		       COALESCE(custom_max_concurrent_exams, 0),
		       COALESCE(custom_max_storage_size, 0),
		       COALESCE(custom_max_users, 0),
		       COALESCE(custom_role, '')
		FROM vouchers
		WHERE UPPER(TRIM(code)) = UPPER(TRIM($1))
		FOR UPDATE`, code).Scan(
			&v.ID, &v.Code, &v.Package, &v.DurationType, &v.MaxUsage, &v.UsedCount, &v.ExpiresAt, &v.IsActive,
			&v.IsCustom, &v.CustomLabel, &v.CustomMaxExams, &v.CustomMaxPDFSize,
			&v.CustomMaxConcurrentExams,
			&v.CustomMaxStorageSize, &v.CustomMaxUsers, &v.CustomRole,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				errorResponse(c, http.StatusBadRequest, voucherInvalidMsg)
				return
			}
			log.Printf("redeem voucher query error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses klaim voucher")
			return
		}

		// 2. Validate voucher. All code-level failures return the SAME generic
		// message (no "not found" vs "expired" vs "quota full" oracle) so an
		// attacker cannot distinguish a valid-but-exhausted code from a bogus
		// one while enumerating codes.
		now := time.Now().UTC()
		if !v.IsActive || (v.ExpiresAt != nil && v.ExpiresAt.Before(now)) || v.UsedCount >= v.MaxUsage {
			errorResponse(c, http.StatusBadRequest, voucherInvalidMsg)
			return
		}

		// 3. Check if user already redeemed this voucher
		var existingCount int
		err = dbTx.QueryRow(ctx, `
			SELECT COUNT(*) FROM voucher_redemptions
			WHERE voucher_id = $1 AND user_id = $2`, v.ID, userID).Scan(&existingCount)
		if err != nil {
			log.Printf("redeem check existing redemption error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memvalidasi penggunaan voucher")
			return
		}

		if existingCount > 0 {
			errorResponse(c, http.StatusBadRequest, voucherInvalidMsg)
			return
		}

		// One-operator-per-school serialization: two users of the SAME school
		// redeeming DIFFERENT school-package codes concurrently would each
		// pass the guard below (each reads the pre-commit snapshot — the
		// voucher row lock only serializes SAME-code redeems), so the claim
		// serializes per destination school via the same school-claim advisory
		// lock the claim/edit/create paths use. Taken BEFORE the user row lock
		// below — both this path and UpdateInstansi acquire advisory-then-row,
		// so they can never deadlock — and only for operator-granting
		// packages: a guru-package redeem cannot create a second operator. The
		// shared "personal" bucket is never a school and never serializes.
		// The acting user's instansi is read WITHOUT a lock (the authoritative
		// FOR UPDATE read follows below); the advisory key just needs the
		// school name, and the guard itself re-reads instansi under the lock.
		if v.IsCustom {
			if containsRole(models.ParseRoles(strings.TrimSpace(v.CustomRole)), models.RoleOperator) {
				if aErr := lockSchoolClaim(ctx, dbTx, userID); aErr != nil {
					log.Printf("redeem: lock school claim: %v", aErr)
					errorResponse(c, http.StatusInternalServerError, "Gagal memproses klaim voucher")
					return
				}
			}
		} else {
			if _, _, _, _, _, pkgRole := getPackageEntitlement(ctx, dbTx, v.Package); containsRole(models.ParseRoles(pkgRole), models.RoleOperator) {
				if aErr := lockSchoolClaim(ctx, dbTx, userID); aErr != nil {
					log.Printf("redeem: lock school claim: %v", aErr)
					errorResponse(c, http.StatusInternalServerError, "Gagal memproses klaim voucher")
					return
				}
			}
		}

		// 4. Lock the user row so concurrent claims by the same user serialize
		// (only one can be the "active package" at a time). A suspended account
		// must not be able to redeem: besides being against the admin's
		// decision, applyRedemptionEntitlement never touches status, so a
		// redeem here would only change quota/role/expiry on a frozen account.
		var lockedUserID int
		var lockedStatus string
		var lockedInstansi string
		err = dbTx.QueryRow(ctx, `SELECT id, status, COALESCE(instansi, '') FROM admin_users WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedUserID, &lockedStatus, &lockedInstansi)
		if err != nil {
			log.Printf("redeem fetch user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "User tidak ditemukan")
			return
		}
		if lockedStatus == models.UserStatusSuspended {
			errorResponse(c, http.StatusForbidden, "Akun Anda telah dinonaktifkan oleh administrator.")
			return
		}

		// 5. Build the entitlement snapshot for this redemption: the voucher's
		// package/quota/role captured at claim time, independent of the source
		// voucher row. Each redemption also gets its OWN remaining lifetime,
		// which ONLY decreases while the package is the active one (paused
		// packages freeze and resume automatically when activated again).
		var snapshot models.VoucherRedemption
		if v.IsCustom {
			snapshot = models.VoucherRedemption{
				Package:            strings.TrimSpace(v.CustomLabel),
				MaxExams:           int64(v.CustomMaxExams),
				MaxPDFSize:         v.CustomMaxPDFSize,
				MaxConcurrentExams: int64(v.CustomMaxConcurrentExams),
				MaxStorageSize:     v.CustomMaxStorageSize,
				MaxUsers:           v.CustomMaxUsers,
				Role:               strings.TrimSpace(v.CustomRole),
			}
			if snapshot.Package == "" {
				snapshot.Package = "custom"
			}
		} else {
			// Fixed packages read their quotas from the SuperAdmin-editable
			// package_settings table (falling back to the built-in defaults).
			exams, pdf, concurrent, storage, maxUsers, role := getPackageEntitlement(ctx, dbTx, v.Package)
			snapshot = models.VoucherRedemption{
				Package:            v.Package,
				MaxExams:           exams,
				MaxPDFSize:         pdf,
				MaxConcurrentExams: concurrent,
				MaxStorageSize:     storage,
				MaxUsers:           maxUsers,
				Role:               role,
			}
		}

		now = time.Now().UTC()
		snapshot.RemainingSeconds = int64(durationDays(v.DurationType)) * 86400
		snapshot.ActivatedAt = &now

		// One-operator-per-school policy: a school package grants the operator
		// role, so redeeming one while the user already sits in a school that
		// has ANOTHER operator would create a second. Rejected before anything
		// is paused/applied (the tx rolls back). The school's own operator
		// renewing with a new code is excluded (lockedInstansi read under the
		// same row lock); the shared "personal" bucket is never a school.
		if containsRole(models.ParseRoles(snapshot.Role), models.RoleOperator) {
			hasOp, opErr := schoolAlreadyHasOperator(ctx, dbTx, lockedInstansi, userID)
			if opErr != nil {
				log.Printf("redeem one-operator-per-school check error: %v", opErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal memproses klaim voucher")
				return
			}
			if hasOp {
				errorResponse(c, http.StatusBadRequest,
					"Instansi ini sudah memiliki operator. Satu sekolah hanya dapat memiliki satu operator.")
				return
			}
		}

		// 6. Pause the user's current active package (freeze its remaining
		// lifetime), then apply the new package's snapshot (overwrites quotas,
		// makes the role follow the active package, never touches a
		// SuperAdmin's role).
		if err := pauseActiveRedemption(ctx, dbTx, userID); err != nil {
			log.Printf("redeem pause previous error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses klaim voucher")
			return
		}
		if err := applyRedemptionEntitlement(ctx, dbTx, userID, &snapshot); err != nil {
			log.Printf("redeem apply entitlement error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menerapkan paket dari voucher")
			return
		}

		// 7. Increment voucher used count
		_, err = dbTx.Exec(ctx, `
			UPDATE vouchers
			SET used_count = used_count + 1, updated_at = CURRENT_TIMESTAMP
			WHERE id = $1`, v.ID)
		if err != nil {
			log.Printf("redeem increment voucher error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal meng-update data voucher")
			return
		}

		// 8. Record the redemption as the user's active package with its full
		// remaining lifetime (the partial unique index on (user_id) WHERE
		// is_active holds because the previous active row was just paused).
		_, err = dbTx.Exec(ctx, `
			INSERT INTO voucher_redemptions
				(voucher_id, user_id, remaining_seconds, activated_at, is_active, package,
				 max_exams, max_pdf_size, max_concurrent_exams, max_storage_size, max_users, role)
			VALUES ($1, $2, $3, $4, true, $5, $6, $7, $8, $9, $10, $11)`,
			v.ID, userID, snapshot.RemainingSeconds, now, snapshot.Package,
			snapshot.MaxExams, snapshot.MaxPDFSize, snapshot.MaxConcurrentExams,
			snapshot.MaxStorageSize, snapshot.MaxUsers, snapshot.Role)
		if err != nil {
			log.Printf("redeem insert redemption record error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mencatat klaim voucher")
			return
		}

		// Commit transaction
		if err := dbTx.Commit(ctx); err != nil {
			log.Printf("redeem commit error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan klaim voucher")
			return
		}

		// Refresh the session role to match the persisted role: a package switch
		// can both grant AND remove roles (e.g. leaving a sekolah package drops
		// the operator role), so always sync. Never demote a SuperAdmin.
		// NormalizeSessionRole keeps the stored session value canonical
		// ("operator" for any operator-holding role — multi-role JSON included)
		// exactly like the login handler, so a redeem without re-login never
		// leaves the session with a raw '["guru","operator"]' JSON that the
		// admin UI guards would have to re-parse.
		var updatedRole string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(role, '') FROM admin_users WHERE id = $1`, userID).Scan(&updatedRole); err == nil &&
			updatedRole != "" && !models.HasRole(updatedRole, models.RoleSuperAdmin) {
			session := sessions.Default(c)
			session.Set(middleware.SessionKeyRole, models.NormalizeSessionRole(updatedRole))
			_ = session.Save()
		}

		expiryStr := now.Add(time.Duration(snapshot.RemainingSeconds) * time.Second).Format("2006-01-02 15:04:05")

		// Append-only audit trail: WHO claimed WHICH voucher, with the
		// resulting package and expiry snapshotted (the same denormalized
		// style as the other audit rows, so the trail stays readable without
		// a join). Written after commit; best-effort. examID = 0 → NULL (no
		// exam involved).
		if aErr := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c),
			models.ActionVoucherRedeemed, 0,
			fmt.Sprintf("Voucher %s diklaim — paket %s aktif sampai %s", v.Code, strings.ToUpper(snapshot.Package), expiryStr)); aErr != nil {
			log.Printf("audit voucher redeemed: %v", aErr)
		}

		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"message":    fmt.Sprintf("Selamat! Voucher %s berhasil diklaim. Paket %s kini aktif sampai %s; paket lain otomatis dijeda.", v.Code, strings.ToUpper(snapshot.Package), expiryStr),
			"package":    snapshot.Package,
			"expires_at": expiryStr,
		})
	}
}

// ListMyRedemptionsHandler handles GET /admin/api/vouchers/mine (any logged-in
// user). Returns every voucher the user has claimed, with its entitlement
// snapshot, own lifetime, and whether it is currently the active package.
func ListMyRedemptionsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		if userID == 0 {
			errorResponse(c, http.StatusUnauthorized, "Sesi tidak valid. Silakan login kembali.")
			return
		}
		if getCurrentUserRole(c) == models.RoleSuperAdmin {
			errorResponse(c, http.StatusForbidden, "Akun SuperAdmin tidak memiliki paket voucher")
			return
		}
		// Sub-account voucher policy: an operator-created account can never
		// hold an own voucher package, so listing claimed packages is blocked
		// for it too — the same origin-based 403 as redeem/activate (also
		// hides any legacy pre-policy redemption still lingering on its row).
		if rejectOperatorCreatedAccount(c, pool, userID, "Gagal memuat paket yang diklaim") {
			return
		}

		redemptions, err := models.ListMyRedemptions(c.Request.Context(), pool, userID)
		if err != nil {
			log.Printf("list my redemptions error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat paket yang sudah diklaim")
			return
		}

		// The account's expires_at is the single authoritative clock for the
		// ACTIVE package (it gates login and is kept in sync with the package
		// clock on every redeem/activate and admin extension). Derive the
		// active package's remaining lifetime from it so an admin extension is
		// reflected immediately and the badge never contradicts the account
		// state. Paused packages keep their frozen remaining_seconds.
		var accountExpires *time.Time
		if err := pool.QueryRow(c.Request.Context(), `SELECT expires_at FROM admin_users WHERE id = $1`, userID).Scan(&accountExpires); err != nil {
			log.Printf("list my redemptions: load account expiry error: %v", err)
		}

		now := time.Now().UTC()
		type item struct {
			ID               int       `json:"id"`
			Code             string    `json:"code"`
			Package          string    `json:"package"`
			RedeemedAt       time.Time `json:"redeemed_at"`
			RemainingSeconds int64     `json:"remaining_seconds"`
			IsActive         bool      `json:"is_active"`
			IsExpired        bool      `json:"is_expired"`
			IsUnlimited      bool      `json:"is_unlimited"`
			MaxExams         int64     `json:"max_exams"`
			MaxPDFSizeMB     float64   `json:"max_pdf_size_mb"`
			MaxConcurrent    int64     `json:"max_concurrent_exams"`
			MaxStorageMB     float64   `json:"max_storage_mb"`
		}
		items := make([]item, 0, len(redemptions))
		for _, r := range redemptions {
			remaining := r.RemainingSeconds
			unlimited := false
			if r.IsActive {
				if accountExpires == nil {
					// Account has no expiry (admin cleared it): the active
					// package's clock is not running.
					unlimited = true
					remaining = 0
				} else {
					remaining = int64(accountExpires.Sub(now).Seconds())
					if remaining < 0 {
						remaining = 0
					}
				}
			}
			items = append(items, item{
				ID:               r.ID,
				Code:             r.Code,
				Package:          r.Package,
				RedeemedAt:       r.RedeemedAt,
				RemainingSeconds: remaining,
				IsActive:         r.IsActive,
				IsUnlimited:      unlimited,
				IsExpired:        !unlimited && remaining <= 0,
				MaxExams:         r.MaxExams,
				MaxPDFSizeMB:     roundTo(float64(r.MaxPDFSize)/(1024*1024), 1),
				MaxConcurrent:    r.MaxConcurrentExams,
				MaxStorageMB:     roundTo(float64(r.MaxStorageSize)/(1024*1024), 2),
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success":     true,
			"redemptions": items,
		})
	}
}

// ActivateVoucherHandler handles POST /admin/api/vouchers/activate (any
// logged-in user). Makes a previously-claimed voucher the user's ACTIVE package
// (only if it still has remaining lifetime), applying its entitlement snapshot
// to the account. The previously active package is automatically paused: its
// remaining lifetime freezes. This is the automatic resume — there is no
// user-facing pause/resume control.
//
// Deliberately NOT gated by SettingVoucherRedeemEnabled: activation consumes
// no code (the package was already claimed), so the redeem toggle only stops
// NEW claims. It must also agree with the expiry-job auto-fallback, which
// re-activates usable claimed packages on its own — a user-facing block while
// the background job still activates would contradict the "never locked out
// while a usable package is on hand" invariant.
func ActivateVoucherHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		if userID == 0 {
			errorResponse(c, http.StatusUnauthorized, "Sesi tidak valid. Silakan login kembali.")
			return
		}
		if getCurrentUserRole(c) == models.RoleSuperAdmin {
			errorResponse(c, http.StatusForbidden, "Akun SuperAdmin tidak memiliki paket voucher")
			return
		}
		// Sub-account voucher policy: an operator-created account can never
		// activate a voucher package either (it can never have claimed one;
		// this also blocks legacy/pre-policy redemptions still on its row).
		if rejectOperatorCreatedAccount(c, pool, userID, "Gagal memproses aktivasi paket") {
			return
		}
		ctx := c.Request.Context()

		redemptionID, _ := strconv.Atoi(strings.TrimSpace(c.PostForm("redemption_id")))
		if redemptionID == 0 {
			errorResponse(c, http.StatusBadRequest, "ID paket tidak valid")
			return
		}

		dbTx, err := pool.Begin(ctx)
		if err != nil {
			log.Printf("activate voucher begin tx error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses aktivasi paket")
			return
		}
		defer func() {
			_ = dbTx.Rollback(ctx)
		}()

		// One-operator-per-school serialization (mirror of the redeem guard):
		// activating a school package grants the operator role, so activations
		// of DIFFERENT packages in the SAME school serialize via the school-
		// claim advisory lock — two concurrent activations would otherwise
		// each pass the guard below against the pre-commit snapshot. Taken
		// BEFORE the user row lock below (advisory-then-row, the same order as
		// UpdateInstansi, so the two can never deadlock) and only for
		// operator-granting packages. The snapshot role and the user's
		// instansi are read WITHOUT a lock — the authoritative row/redemption
		// FOR UPDATE reads follow below.
		var snapRole string
		_ = dbTx.QueryRow(ctx,
			`SELECT COALESCE(role, '') FROM voucher_redemptions WHERE id = $1 AND user_id = $2`,
			redemptionID, userID).Scan(&snapRole)
		if containsRole(models.ParseRoles(snapRole), models.RoleOperator) {
			if aErr := lockSchoolClaim(ctx, dbTx, userID); aErr != nil {
				log.Printf("activate: lock school claim: %v", aErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal memproses aktivasi paket")
				return
			}
		}

		// Lock the user row so concurrent activates/redeems by the same user
		// serialize (only one package may be active at a time). A suspended
		// account must not be able to switch/activate packages: the account
		// clock is frozen while suspended, and applyRedemptionEntitlement never
		// touches status.
		var lockedUserID int
		var lockedStatus string
		var lockedInstansi string
		if err := dbTx.QueryRow(ctx, `SELECT id, status, COALESCE(instansi, '') FROM admin_users WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedUserID, &lockedStatus, &lockedInstansi); err != nil {
			log.Printf("activate lock user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "User tidak ditemukan")
			return
		}
		if lockedStatus == models.UserStatusSuspended {
			errorResponse(c, http.StatusForbidden, "Akun Anda telah dinonaktifkan oleh administrator.")
			return
		}

		// Lock and fetch the redemption (must belong to the current user).
		var r models.VoucherRedemption
		err = dbTx.QueryRow(ctx, `
			SELECT id, voucher_id, user_id, redeemed_at, remaining_seconds, activated_at, is_active,
			       COALESCE(package, ''), COALESCE(max_exams, 0),
			       COALESCE(max_pdf_size, 0), COALESCE(max_concurrent_exams, 0),
			       COALESCE(max_storage_size, 0), COALESCE(role, '')
			FROM voucher_redemptions
			WHERE id = $1 AND user_id = $2
			FOR UPDATE`, redemptionID, userID).Scan(
			&r.ID, &r.VoucherID, &r.UserID, &r.RedeemedAt, &r.RemainingSeconds, &r.ActivatedAt, &r.IsActive,
			&r.Package, &r.MaxExams, &r.MaxPDFSize, &r.MaxConcurrentExams,
			&r.MaxStorageSize, &r.Role,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				errorResponse(c, http.StatusNotFound, "Paket tidak ditemukan pada akun Anda")
				return
			}
			log.Printf("activate fetch redemption error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat paket")
			return
		}

		if r.IsActive {
			errorResponse(c, http.StatusBadRequest, "Paket ini sudah menjadi paket aktif Anda")
			return
		}

		if r.RemainingSeconds <= 0 {
			errorResponse(c, http.StatusBadRequest, "Masa aktif paket ini sudah berakhir, tidak dapat diaktifkan")
			return
		}

		// One-operator-per-school policy (mirror of the redeem guard):
		// activating a claimed school package in a school that already has
		// ANOTHER operator would create a second operator, so it is rejected
		// before anything is paused/applied.
		if containsRole(models.ParseRoles(r.Role), models.RoleOperator) {
			hasOp, opErr := schoolAlreadyHasOperator(ctx, dbTx, lockedInstansi, userID)
			if opErr != nil {
				log.Printf("activate one-operator-per-school check error: %v", opErr)
				errorResponse(c, http.StatusInternalServerError, "Gagal memproses aktivasi paket")
				return
			}
			if hasOp {
				errorResponse(c, http.StatusBadRequest,
					"Instansi ini sudah memiliki operator. Satu sekolah hanya dapat memiliki satu operator.")
				return
			}
		}

		now := time.Now().UTC()

		// Pause the current active package (freeze its remaining lifetime),
		// then resume this one from where it left off.
		if err := pauseActiveRedemption(ctx, dbTx, userID); err != nil {
			log.Printf("activate pause current error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengaktifkan paket")
			return
		}
		if _, err := dbTx.Exec(ctx, `
			UPDATE voucher_redemptions SET is_active = true, activated_at = $2
			WHERE id = $1`, redemptionID, now); err != nil {
			log.Printf("activate set active error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengaktifkan paket")
			return
		}

		// Apply the snapshot to the account (sets admin_users.expires_at =
		// now + remaining_seconds, resuming the timer).
		r.ActivatedAt = &now
		if err := applyRedemptionEntitlement(ctx, dbTx, userID, &r); err != nil {
			log.Printf("activate apply entitlement error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengaktifkan paket")
			return
		}

		if err := dbTx.Commit(ctx); err != nil {
			log.Printf("activate commit error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan aktivasi paket")
			return
		}

		// Refresh the session role to match the persisted role: a package switch
		// can both grant AND remove roles (e.g. leaving a sekolah package drops
		// the operator role), so always sync. Never demote a SuperAdmin.
		// NormalizeSessionRole keeps the stored session value canonical
		// ("operator" for any operator-holding role — multi-role JSON included)
		// exactly like the login handler, so an activate without re-login never
		// leaves the session with a raw '["guru","operator"]' JSON.
		var updatedRole string
		if err := pool.QueryRow(ctx, `SELECT COALESCE(role, '') FROM admin_users WHERE id = $1`, userID).Scan(&updatedRole); err == nil &&
			updatedRole != "" && !models.HasRole(updatedRole, models.RoleSuperAdmin) {
			session := sessions.Default(c)
			session.Set(middleware.SessionKeyRole, models.NormalizeSessionRole(updatedRole))
			_ = session.Save()
		}

		pkgLabel := strings.TrimSpace(r.Package)
		if pkgLabel == "" {
			pkgLabel = "custom"
		}
		expiryStr := now.Add(time.Duration(r.RemainingSeconds) * time.Second).Format("2006-01-02 15:04:05")

		// Append-only audit trail: WHO activated WHICH package, with the new
		// account expiry snapshotted. Written after commit; best-effort.
		// examID = 0 → NULL (no exam involved).
		if aErr := models.CreateAdminAuditLog(ctx, pool, userID, getCurrentUsername(c),
			models.ActionVoucherActivated, 0,
			fmt.Sprintf("Paket %s diaktifkan — masa aktif sampai %s", strings.ToUpper(pkgLabel), expiryStr)); aErr != nil {
			log.Printf("audit voucher activated: %v", aErr)
		}

		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"message":    fmt.Sprintf("Paket %s kini aktif dan melanjutkan sisa masanya sampai %s; paket lain otomatis dijeda.", strings.ToUpper(r.Package), expiryStr),
			"package":    r.Package,
			"expires_at": expiryStr,
		})
	}
}

// ListVoucherAuditLogs handles GET /admin/api/vouchers/audit-logs (SuperAdmin
// only): paginated, searchable voucher claim/activation audit trail, newest
// first. Search matches the actor username or the detail snapshot (e.g. a
// voucher code). Bounded per-page so the payload stays small regardless of
// trail size.
func ListVoucherAuditLogs() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
		search := c.Query("search")

		if perPage < 1 {
			perPage = 20
		} else if perPage > 200 {
			perPage = 200
		}

		result, err := models.ListVoucherAuditLogs(ctx, pool, models.ListVoucherAuditLogsOpts{
			Page:    page,
			PerPage: perPage,
			Search:  search,
		})
		if err != nil {
			log.Printf("list voucher audit logs error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat riwayat audit voucher")
			return
		}

		type auditItem struct {
			ID        int    `json:"id"`
			Username  string `json:"username"`
			Action    string `json:"action"`
			Detail    string `json:"detail"`
			CreatedAt string `json:"created_at"`
		}
		items := make([]auditItem, 0, len(result.Logs))
		for _, l := range result.Logs {
			items = append(items, auditItem{
				ID:        l.ID,
				Username:  l.Username,
				Action:    l.Action,
				Detail:    l.Detail,
				CreatedAt: l.CreatedAt.Format(time.RFC3339),
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"logs":    items,
			"pagination": gin.H{
				"page":        result.Page,
				"per_page":    result.PerPage,
				"total":       result.Total,
				"total_pages": result.TotalPages,
			},
		})
	}
}
