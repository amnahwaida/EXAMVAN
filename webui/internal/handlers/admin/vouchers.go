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
	v.CustomMaxDrafts = voucherAtoiDefault(c.PostForm("custom_max_drafts"), 1)
	v.CustomMaxPDFSize = voucherMBToBytes(c.PostForm("custom_max_pdf_size_mb"), 1)
	v.CustomMaxDraftSize = voucherMBToBytes(c.PostForm("custom_max_draft_size_mb"), 1)
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
			       COALESCE(custom_max_pdf_size, 0), COALESCE(custom_max_drafts, 0),
			       COALESCE(custom_max_draft_size, 0), COALESCE(custom_max_storage_size, 0),
			       COALESCE(custom_role, '')
			FROM vouchers
			WHERE UPPER(TRIM(code)) = UPPER(TRIM($1))
			FOR UPDATE`, code).Scan(
			&v.ID, &v.Code, &v.Package, &v.DurationType, &v.MaxUsage, &v.UsedCount, &v.ExpiresAt, &v.IsActive,
			&v.IsCustom, &v.CustomLabel, &v.CustomMaxExams, &v.CustomMaxPDFSize, &v.CustomMaxDrafts,
			&v.CustomMaxDraftSize, &v.CustomMaxStorageSize, &v.CustomRole,
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

		// 4. Lock and fetch user (COALESCE to avoid NULL scan errors)
		var user models.AdminUser
		err = dbTx.QueryRow(ctx, `
			SELECT id, username, expires_at, COALESCE(package, 'free'), COALESCE(role, '["guru"]')
			FROM admin_users
			WHERE id = $1
			FOR UPDATE`, userID).Scan(&user.ID, &user.Username, &user.ExpiresAt, &user.Package, &user.Role)
		if err != nil {
			log.Printf("redeem fetch user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "User tidak ditemukan")
			return
		}

		// 5. Calculate new expiry
		days := durationDays(v.DurationType)
		var newExpiry time.Time
		if user.ExpiresAt != nil && user.ExpiresAt.After(now) {
			newExpiry = user.ExpiresAt.AddDate(0, 0, days)
		} else {
			newExpiry = now.AddDate(0, 0, days)
		}

		// 6. Apply entitlement — custom vouchers carry their own limits/role;
		// otherwise fall back to the fixed package entitlement.
		roleMayChange := false
		if v.IsCustom {
			if v.CustomRole != "" {
				roleMayChange = true
			}
			if err := applyCustomVoucherEntitlement(ctx, dbTx, userID, &v, newExpiry); err != nil {
				log.Printf("redeem apply custom entitlement error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menerapkan paket dari voucher")
				return
			}
		} else {
			newRole := ""
			if _, _, _, _, role := packageEntitlement(v.Package); role != "" {
				newRole = role
				roleMayChange = true
			}
			if err := applyApprovedTransactionEntitlement(ctx, dbTx, userID, v.Package, v.DurationType, newExpiry, newRole); err != nil {
				log.Printf("redeem apply entitlement error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal menerapkan paket dari voucher")
				return
			}
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

		// 8. Record redemption
		_, err = dbTx.Exec(ctx, `
			INSERT INTO voucher_redemptions (voucher_id, user_id)
			VALUES ($1, $2)`, v.ID, userID)
		if err != nil {
			log.Printf("redeem insert redemption record error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mencatat klaim voucher")
			return
		}

		// 9. Record transaction history entry for auditability
		notes := fmt.Sprintf("Redeemed voucher %s", v.Code)
		_, err = dbTx.Exec(ctx, `
			INSERT INTO transactions (user_id, package, amount, duration_type, status, payment_method, proof_path, notes)
			VALUES ($1, $2, 0, $3, $4, 'voucher', '', $5)`,
			userID, v.Package, v.DurationType, models.TxStatusApproved, notes)
		if err != nil {
			log.Printf("redeem create transaction record error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal mencatat transaksi voucher")
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

		expiryStr := newExpiry.Format("2006-01-02 15:04:05")
		c.JSON(http.StatusOK, gin.H{
			"success":    true,
			"message":    fmt.Sprintf("Selamat! Voucher %s berhasil diklaim. Paket Anda kini aktif sebagai %s sampai %s.", v.Code, strings.ToUpper(v.Package), expiryStr),
			"package":    v.Package,
			"expires_at": expiryStr,
		})
	}
}
