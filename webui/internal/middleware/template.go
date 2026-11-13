package middleware

import (
	"context"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

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
	version := config.DefaultVersion
	if cfg, exists := c.Get("cfg"); exists {
		if cObj, ok := cfg.(*config.Config); ok && cObj.Version != "" {
			version = cObj.Version
		}
	}
	data := gin.H{
		"csrf_token": GenerateCSRFToken(c),
		"version":    version,
	}

	// Fetch database pool and read SEO settings — SATU query untuk semua kunci
	// (review 14 Nov 2026): dulu enam QueryRow berurutan per render halaman.
	if dbVal, exists := c.Get("db"); exists && dbVal != nil {
		if pool, ok := dbVal.(*pgxpool.Pool); ok {
			for k, v := range loadSeoFooterSettings(c.Request.Context(), pool) {
				data[k] = v
			}
		} else {
			for k, v := range defaultSeoFooterSettings() {
				data[k] = v
			}
		}
	} else {
		for k, v := range defaultSeoFooterSettings() {
			data[k] = v
		}
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

// ---------------------------------------------------------------------------
// SEO/footer settings — SATU query untuk seluruh halaman (guard:
// TestTemplateDataSingleSettingsQuery)
// ---------------------------------------------------------------------------

// seoFooterKeys adalah kunci saas_settings yang dikonsumsi TemplateData.
// Kunci baru wajib ditambahkan DI SINI (bukan dengan SELECT per-kunci sendiri)
// agar tetap satu query per render.
var seoFooterKeys = []string{
	"seo_title", "seo_description", "seo_keywords", "seo_index",
	"footer_text", "footer_tagline",
}

// defaultSeoFooterSettings returns the built-in values used when the DB is
// unavailable or a key has no row.
func defaultSeoFooterSettings() gin.H {
	return gin.H{
		"seo_title":       "EXAMVAN - Aplikasi Ujian Online Aman & Tertib",
		"seo_description": "EXAMVAN adalah aplikasi ujian online mandiri dengan sistem keamanan tinggi terhindar dari kecurangan.",
		"seo_keywords":    "examvan, ujian online, ujian sekolah",
		"seo_index":       true,
		"footer_text":     "© 2026 EXAMVAN Team. All rights reserved.",
		"footer_tagline":  "Dibuat khusus untuk pengujian sekolah digital mandiri yang aman.",
	}
}

// loadSeoFooterSettings reads every SEO/footer key in ONE round trip and
// overlays the defaults with whatever rows exist. Empty/missing values fall
// back to the defaults; seo_index is normalised to a bool.
func loadSeoFooterSettings(ctx context.Context, pool *pgxpool.Pool) gin.H {
	out := gin.H{}
	for k, v := range defaultSeoFooterSettings() {
		out[k] = v
	}

	rows, err := pool.Query(ctx,
		`SELECT key, value FROM saas_settings WHERE key = ANY($1)`, seoFooterKeys)
	if err != nil {
		return out // DB error → defaults, halaman tetap render
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		values[k] = v
	}
	if v := values["seo_title"]; v != "" {
		out["seo_title"] = v
	}
	if v := values["seo_description"]; v != "" {
		out["seo_description"] = v
	}
	if v := values["seo_keywords"]; v != "" {
		out["seo_keywords"] = v
	}
	if v := values["seo_index"]; v != "" {
		out["seo_index"] = (v == "1" || v == "true")
	}
	if v := values["footer_text"]; v != "" {
		out["footer_text"] = v
	}
	if v := values["footer_tagline"]; v != "" {
		out["footer_tagline"] = v
	}
	return out
}
