// Package middleware provides reusable Gin HTTP middleware for EXAMVAN webui.
//
// This file implements authentication and authorization middleware using
// gin-contrib/sessions for session-backed auth.
package middleware

import (
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Session key constants — these must match what the login handler stores.
// ---------------------------------------------------------------------------

const (
	SessionKeyAdminID     = "admin_id"
	SessionKeyUsername    = "username"
	SessionKeyName        = "name"
	SessionKeyRole        = "role"
	SessionKeyIsSuper     = "is_super_admin"
	SessionKeyInstansi    = "instansi"
)

// ---------------------------------------------------------------------------
// Context key constants — values written into gin.Context for handlers.
// ---------------------------------------------------------------------------

const (
	ContextKeyUserID      = "user_id"
	ContextKeyUsername    = "username"
	ContextKeyName        = "name"
	ContextKeyRole        = "role"
	ContextKeyIsSuper     = "is_super_admin"
	ContextKeyIsOperator  = "is_operator"
	ContextKeyUserRoles   = "user_roles"
	ContextKeyInstansi    = "instansi"
)

// AuthRequired ensures the request has a valid session containing admin_id.
// It sets user info in the gin context for downstream handlers.
//
// On failure it redirects to /login for HTML requests or returns a 401 JSON
// response for API / AJAX requests.
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		adminID := session.Get(SessionKeyAdminID)
		if adminID == nil {
			if isAPIRequest(c) {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": "Sesi telah berakhir. Silakan login kembali.",
				})
			} else {
				c.Redirect(http.StatusFound, "/login")
			}
			c.Abort()
			return
		}

		id, ok := toInt(adminID)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "Sesi tidak valid.",
			})
			c.Abort()
			return
		}

		username, _ := session.Get(SessionKeyUsername).(string)
		nameVal := session.Get(SessionKeyName)
		var name string
		if nameVal == nil {
			// Query the database for the name to populate existing sessions
			pool, exists := c.Get("db")
			if exists && pool != nil {
				dbPool := pool.(*pgxpool.Pool)
				var dbName string
				err := dbPool.QueryRow(c.Request.Context(), `SELECT name FROM admin_users WHERE id = $1`, id).Scan(&dbName)
				if err == nil {
					name = dbName
					session.Set(SessionKeyName, name)
					_ = session.Save()
				}
			}
		} else {
			name, _ = nameVal.(string)
		}
		role, _ := session.Get(SessionKeyRole).(string)
		isSuper, _ := session.Get(SessionKeyIsSuper).(bool)
		isOperator := models.HasRole(role, models.RoleOperator)
		instansi, _ := session.Get(SessionKeyInstansi).(string)

		c.Set(ContextKeyUserID, id)
		c.Set(ContextKeyUsername, username)
		c.Set(ContextKeyName, name)
		c.Set(ContextKeyRole, role)
		c.Set(ContextKeyIsSuper, isSuper)
		c.Set(ContextKeyIsOperator, isOperator)
		c.Set(ContextKeyUserRoles, models.ParseRoles(role))
		if instansi != "" {
			c.Set(ContextKeyInstansi, instansi)
		}

		c.Next()
	}
}

// SuperAdminRequired restricts access to super admin accounts only.
// It must be used after AuthRequired so that is_super_admin is available
// in the context.
func SuperAdminRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		isSuper, exists := c.Get(ContextKeyIsSuper)
		if !exists {
			abortForbidden(c, "Akses ditolak.")
			return
		}
		if b, ok := isSuper.(bool); !ok || !b {
			abortForbidden(c, "Akses ditolak. Halaman ini khusus Super Admin.")
			return
		}
		c.Next()
	}
}

// AdminManagementRequired restricts access to super admin or operator roles.
// It must be used after AuthRequired.
func AdminManagementRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Super admin always passes.
		if isSuper, exists := c.Get(ContextKeyIsSuper); exists {
			if b, ok := isSuper.(bool); ok && b {
				c.Next()
				return
			}
		}
		// Operator also passes.
		if isOp, exists := c.Get(ContextKeyIsOperator); exists {
			if b, ok := isOp.(bool); ok && b {
				c.Next()
				return
			}
		}
		abortForbidden(c, "Akses ditolak. Halaman ini khusus Super Admin atau Operator.")
	}
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// abortForbidden writes a 403 JSON response and aborts the request chain.
func abortForbidden(c *gin.Context, message string) {
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
		"success": false,
		"message": message,
	})
}

// isAPIRequest returns true when the request looks like an API or AJAX call
// based on the Accept header or the X-Requested-With header.
func isAPIRequest(c *gin.Context) bool {
	if c.GetHeader("Accept") == "application/json" {
		return true
	}
	if c.GetHeader("Content-Type") == "application/json" {
		return true
	}
	if c.GetHeader("X-Requested-With") == "XMLHttpRequest" {
		return true
	}
	return false
}

// toInt attempts to convert an interface{} value to an int, handling common
// numeric types that may come from different session backends (int, int64,
// float64).
func toInt(v interface{}) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(val), true
	default:
		return 0, false
	}
}
