// Package admin provides Gin handler functions for EXAMVAN admin panel routes
// (dashboard, exam CRUD, user management, submissions, pengawas, and settings).
package admin

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

// getPool extracts the PostgreSQL connection pool from the gin context.
func getPool(c *gin.Context) *pgxpool.Pool {
	return c.MustGet("db").(*pgxpool.Pool)
}

// getStoragePath returns the configured storage directory, falling back to
// the compiled-in default when no config is available in context.
func getStoragePath(c *gin.Context) string {
	if cfg, exists := c.Get("cfg"); exists {
		return cfg.(*config.Config).StoragePath
	}
	return config.DefaultStoragePath
}

// getFreeDiskSpace returns the free space of the storage partition in bytes.
func getFreeDiskSpace(path string) float64 {
	var stat syscall.Statfs_t
	// Only attempt directory creation if it doesn't already exist
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0755); err != nil {
			return 0
		}
	}
	err := syscall.Statfs(path, &stat)
	if err != nil {
		return 0
	}
	// Available blocks * size per block
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	return float64(freeBytes)
}

// getCurrentUserID returns the authenticated user's ID from the gin context.
// These fields are expected to be set by auth middleware.
func getCurrentUserID(c *gin.Context) int {
	if v, exists := c.Get("user_id"); exists {
		if id, ok := v.(int); ok {
			return id
		}
	}
	return 0
}

// getCurrentUsername returns the authenticated user's username from context.
func getCurrentUsername(c *gin.Context) string {
	if v, exists := c.Get("username"); exists {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getCurrentName returns the authenticated user's name from context.
func getCurrentName(c *gin.Context) string {
	if v, exists := c.Get("name"); exists {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// getCurrentUserRole returns the role JSON string from context.
func getCurrentUserRole(c *gin.Context) string {
	if v, exists := c.Get("role"); exists {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return models.RoleGuru
}

// getCurrentUserRoles returns the parsed roles slice from context.
func getCurrentUserRoles(c *gin.Context) []string {
	if v, exists := c.Get("user_roles"); exists {
		if roles, ok := v.([]string); ok {
			return roles
		}
	}
	return []string{models.RoleGuru}
}

// hasCurrentRole checks if the authenticated user has a specific role.
func hasCurrentRole(c *gin.Context, target string) bool {
	for _, r := range getCurrentUserRoles(c) {
		if r == target {
			return true
		}
	}
	return false
}

// isSuperAdmin checks if the current user is the super admin.
func isSuperAdmin(c *gin.Context) bool {
	if v, exists := c.Get("is_super_admin"); exists {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// isOperator checks if the current user has the operator role.
func isOperator(c *gin.Context) bool {
	if v, exists := c.Get("is_operator"); exists {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Generic response helpers
// ---------------------------------------------------------------------------

func errorResponse(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"success": false, "message": message})
	_ = c.AbortWithError(status, fmt.Errorf("%s", message))
}

func successData(c *gin.Context, data gin.H) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func successMessage(c *gin.Context, message string) {
	c.JSON(http.StatusOK, gin.H{"success": true, "message": message})
}

// renderAdminPage renders a self-contained admin page template.
// Each template must be a full HTML document (not inheriting from base.html).
// Common template data (version, csrf, admin info, active_page) is injected automatically.
func renderAdminPage(c *gin.Context, pageTemplate string, data gin.H) {
	if data == nil {
		data = gin.H{}
	}
	cfg, _ := c.Get("cfg")
	if cfg != nil {
		data["version"] = cfg.(*config.Config).Version
	}
	data["csrf_token"] = middleware.GenerateCSRFToken(c)
	displayName := getCurrentUsername(c)
	if name := getCurrentName(c); name != "" {
		displayName = fmt.Sprintf("%s (%s)", name, displayName)
	}
	data["admin_user"] = displayName
	data["admin_role"] = getCurrentUserRole(c)
	data["admin_id"] = getCurrentUserID(c)

	pool := getPool(c)
	if pool != nil {
		userID := getCurrentUserID(c)
		if userID > 0 {
			var pkg, inst string
			err := pool.QueryRow(c.Request.Context(), `SELECT COALESCE(package, 'free'), COALESCE(instansi, '') FROM admin_users WHERE id = $1`, userID).Scan(&pkg, &inst)
			if err == nil {
				data["admin_package"] = pkg
				data["admin_instansi"] = inst
				instTrim := strings.TrimSpace(inst)
				isSuper := getCurrentUserRole(c) == models.RoleSuperAdmin
				if !isSuper && strings.HasPrefix(strings.ToLower(pkg), "sekolah") && (instTrim == "" || strings.ToLower(instTrim) == "personal" || instTrim == "Belum Ditetapkan") {
					data["needs_instansi"] = true
				}
			}
		}
	}

	c.HTML(http.StatusOK, pageTemplate, data)
}

// ---------------------------------------------------------------------------
// Time / number formatting
// ---------------------------------------------------------------------------

func formatISOUTC(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

func formatNullableISOUTC(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return formatISOUTC(*t)
}

func roundTo(val float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}

// ---------------------------------------------------------------------------
// Ownership check helpers
// ---------------------------------------------------------------------------

// checkExamOwnership returns true when the current user owns the exam or is
// super admin / operator with the same instansi.
func checkExamOwnership(ctx *gin.Context, pool *pgxpool.Pool, examID int) bool {
	userID := getCurrentUserID(ctx)
	if isSuperAdmin(ctx) {
		return true
	}

	var ownerID int
	var ownerInstansi string
	err := pool.QueryRow(ctx.Request.Context(),
		`SELECT e.created_by, COALESCE(u.instansi, '') FROM exams e
		 LEFT JOIN admin_users u ON e.created_by = u.id
		 WHERE e.id = $1`, examID).Scan(&ownerID, &ownerInstansi)
	if err != nil {
		return false
	}

	if ownerID == userID {
		return true
	}

	// Check delegated_to: delegated user controls the exam
	var delegatedTo *int
	_ = pool.QueryRow(ctx.Request.Context(),
		`SELECT delegated_to FROM exams WHERE id = $1`, examID).Scan(&delegatedTo)
	if delegatedTo != nil && *delegatedTo == userID {
		return true
	}

	if isOperator(ctx) {
		var userInstansi string
		pool.QueryRow(ctx.Request.Context(),
			`SELECT instansi FROM admin_users WHERE id = $1`, userID).Scan(&userInstansi)
		// An empty or "personal" (unset sentinel) instansi must never match
		// (consistent with UserCanAccessExam / FilterAccessibleExamIDs):
		// otherwise accounts in the shared default bucket would grant
		// cross-tenant access.
		return userInstansi != "" && userInstansi != "personal" && userInstansi == ownerInstansi
	}

	return false
}

// checkSubmissionOwnership returns true when the current user has access to the
// submission's exam.
func checkSubmissionOwnership(ctx *gin.Context, pool *pgxpool.Pool, submissionID int) bool {
	var examID int
	err := pool.QueryRow(ctx.Request.Context(),
		`SELECT exam_id FROM submissions WHERE id = $1`, submissionID).Scan(&examID)
	if err != nil {
		return false
	}
	return checkExamOwnership(ctx, pool, examID)
}
