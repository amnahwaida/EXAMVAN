package admin

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

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
			ID             int      `json:"id"`
			Username       string   `json:"username"`
			WhatsappNumber string   `json:"whatsapp_number"`
			Status         string   `json:"status"`
			MaxExams       int      `json:"max_exams"`
			MaxPDFSize     int      `json:"max_pdf_size"`
			MaxDrafts      int      `json:"max_drafts"`
			MaxDraftSize   int      `json:"max_draft_size"`
			Instansi       string   `json:"instansi"`
			Roles          []string `json:"roles"`
			Role           string   `json:"role"`
			ExpiresAt      string   `json:"expires_at"`
			ExamCount      int      `json:"exam_count"`
			CreatedAt      string   `json:"created_at"`
		}

		users := make([]userItem, 0, len(result.Users))
		for _, u := range result.Users {
			expStr := ""
			if u.ExpiresAt != nil {
				expStr = u.ExpiresAt.Format("2006-01-02 15:04:05")
			}
			users = append(users, userItem{
				ID:             u.ID,
				Username:       u.Username,
				WhatsappNumber: u.WhatsappNumber,
				Status:         u.Status,
				MaxExams:       u.MaxExams,
				MaxPDFSize:     u.MaxPDFSize,
				MaxDrafts:      u.MaxDrafts,
				MaxDraftSize:   u.MaxDraftSize,
				Instansi:       u.Instansi,
				Roles:          models.ParseRoles(u.Role),
				Role:           models.SerializeRoles(models.ParseRoles(u.Role)),
				ExpiresAt:      expStr,
				ExamCount:      u.ExamCount,
				CreatedAt:      formatISOUTC(u.CreatedAt),
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
			Username        string   `json:"username"`
			Password        string   `json:"password"`
			WhatsappNumber  string   `json:"whatsapp_number"`
			Roles           []string `json:"roles"`
			Role            string   `json:"role"` // fallback if Roles is empty
			Instansi        string   `json:"instansi"`
			MaxExams        int      `json:"max_exams"`
			MaxPDFSizeMB    float64  `json:"max_pdf_size_mb"`
			MaxDrafts       int      `json:"max_drafts"`
			MaxDraftSizeMB  float64  `json:"max_draft_size_mb"`
			ExpiresAt       string   `json:"expires_at"`
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
		instansi := strings.TrimSpace(body.Instansi)
		if isOp {
			// Operator cannot create operator accounts
			for _, r := range roles {
				if r == models.RoleOperator {
					errorResponse(c, http.StatusBadRequest, "Operator tidak dapat membuat akun dengan role Operator")
					return
				}
			}
			opInstansi := getInstansiForOperator(ctx, pool, userID)
			instansi = opInstansi
			// Force whatsapp from operator's account
			opUser, _ := models.GetUserByID(ctx, pool, userID)
			if opUser.WhatsappNumber != "" {
				body.WhatsappNumber = opUser.WhatsappNumber
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

		// Default expiry
		defaultDays := models.GetSaasSettingInt(ctx, pool,
			models.SettingDefaultActiveDays, 1)
		expiresAtStr := strings.TrimSpace(body.ExpiresAt)
		var expiresAtPtr *time.Time
		if expiresAtStr != "" {
			t, err := time.Parse("2006-01-02 15:04:05", expiresAtStr)
			if err == nil {
				expiresAtPtr = &t
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

		maxDrafts := body.MaxDrafts
		if maxDrafts <= 0 {
			maxDrafts = models.GetSaasSettingInt(ctx, pool,
				models.SettingDefaultMaxDrafts, 2)
		}

		maxDraftSize := int(body.MaxDraftSizeMB * 1024 * 1024)
		if maxDraftSize <= 0 {
			maxDraftSize = models.GetSaasSettingInt(ctx, pool,
				models.SettingDefaultMaxDraftSize, 1048576)
		}

		user := &models.AdminUser{
			Username:       username,
			PasswordHash:   password, // will be hashed by CreateUser
			Status:         models.UserStatusActive,
			Instansi:       instansi,
			Role:           roleStr,
			MaxExams:       maxExams,
			MaxPDFSize:     maxPDFSize,
			MaxDrafts:      maxDrafts,
			MaxDraftSize:   maxDraftSize,
			WhatsappNumber: strings.TrimSpace(body.WhatsappNumber),
			ExpiresAt:      expiresAtPtr,
		}

		created, err := models.CreateUser(ctx, pool, user)
		if err != nil {
			log.Printf("create user error: %v", err)
			errorResponse(c, http.StatusInternalServerError, "Gagal membuat user")
			return
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
			MaxExams       *int     `json:"max_exams"`
			MaxPDFSizeMB   *float64 `json:"max_pdf_size_mb"`
			WhatsappNumber *string  `json:"whatsapp_number"`
			Status         *string  `json:"status"`
			Password       *string  `json:"password"`
			Instansi       *string  `json:"instansi"`
			Role           *string  `json:"role"`
			Roles          []string `json:"roles"`
			MaxDrafts      *int     `json:"max_drafts"`
			MaxDraftSizeMB *float64 `json:"max_draft_size_mb"`
			ExpiresAt      *string  `json:"expires_at"`
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

		// Cannot edit super admin
		if targetUser.Username == models.SuperAdminUsername {
			errorResponse(c, http.StatusBadRequest,
				fmt.Sprintf("Super Admin %s tidak dapat diubah", models.SuperAdminUsername))
			return
		}

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
			// Operator cannot grant operator role
			if body.Roles != nil {
				for _, r := range body.Roles {
					if r == models.RoleOperator {
						errorResponse(c, http.StatusBadRequest, "Operator tidak dapat memberikan role Operator")
						return
					}
				}
			}
		}

		updates := make(map[string]interface{})

		if body.MaxExams != nil {
			updates["max_exams"] = *body.MaxExams
		}

		if body.MaxPDFSizeMB != nil {
			pdfSize := int(*body.MaxPDFSizeMB * 1024 * 1024)
			updates["max_pdf_size"] = pdfSize
		}

		if body.MaxDrafts != nil {
			updates["max_drafts"] = *body.MaxDrafts
		}

		if body.MaxDraftSizeMB != nil {
			draftSize := int(*body.MaxDraftSizeMB * 1024 * 1024)
			updates["max_draft_size"] = draftSize
		}

		if body.WhatsappNumber != nil {
			updates["whatsapp_number"] = strings.TrimSpace(*body.WhatsappNumber)
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

		if body.Status != nil {
			status := *body.Status
			switch status {
			case models.UserStatusActive, models.UserStatusSuspended, models.UserStatusPendingOTP:
				updates["status"] = status
			}
		}

		if body.Role != nil || body.Roles != nil {
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
			}
		}

		if body.Password != nil && *body.Password != "" {
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

		if len(updates) > 0 {
			if err := models.UpdateUser(ctx, pool, targetID, updates); err != nil {
				log.Printf("edit user error: %v", err)
				errorResponse(c, http.StatusInternalServerError, "Gagal memperbarui user")
				return
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
						if _, err := pool.Exec(ctx,
							`UPDATE admin_users SET status = 'active', suspended_by_cascade = FALSE WHERE instansi = $1 AND suspended_by_cascade = TRUE AND id != $2`,
							opInstansi, targetID); err != nil {
							log.Printf("cascade restore for instansi %s error: %v", opInstansi, err)
						} else {
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
							`UPDATE admin_users SET status = 'suspended', suspended_by_cascade = TRUE WHERE instansi = $1 AND status = 'active' AND id != $2`,
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
