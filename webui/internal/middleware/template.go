package middleware

import (
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/config"
)

// ---------------------------------------------------------------------------
// Template data builder
// ---------------------------------------------------------------------------

// TemplateData returns a gin.H map of common template variables that should
// be merged into every HTML page render. Call it inside your route handler
// and merge the result into the data you pass to c.HTML():
//
//	func DashboardPage() gin.HandlerFunc {
//	    return func(c *gin.Context) {
//	        data := middleware.TemplateData(c)
//	        data["title"] = "Dashboard"
//	        data["exams"] = exams
//	        c.HTML(http.StatusOK, "dashboard.tmpl", data)
//	    }
//	}
//
// The returned map always contains:
//   - csrf_token — the session-bound CSRF token string
//   - version   — app version (from config.DefaultVersion)
//
// When the user is authenticated (session has admin_id) it also includes:
//   - admin_user — gin.H{id, username, role, is_super_admin}
//   - instansi   — the user's instansi (if set)
//   - flashes    — any unread session flash messages (cleared after read)
func TemplateData(c *gin.Context) gin.H {
	data := gin.H{
		"csrf_token": GenerateCSRFToken(c),
		"version":    config.DefaultVersion,
	}

	// Attach authenticated user info when available.
	session := sessions.Default(c)
	if adminID := session.Get(SessionKeyAdminID); adminID != nil {
		username, _ := session.Get(SessionKeyUsername).(string)
		role, _ := session.Get(SessionKeyRole).(string)
		isSuper, _ := session.Get(SessionKeyIsSuper).(bool)
		instansi, _ := session.Get(SessionKeyInstansi).(string)

		id, _ := toInt(adminID)

		data["admin_user"] = gin.H{
			"id":             id,
			"username":       username,
			"role":           role,
			"is_super_admin": isSuper,
		}
		if instansi != "" {
			data["instansi"] = instansi
		}

		// Read flash messages (cleared by the session store on read).
		if flashes := session.Flashes(); len(flashes) > 0 {
			data["flashes"] = flashes
			_ = session.Save()
		}
	}

	// Attach the full config object if one was placed in context.
	if cfg, exists := c.Get("cfg"); exists {
		if cObj, ok := cfg.(*config.Config); ok {
			data["cfg"] = cObj
		}
	}

	return data
}

// MergeTemplateData is a convenience wrapper that merges the output of
// TemplateData into an existing gin.H map and returns it.
//
//	func MyPage() gin.HandlerFunc {
//	    return func(c *gin.Context) {
//	        data := gin.H{"title": "My Page", "items": items}
//	        data = middleware.MergeTemplateData(c, data)
//	        c.HTML(http.StatusOK, "page.tmpl", data)
//	    }
//	}
func MergeTemplateData(c *gin.Context, existing gin.H) gin.H {
	base := TemplateData(c)
	for k, v := range existing {
		base[k] = v
	}
	return base
}
