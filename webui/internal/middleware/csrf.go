package middleware

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// CSRF token generation
// ---------------------------------------------------------------------------

const (
	csrfTokenLength = 32        // 32 bytes → 64 hex chars
	csrfSessionKey  = "_csrf_token"
)

// GenerateCSRFToken returns the existing CSRF token from the session, or
// creates a new random token, stores it in the session, and returns it.
// It is safe to call on every page render — the token is only generated once
// per session.
func GenerateCSRFToken(c *gin.Context) string {
	session := sessions.Default(c)
	if token, ok := session.Get(csrfSessionKey).(string); ok && token != "" {
		return token
	}

	token := randomHex(csrfTokenLength)
	session.Set(csrfSessionKey, token)
	if err := session.Save(); err != nil {
		// If saving fails (e.g. session store unavailable) we still return
		// a token so the form renders, but CSRF validation will reject the
		// subsequent POST. The caller may want to log this.
		return token
	}
	return token
}

// CSRFRequired validates the X-CSRF-Token header (or _csrf_token form field)
// against the token stored in the session. Requests using GET, HEAD, OPTIONS,
// and TRACE methods are always allowed through.
//
// Usage:
//
//	r := gin.Default()
//	r.Use(sessions.Sessions("examvan_session", store))
//	r.POST("/admin/api/...", middleware.CSRFRequired(), handler)
func CSRFRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only enforce for state-changing methods.
		if c.Request.Method == http.MethodGet ||
			c.Request.Method == http.MethodHead ||
			c.Request.Method == http.MethodOptions ||
			c.Request.Method == http.MethodTrace {
			c.Next()
			return
		}

		session := sessions.Default(c)
		expected, ok := session.Get(csrfSessionKey).(string)
		if !ok || expected == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "CSRF token tidak ditemukan. Silakan refresh halaman.",
			})
			return
		}

		// Check header first, then fall back to form field.
		provided := c.GetHeader("X-CSRF-Token")
		if provided == "" {
			provided = c.GetHeader("X-Csrf-Token")
		}
		if provided == "" {
			provided = c.PostForm("_csrf_token")
		}

		if provided != expected {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "CSRF token tidak valid. Silakan refresh halaman.",
			})
			return
		}

		c.Next()
	}
}

// randomHex generates a hex-encoded random string of n bytes.
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read only returns an error when the system's entropy
		// pool is depleted — extremely unlikely. Fall back to a zero string
		// (which will fail validation and force a retry).
		return ""
	}
	return hex.EncodeToString(b)
}
