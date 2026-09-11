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
	// SessionKeyIssuedAt records when the session was established (Unix
	// milliseconds, for millisecond-granular comparison against
	// password_changed_at). It is the client-side half of the password-change
	// revocation (M2): AuthRequired compares it against the admin_users row's
	// password_changed_at and ends every session issued BEFORE the change —
	// sessions predating the column have no value and are treated as issued
	// at time zero (i.e. revoked by any recorded change).
	SessionKeyIssuedAt = "issued_at"
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

	// Backslashes are never legitimate inside an in-app path, but browsers
	// normalize them to forward slashes when resolving the Location header —
	// so "/\evil.com" would be read as "//evil.com" and leave the site as an
	// open redirect. The URL-encoded form (%5C) is rejected for the same
	// reason (it decodes to a backslash).
	if strings.Contains(raw, "\\") || strings.Contains(strings.ToUpper(raw), `%5C`) {
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

		// Identity values start from the session — the fast path — and are
		// then REVALIDATED against the admin_users row when a DB pool is
		// available (production always provides one; the pure-HTTP unit tests
		// exercise the session-only fallback).
		username, _ := session.Get(SessionKeyUsername).(string)
		nameVal := session.Get(SessionKeyName)
		name, _ := nameVal.(string)
		role, _ := session.Get(SessionKeyRole).(string)
		isSuper, _ := session.Get(SessionKeyIsSuper).(bool)
		instansi, _ := session.Get(SessionKeyInstansi).(string)

		// Per-request status AND authority enforcement: a suspended (or
		// unverified) account must lose access immediately — the cookie may
		// still be valid, but the account's status revokes its authority.
		// Without this, an admin suspension would only take effect after the
		// 1-day session expired (or could even be reversed by the user via
		// voucher redeem/activate). The same query revalidates role /
		// superadmin / instansi from the DB row, so a DEMOTED or reassigned
		// account loses its old powers on the very next request — a stale
		// cookie alone must never keep operator powers after the account was
		// downgraded in the DB. When the DB values differ from the session's
		// copy, the session is self-healed (role normalized exactly like the
		// login handler, so the template guards and downstream middlewares
		// always see the canonical value).
		if pool, exists := c.Get("db"); exists && pool != nil {
			dbPool := pool.(*pgxpool.Pool)
			var dbStatus string
			var dbExpiresAt *time.Time
			var dbRole, dbInstansi, dbUsername, dbName string
			var dbPasswordChangedAt *time.Time
			if err := dbPool.QueryRow(c.Request.Context(),
				`SELECT status, expires_at, COALESCE(role, ''), COALESCE(instansi, ''), COALESCE(username, ''), COALESCE(name, ''), password_changed_at
				 FROM admin_users WHERE id = $1`, id).Scan(&dbStatus, &dbExpiresAt, &dbRole, &dbInstansi, &dbUsername, &dbName, &dbPasswordChangedAt); err != nil {
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

			// Password-change revocation (M2): a successful password change or
			// reset stamps password_changed_at on the row, and every session
			// issued BEFORE that moment dies here — with cookie-only sessions
			// this is the ONLY way to evict a stolen cookie before its 24h
			// MaxAge runs out. Legacy sessions without issued_at are treated as
			// issued at time zero, so a recorded change revokes them too; the
			// legitimate owner simply logs in again and gets a fresh stamped
			// session. An absent password_changed_at revokes nothing.
			if dbPasswordChangedAt != nil {
				issuedAt := int64(0)
				if v, ok := session.Get(SessionKeyIssuedAt).(int64); ok {
					issuedAt = v
				}
				if issuedAt < dbPasswordChangedAt.UnixMilli() {
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

			// Authority revalidation: the DB row is authoritative. isSuper is
			// derived from the role exactly like the login handler, and the
			// session role is normalized the same way so the two never
			// disagree on the format.
			dbIsSuper := models.HasRole(dbRole, models.RoleSuperAdmin)
			dbRoleNorm := models.NormalizeSessionRole(dbRole)
			if dbIsSuper {
				dbRoleNorm = models.RoleSuperAdmin
			}
			heal := false
			if cur, _ := session.Get(SessionKeyRole).(string); cur != dbRoleNorm {
				session.Set(SessionKeyRole, dbRoleNorm)
				heal = true
			}
			if cur, _ := session.Get(SessionKeyIsSuper).(bool); cur != dbIsSuper {
				session.Set(SessionKeyIsSuper, dbIsSuper)
				heal = true
			}
			if cur, _ := session.Get(SessionKeyInstansi).(string); cur != dbInstansi {
				session.Set(SessionKeyInstansi, dbInstansi)
				heal = true
			}
			if cur, _ := session.Get(SessionKeyUsername).(string); cur != dbUsername {
				session.Set(SessionKeyUsername, dbUsername)
				heal = true
			}
			if cur, _ := session.Get(SessionKeyName).(string); cur != dbName {
				session.Set(SessionKeyName, dbName)
				heal = true
			}
			if heal {
				_ = session.Save()
			}
			username, role, isSuper, instansi, name = dbUsername, dbRoleNorm, dbIsSuper, dbInstansi, dbName

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

		isOperator := models.HasRole(role, models.RoleOperator)

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

// FeatureLockRequired gates every admin page/API except the settings hub. It
// must be used after AuthRequired, which sets ContextKeyLocked when the
// account's active period has passed (an expired owner keeps their session so
// they can renew). A locked account is redirected to the settings hub's
// Paket & Voucher tab — /admin/settings#billing — which replaced the old
// /admin/billing renewal page (HTML) or gets a 403 JSON (API) — the same
// shape as the suspended-account handling, but pointing the owner at renewal
// instead of blocking them out entirely. Accounts that are not
// feature-locked (valid, suspended — suspended is rejected earlier by
// AuthRequired — or SuperAdmin) pass through untouched.
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
				c.Redirect(http.StatusFound, "/admin/settings#billing")
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
