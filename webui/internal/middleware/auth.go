// Package middleware provides reusable Gin HTTP middleware for EXAMVAN webui.
//
// This file implements authentication and authorization middleware using
// gin-contrib/sessions for session-backed auth.
package middleware

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/models"
)

// ---------------------------------------------------------------------------
// Session key constants — these must match what the login handler stores.
// ---------------------------------------------------------------------------

const (
	SessionKeyAdminID  = "admin_id"
	SessionKeyUsername = "username"
	SessionKeyName     = "name"
	SessionKeyRole     = "role"
	SessionKeyIsSuper  = "is_super_admin"
	SessionKeyInstansi = "instansi"
)

// ---------------------------------------------------------------------------
// Context key constants — values written into gin.Context for handlers.
// ---------------------------------------------------------------------------

const (
	ContextKeyUserID     = "user_id"
	ContextKeyUsername   = "username"
	ContextKeyName       = "name"
	ContextKeyRole       = "role"
	ContextKeyIsSuper    = "is_super_admin"
	ContextKeyIsOperator = "is_operator"
	ContextKeyUserRoles  = "user_roles"
	ContextKeyInstansi   = "instansi"
	ContextKeyLocked     = "feature_locked"
)

// SafeRedirectPath normalizes an in-app redirect target.
// It only accepts relative paths within the site and strips malformed values.
func SafeRedirectPath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.IsAbs() || u.Scheme != "" || u.Host != "" {
		return ""
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return ""
	}
	if strings.HasPrefix(u.Path, "/login") || strings.HasPrefix(u.Path, "/admin/login") {
		return ""
	}
	return u.RequestURI()
}

// LoginURLWithNext builds the login URL with an optional safe next target.
func LoginURLWithNext(next string) string {
	safeNext := SafeRedirectPath(next)
	if safeNext == "" {
		return "/login"
	}
	return "/login?next=" + url.QueryEscape(safeNext)
}

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
				c.Redirect(http.StatusFound, LoginURLWithNext(c.Request.URL.RequestURI()))
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

		// isSuper is read from the session early so the expiry block below can
		// exempt SuperAdmin from the feature lock (its expiry may be absent or
		// stale and must not gate the platform owner).
		isSuper, _ := session.Get(SessionKeyIsSuper).(bool)

		// Per-request status enforcement: a suspended (or unverified) account
		// must lose access immediately — the cookie may still be valid, but the
		// account's status revokes its authority. Without this, an admin
		// suspension would only take effect after the 1-day session expired
		// (or could even be reversed by the user via voucher redeem/activate).
		if pool, exists := c.Get("db"); exists && pool != nil {
			dbPool := pool.(*pgxpool.Pool)
			var dbStatus string
			var dbExpiresAt *time.Time
			if err := dbPool.QueryRow(c.Request.Context(),
				`SELECT status, expires_at FROM admin_users WHERE id = $1`, id).Scan(&dbStatus, &dbExpiresAt); err != nil {
				// Account no longer exists — drop the stale session.
				session.Clear()
				_ = session.Save()
				if isAPIRequest(c) {
					c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
						"success": false,
						"message": "Sesi telah berakhir. Silakan login kembali.",
					})
				} else {
					c.Redirect(http.StatusFound, LoginURLWithNext(c.Request.URL.RequestURI()))
				}
				c.Abort()
				return
			}
			if dbStatus == models.UserStatusSuspended || dbStatus == models.UserStatusPendingOTP {
				session.Clear()
				_ = session.Save()
				msg := "Akun Anda telah dinonaktifkan oleh administrator."
				if dbStatus == models.UserStatusPendingOTP {
					msg = "Pendaftaran Anda membutuhkan konfirmasi OTP Email. Silakan cek email Anda."
				}
				if isAPIRequest(c) {
					c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": msg})
				} else {
					c.Redirect(http.StatusFound, "/login")
				}
				c.Abort()
				return
			}
			// Per-request expiry enforcement: an account whose active period has
			// passed keeps its session (so the owner can renew on the billing
			// page) but is marked feature-locked. The FeatureLockRequired
			// middleware then blocks every admin page/API except billing until
			// the account's expiry is extended. The session is deliberately NOT
			// cleared — unlike a suspended account, an expired owner is still
			// entitled to reach the package/voucher page and redeem a voucher.
			// SuperAdmin is never feature-locked (its expiry may be absent or
			// stale and must not gate the platform owner).
			if dbExpiresAt != nil && dbExpiresAt.Before(time.Now().UTC()) && !isSuper {
				c.Set(ContextKeyLocked, true)
			}
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

// FeatureLockedMessage is the message shown to (and stored as a flash for) an
// account whose active period has run out when it tries to use a feature
// beyond the billing page.
const FeatureLockedMessage = "Masa aktif akun Anda telah habis. Silakan perpanjang masa aktif melalui halaman Paket & Voucher untuk melanjutkan penggunaan."

// FeatureLockRequired gates every admin page/API except billing. It must be
// used after AuthRequired, which sets ContextKeyLocked when the account's
// active period has passed (an expired owner keeps their session so they can
// renew). A locked account is redirected to the billing page (HTML) or gets a
// 403 JSON (API) — the same shape as the suspended-account handling, but
// pointing the owner at renewal instead of blocking them out entirely.
// Accounts that are not feature-locked (valid, suspended — suspended is
// rejected earlier by AuthRequired — or SuperAdmin) pass through untouched.
func FeatureLockRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		locked, _ := c.Get(ContextKeyLocked)
		if b, ok := locked.(bool); ok && b {
			msg := FeatureLockedMessage
			if isAPIRequest(c) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": msg})
			} else {
				session := sessions.Default(c)
				session.AddFlash(msg)
				_ = session.Save()
				c.Redirect(http.StatusFound, "/admin/billing")
			}
			c.Abort()
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
