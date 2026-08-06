package admin

import (
	"context"
	cryptoRand "crypto/rand"
	"encoding/hex"
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

		renderAdminPage(c, "admin/users.html", gin.H{
			"active_page":         "users",
			"admin_instansi":      adminInstansi,
			"operator_expires_at": operatorExpiresAt,
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
		instansi := strings.TrimSpace(body.Instansi)
		var opInstansiID *int
		var opInstansiCode string

		if isOp {
			// Operator cannot create operator accounts
			for _, r := range roles {
				if r == models.RoleOperator {
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
				// Force expiry: user expiry = operator's expiry
				if opUser.ExpiresAt != nil {
					expiresAt := *opUser.ExpiresAt
					expiresAtPtr = &expiresAt
				}
			}
		}

		if instansi == "" {
			instansi = "personal"
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
		defaultDays := models.GetSaasSettingInt(ctx, pool,
			models.SettingDefaultActiveDays, 1)
		if expiresAtPtr == nil {
			expiresAtStr := strings.TrimSpace(body.ExpiresAt)
			if expiresAtStr != "" {
				t, err := time.Parse("2006-01-02 15:04:05", expiresAtStr)
				if err == nil {
					expiresAtPtr = &t
				}
			}
		}
		if expiresAtPtr == nil {
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

		pkg := strings.TrimSpace(body.Package)
		if pkg == "" {
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
				if strings.TrimSpace(r) == models.RoleOperator {
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

		if body.Package != nil {
			updates["package"] = strings.TrimSpace(*body.Package)
		}

		// Cascade: if operator's expiry changed, sync to all users in same instansi
		if targetUser.IsOperator() && body.ExpiresAt != nil {
			expVal := strings.TrimSpace(*body.ExpiresAt)
			if expVal != "" {
				if !strings.Contains(expVal, " ") {
					expVal += " 23:59:59"
				}
				if _, err := pool.Exec(ctx,
					`UPDATE admin_users SET expires_at = $1::timestamp WHERE instansi = $2 AND id != $3`,
					expVal, targetUser.Instansi, targetID); err != nil {
					log.Printf("cascade expiry for instansi %s error: %v", targetUser.Instansi, err)
				}
				// Align the instansi users' active packages with the new expiry too.
				if _, err := pool.Exec(ctx, `
					UPDATE voucher_redemptions r
					SET remaining_seconds = GREATEST(EXTRACT(EPOCH FROM ($1::timestamp - now()))::bigint, 0),
					    activated_at = now()
					FROM admin_users u
					WHERE r.user_id = u.id AND r.is_active AND u.instansi = $2 AND u.id != $3`,
					expVal, targetUser.Instansi, targetID); err != nil {
					log.Printf("cascade sync redemption expiry for instansi %s error: %v", targetUser.Instansi, err)
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
		if body.Status != nil && *body.Status == models.UserStatusActive && !isSuperAdminTarget && body.ExpiresAt == nil {
			if _, err := models.ResumeSuspendedAccountClock(ctx, pool, targetID); err != nil {
				log.Printf("edit user: resume suspended account clock error: %v", err)
			}
		}

		successMessage(c, fmt.Sprintf("Pengaturan user %s berhasil diperbarui", targetUser.Username))
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
						// period: expires_at is extended by (now - suspended_at), then
						// the active packages are realigned to the frozen expiry.
						if _, err := pool.Exec(ctx, `
						UPDATE admin_users u
						SET status = 'active',
						    suspended_by_cascade = FALSE,
						    suspended_at = NULL,
						    expires_at = CASE
						        WHEN u.suspended_at IS NOT NULL AND u.expires_at IS NOT NULL AND u.suspended_at < now()
						            THEN u.expires_at + (now() - u.suspended_at)
						        ELSE u.expires_at
						    END
						WHERE u.instansi = $1 AND u.suspended_by_cascade = TRUE AND u.id != $2`,
							opInstansi, targetID); err != nil {
							log.Printf("cascade restore for instansi %s error: %v", opInstansi, err)
						} else {
							if _, err := pool.Exec(ctx, `
							UPDATE voucher_redemptions r
							SET remaining_seconds = GREATEST(EXTRACT(EPOCH FROM (u.expires_at - now()))::bigint, 0),
							    activated_at = now()
							FROM admin_users u
							WHERE r.user_id = u.id AND r.is_active AND u.expires_at IS NOT NULL
							  AND u.instansi = $1 AND u.id != $2`,
								opInstansi, targetID); err != nil {
								log.Printf("cascade sync redemption expiry for instansi %s error: %v", opInstansi, err)
							}
							msg += fmt.Sprintf(". %d user di instansi %s juga diaktifkan kembali.", count, opInstansi)
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
// 8. POST /admin/api/users/update-instansi — Update Operator Instansi
// ---------------------------------------------------------------------------

func generateInstansiCode() string {
	b := make([]byte, 4)
	_, _ = cryptoRand.Read(b)
	hexStr := strings.ToUpper(hex.EncodeToString(b))
	return fmt.Sprintf("SCH-%s-%s", hexStr[:4], hexStr[4:])
}

// UpdateInstansi handles POST /admin/api/instansi/update
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
