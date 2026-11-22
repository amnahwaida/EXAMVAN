// Package admin provides Gin handler functions for EXAMVAN admin panel routes
// (dashboard, exam CRUD, user management, submissions, pengawas, and settings).
package admin

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/sessions"
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
// When the exact storage path does not exist and cannot be created (e.g. the
// default /app/storage is a Docker-only path and a bare dev machine cannot
// mkdir /app as non-root), it falls back to the nearest existing ancestor:
// statfs reports the same partition free space for any path on it, which is
// the realistic development value instead of 0. Production is unaffected —
// the configured storage dir exists there, so statfs hits the exact path. The
// fallback is triggered ONLY by ENOENT (path missing): any other statfs error
// still returns 0 so the caller's fail-open handling (quota checks skipped)
// stays intact instead of borrowing another partition's free space.
func getFreeDiskSpace(path string) float64 {
	// Best effort: ensure the storage dir exists (prod can create it). A
	// failure here is not fatal — the ENOENT fallback below covers it.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.MkdirAll(path, 0755)
	}

	var stat syscall.Statfs_t
	err := syscall.Statfs(path, &stat)
	if err == nil {
		// Available blocks * size per block
		return float64(stat.Bavail * uint64(stat.Bsize))
	}
	if !errors.Is(err, syscall.ENOENT) {
		// Real statfs error (EACCES on a mount, I/O error, ...) — do not
		// report an ancestor partition's free space as if it were the
		// storage partition's. Return 0; callers fail open.
		return 0
	}

	// ENOENT: storage path missing and uncreatable — walk up to the nearest
	// ancestor statfs accepts (always terminates at the filesystem root).
	// Same-partition free space, so the dev value is realistic.
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		if err := syscall.Statfs(p, &stat); err == nil {
			return float64(stat.Bavail * uint64(stat.Bsize))
		}
		next := filepath.Dir(p)
		if next == p {
			return 0 // no readable ancestor — genuinely undeterminable
		}
	}
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

// errorResponseWithCode is like errorResponse but also exposes a stable
// machine-readable error_code (e.g. r2client.ErrCodeNotConfigured) so clients
// can branch on the code instead of matching the human-readable message text.
func errorResponseWithCode(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"success": false, "error_code": code, "message": message})
	_ = c.AbortWithError(status, fmt.Errorf("%s [%s]", message, code))
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

	// Consume any pending session flash (e.g. the feature-lock notice left by
	// FeatureLockRequired when a locked account was redirected here) so the
	// template can surface it. Admin pages don't go through TemplateData, which
	// is what normally reads flashes for the public/login pages.
	if flashes := sessions.Default(c).Flashes(); len(flashes) > 0 {
		data["flashes"] = flashes
		_ = sessions.Default(c).Save()
	}

	pool := getPool(c)
	if pool != nil {
		userID := getCurrentUserID(c)
		if userID > 0 {
			var pkg, inst, role string
			err := pool.QueryRow(c.Request.Context(), `SELECT COALESCE(package, 'free'), COALESCE(instansi, ''), COALESCE(role, '') FROM admin_users WHERE id = $1`, userID).Scan(&pkg, &inst, &role)
			if err == nil {
				data["admin_package"] = pkg
				data["admin_instansi"] = inst
				// The mandatory-instansi onboarding modal is for OPERATORS
				// whose instansi is still the unset "personal" sentinel,
				// regardless of package (school or custom operator). Requiring
				// the operator role keeps the modal off accounts that could not
				// act on it anyway — the submit endpoint
				// (POST /admin/api/instansi/update) is AdminManagementRequired,
				// so a plain guru holding a SuperAdmin-assigned package label
				// would see a modal that always 403s. Read the DB role (not
				// just the session flag) so a package just granted by
				// redeem/activate cannot miss the modal for one stale-session
				// request. Feature-locked accounts are excluded: they can only
				// reach the billing tab, while the instansi update endpoint is
				// feature-gated — showing the modal there would deadlock them
				// until renewal. Once renewed, the modal appears normally.
				locked, _ := c.Get(middleware.ContextKeyLocked)
				featureLocked, _ := locked.(bool)
				if shouldShowInstansiOnboarding(isSuperAdmin(c), featureLocked, role, inst) {
					data["needs_instansi"] = true
				}
			}
		}
	}

	// Admin pages embed their JavaScript INLINE, so a cached copy keeps running
	// the previous script after a deploy — the supervisor keeps experiencing
	// bugs that were already fixed server-side (notably the "Memuat data..."
	// wedge). Never let browsers or proxies cache them. Matches the public
	// hasil page contract.
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	c.Header("Pragma", "no-cache")

	c.HTML(http.StatusOK, pageTemplate, data)
}

// needsInstansiOnboarding reports whether an operator still carries the shared
// "personal" sentinel as its instansi. This is the single source of truth for
// both the mandatory onboarding modal (renderAdminPage) and the
// redeem/activate success message, so the two can never disagree. The package
// name is deliberately irrelevant: a custom operator package needs a real
// instansi exactly like a school package does.
func needsInstansiOnboarding(role, instansi string) bool {
	if !models.HasRole(role, models.RoleOperator) {
		return false
	}
	inst := strings.TrimSpace(instansi)
	return inst == "" || strings.EqualFold(inst, "personal") || inst == "Belum Ditetapkan"
}

// shouldShowInstansiOnboarding adds the page-level exclusions to the core
// predicate: SuperAdmin never needs onboarding, and a feature-locked account
// must not see the mandatory modal because the instansi update endpoint is
// feature-gated. Such an account can only reach the billing tab; after a
// successful renewal it becomes unlocked and the modal appears on reload.
func shouldShowInstansiOnboarding(isSuper, featureLocked bool, role, instansi string) bool {
	if isSuper || featureLocked {
		return false
	}
	return needsInstansiOnboarding(role, instansi)
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

// formatOptionalISOUTC renders an optional timestamp as an ISO string, or ""
// when absent — for JSON fields the UI reads with a `x || fallback` check,
// where a null/absent value must not stringify into something truthy.
func formatOptionalISOUTC(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
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

// checkExamOwnership returns true when the current user may manage the exam:
// SuperAdmin, the owner, a delegate, or an operator in the exam creator's
// (non-empty) tenant. A pengawas-only assignment does NOT grant management
// rights — the same contract as models.UserCanControlExam, which this helper
// now delegates to so single-exam management authorization matches the batch
// path (FilterAccessibleExamIDs) exactly.
//
// Previously this function re-implemented the operator branch with a raw
// byte-for-byte `instansi` comparison (case-sensitive, untrimmed, no
// instansi_id), which disagreed with the list/export/pengawas gates that
// match tenants via InstansiMatchSelfSQL (canonical instansi_id +
// case-insensitive name fallback). Delegating removes that divergence: an
// operator is treated as same-tenant here by the same rule everywhere else.
func checkExamOwnership(ctx *gin.Context, pool *pgxpool.Pool, examID int) bool {
	return models.UserCanControlExam(
		ctx.Request.Context(), pool, getCurrentUserID(ctx), isSuperAdmin(ctx), examID)
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
