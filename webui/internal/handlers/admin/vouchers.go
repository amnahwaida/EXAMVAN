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

	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// VouchersPage renders the GET /admin/vouchers management page for SuperAdmin.
func VouchersPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		role := getCurrentUserRole(c)
		if role != models.RoleSuperAdmin {
			c.Redirect(http.StatusFound, "/admin/dashboard")
			return
		}

		data := gin.H{
			"title":       "Manajemen Voucher",
			"active_page": "vouchers",
			"admin_user":  getCurrentUsername(c),
			"admin_role":  role,
		}

		renderAdminPage(c, "admin/vouchers.html", data)
	}
}

// ListVouchers handles GET /admin/api/vouchers (SuperAdmin only).
func ListVouchers() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		ctx := c.Request.Context()

		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
		search := c.Query("search")

		result, err := models.ListVouchers(ctx, pool, models.ListVouchersOpts{
			Page:    page,
			PerPage: perPage,
			Search:  search,
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

// voucherMBToBytes parses a megabyte float form value into bytes, or def (MB).
func voucherMBToBytes(s string, defMB float64) int64 {
	mb, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || mb < 0 {
		mb = defMB
	}
	return int64(mb * 1024 * 1024)
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
	v.CustomMaxPDFSize = voucherMBToBytes(c.PostForm("custom_max_pdf_size_mb"), 1)
	v.CustomMaxStorageSize = voucherMBToBytes(c.PostForm("custom_max_storage_size_mb"), 100)
	v.CustomRole = role
	return ""
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
			if d, err := strconv.Atoi(customDaysStr); err == nil && d > 0 {
				durationType = strconv.Itoa(d)
			} else {
				errorResponse(c, http.StatusBadRequest, "Jumlah hari durasi kustom harus berupa angka positif")
				return
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
			if d, err := strconv.Atoi(customDaysStr); err == nil && d > 0 {
				durationType = strconv.Itoa(d)
			} else {
				errorResponse(c, http.StatusBadRequest, "Jumlah hari durasi kustom harus berupa angka positif")
				return
			}
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
			log.Printf("batch create vouchers error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal membuat batch voucher")
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
		// Voucher/promo redemption can be disabled by SuperAdmin.
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

		pool := getPool(c)
		userID := getCurrentUserID(c)
		if userID == 0 {
			errorResponse(c, http.StatusUnauthorized, "Sesi tidak valid. Silakan login kembali.")
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
			       COALESCE(custom_role, '')
			FROM vouchers
			WHERE UPPER(TRIM(code)) = UPPER(TRIM($1))
			FOR UPDATE`, code).Scan(
			&v.ID, &v.Code, &v.Package, &v.DurationType, &v.MaxUsage, &v.UsedCount, &v.ExpiresAt, &v.IsActive,
			&v.IsCustom, &v.CustomLabel, &v.CustomMaxExams, &v.CustomMaxPDFSize,
			&v.CustomMaxConcurrentExams,
			&v.CustomMaxStorageSize, &v.CustomRole,
		)
		if err != nil {
			if err == pgx.ErrNoRows {
				errorResponse(c, http.StatusNotFound, "Kode voucher tidak ditemukan atau salah")
				return
			}
			log.Printf("redeem voucher query error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memproses klaim voucher")
			return
		}

		// 2. Validate voucher
		if !v.IsActive {
			errorResponse(c, http.StatusBadRequest, "Kode voucher ini sudah tidak aktif")
			return
		}

		now := time.Now().UTC()
		if v.ExpiresAt != nil && v.ExpiresAt.Before(now) {
			errorResponse(c, http.StatusBadRequest, "Kode voucher ini telah kadaluarsa")
			return
		}

		if v.UsedCount >= v.MaxUsage {
			errorResponse(c, http.StatusBadRequest, "Kuota penggunaan kode voucher ini telah habis")
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
			errorResponse(c, http.StatusBadRequest, "Anda sudah pernah menggunakan kode voucher ini")
			return
		}

		// 4. Lock the user row so concurrent claims by the same user serialize
		// (only one can be the "active package" at a time).
		var lockedUserID int
		err = dbTx.QueryRow(ctx, `SELECT id FROM admin_users WHERE id = $1 FOR UPDATE`, userID).Scan(&lockedUserID)
		if err != nil {
			log.Printf("redeem fetch user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "User tidak ditemukan")
			return
		}

		// 5. Build the entitlement snapshot for this redemption: the voucher's
		// package/quota/role captured at claim time, independent of the source
		// voucher row. Each redemption also gets its OWN lifetime starting now
		// (no merging with the account's current expiry), so the user can later
		// switch between claimed packages without their lifetimes interleaving.
		var snapshot models.VoucherRedemption
		if v.IsCustom {
			snapshot = models.VoucherRedemption{
				Package:            strings.TrimSpace(v.CustomLabel),
				MaxExams:           int64(v.CustomMaxExams),
				MaxPDFSize:         v.CustomMaxPDFSize,
				MaxConcurrentExams: int64(v.CustomMaxConcurrentExams),
				MaxStorageSize:     v.CustomMaxStorageSize,
				Role:               strings.TrimSpace(v.CustomRole),
			}
			if snapshot.Package == "" {
				snapshot.Package = "custom"
			}
		} else {
			exams, pdf, concurrent, storage, role := packageEntitlement(v.Package)
			snapshot = models.VoucherRedemption{
				Package:            v.Package,
				MaxExams:           exams,
				MaxPDFSize:         pdf,
				MaxConcurrentExams: concurrent,
				MaxStorageSize:     storage,
				Role:               role,
			}
		}

		days := durationDays(v.DurationType)
		redeemExpiry := time.Now().UTC().AddDate(0, 0, days)
		snapshot.ExpiresAt = &redeemExpiry

		// 6. Apply the entitlement snapshot (overwrites quotas, merges role,
		// never touches a SuperAdmin's role).
		roleMayChange := strings.TrimSpace(snapshot.Role) != ""
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

		// 8. Record redemption with its snapshot + lifetime, and make it the
		// user's active package (deactivate the previous one first so the
		// partial unique index on (user_id) WHERE is_active holds).
		_, err = dbTx.Exec(ctx, `
			UPDATE voucher_redemptions SET is_active = false
			WHERE user_id = $1 AND is_active`, userID)
		if err != nil {
			log.Printf("redeem deactivate previous error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mencatat klaim voucher")
			return
		}

		_, err = dbTx.Exec(ctx, `
			INSERT INTO voucher_redemptions
				(voucher_id, user_id, expires_at, is_active, package,
				 max_exams, max_pdf_size, max_concurrent_exams, max_storage_size, role)
			VALUES ($1, $2, $3, true, $4, $5, $6, $7, $8, $9)`,
			v.ID, userID, redeemExpiry, snapshot.Package,
			snapshot.MaxExams, snapshot.MaxPDFSize, snapshot.MaxConcurrentExams,
			snapshot.MaxStorageSize, snapshot.Role)
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

		// Refresh session role to match the (merged) role now stored, without
		// demoting a SuperAdmin. Read the actual persisted role rather than
		// blindly setting the package role, which could strip existing roles.
		if roleMayChange {
			var updatedRole string
			if err := pool.QueryRow(ctx, `SELECT COALESCE(role, '') FROM admin_users WHERE id = $1`, userID).Scan(&updatedRole); err == nil &&
				updatedRole != "" && !models.HasRole(updatedRole, models.RoleSuperAdmin) {
				session := sessions.Default(c)
				session.Set(middleware.SessionKeyRole, updatedRole)
				_ = session.Save()
			}
		}

		expiryStr := redeemExpiry.Format("2006-01-02 15:04:05")
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"message":    fmt.Sprintf("Selamat! Voucher %s berhasil diklaim. Paket Anda kini aktif sebagai %s sampai %s.", v.Code, strings.ToUpper(snapshot.Package), expiryStr),
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

		redemptions, err := models.ListMyRedemptions(c.Request.Context(), pool, userID)
		if err != nil {
			log.Printf("list my redemptions error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal memuat paket yang sudah diklaim")
			return
		}

		now := time.Now().UTC()
		type item struct {
			ID           int        `json:"id"`
			Code         string     `json:"code"`
			Package      string     `json:"package"`
			RedeemedAt   time.Time  `json:"redeemed_at"`
			ExpiresAt    *time.Time `json:"expires_at"`
			IsActive     bool       `json:"is_active"`
			IsExpired    bool       `json:"is_expired"`
			MaxExams     int64      `json:"max_exams"`
			MaxPDFSizeMB float64    `json:"max_pdf_size_mb"`
			MaxConcurrent int64     `json:"max_concurrent_exams"`
			MaxStorageMB float64    `json:"max_storage_mb"`
		}
		items := make([]item, 0, len(redemptions))
		for _, r := range redemptions {
			expired := r.ExpiresAt == nil || r.ExpiresAt.Before(now)
			items = append(items, item{
				ID:            r.ID,
				Code:          r.Code,
				Package:       r.Package,
				RedeemedAt:    r.RedeemedAt,
				ExpiresAt:     r.ExpiresAt,
				IsActive:      r.IsActive,
				IsExpired:     expired,
				MaxExams:      r.MaxExams,
				MaxPDFSizeMB:  roundTo(float64(r.MaxPDFSize)/(1024*1024), 1),
				MaxConcurrent: r.MaxConcurrentExams,
				MaxStorageMB:  roundTo(float64(r.MaxStorageSize)/(1024*1024), 2),
			})
		}

		c.JSON(http.StatusOK, gin.H{
			"success":      true,
			"redemptions":  items,
		})
	}
}

// ActivateVoucherHandler handles POST /admin/api/vouchers/activate (any
// logged-in user). Makes a previously-claimed voucher the user's ACTIVE package
// (only if its own lifetime has not expired yet), applying its entitlement
// snapshot to the account. At most one package can be active at a time.
func ActivateVoucherHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		pool := getPool(c)
		userID := getCurrentUserID(c)
		if userID == 0 {
			errorResponse(c, http.StatusUnauthorized, "Sesi tidak valid. Silakan login kembali.")
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

		// Lock and fetch the redemption (must belong to the current user).
		var r models.VoucherRedemption
		err = dbTx.QueryRow(ctx, `
			SELECT id, voucher_id, user_id, redeemed_at, expires_at, is_active,
			       COALESCE(package, ''), COALESCE(max_exams, 0),
			       COALESCE(max_pdf_size, 0), COALESCE(max_concurrent_exams, 0),
			       COALESCE(max_storage_size, 0), COALESCE(role, '')
			FROM voucher_redemptions
			WHERE id = $1 AND user_id = $2
			FOR UPDATE`, redemptionID, userID).Scan(
			&r.ID, &r.VoucherID, &r.UserID, &r.RedeemedAt, &r.ExpiresAt, &r.IsActive,
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

		now := time.Now().UTC()
		if r.ExpiresAt == nil || r.ExpiresAt.Before(now) {
			errorResponse(c, http.StatusBadRequest, "Masa aktif paket ini sudah berakhir, tidak dapat diaktifkan")
			return
		}

		// Apply the snapshot to the account.
		if err := applyRedemptionEntitlement(ctx, dbTx, userID, &r); err != nil {
			log.Printf("activate apply entitlement error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengaktifkan paket")
			return
		}

		// Deactivate any other active package first, then activate this one.
		if _, err := dbTx.Exec(ctx, `
			UPDATE voucher_redemptions SET is_active = false
			WHERE user_id = $1 AND is_active AND id <> $2`, userID, redemptionID); err != nil {
			log.Printf("activate deactivate others error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengaktifkan paket")
			return
		}
		if _, err := dbTx.Exec(ctx, `
			UPDATE voucher_redemptions SET is_active = true
			WHERE id = $1`, redemptionID); err != nil {
			log.Printf("activate set active error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mengaktifkan paket")
			return
		}

		if err := dbTx.Commit(ctx); err != nil {
			log.Printf("activate commit error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal menyimpan aktivasi paket")
			return
		}

		// Refresh session role if the activated package grants roles.
		if strings.TrimSpace(r.Role) != "" {
			var updatedRole string
			if err := pool.QueryRow(ctx, `SELECT COALESCE(role, '') FROM admin_users WHERE id = $1`, userID).Scan(&updatedRole); err == nil &&
				updatedRole != "" && !models.HasRole(updatedRole, models.RoleSuperAdmin) {
				session := sessions.Default(c)
				session.Set(middleware.SessionKeyRole, updatedRole)
				_ = session.Save()
			}
		}

		expiryStr := "—"
		if r.ExpiresAt != nil {
			expiryStr = r.ExpiresAt.Format("2006-01-02 15:04:05")
		}
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"message":    fmt.Sprintf("Paket %s kini aktif sampai %s", strings.ToUpper(r.Package), expiryStr),
			"package":    r.Package,
			"expires_at": expiryStr,
		})
	}
}
