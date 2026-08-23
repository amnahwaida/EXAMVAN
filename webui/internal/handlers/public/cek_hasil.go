package public

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/examvan/webui/internal/middleware"
)

// tokenChars guards the token before it is used to build the redirect URL.
// Exam tokens are 8 alphanumeric characters (see config.TokenLength); 1-32 is
// accepted here so the /hasil/:token page stays the single place that decides
// "not found" for anything else. Anything outside [A-Za-z0-9] never reaches
// the redirect path.
var tokenChars = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)

// ---------------------------------------------------------------------------
// GET /hasil — Public "Cek Hasil Ujian" entry page (token form)
// ---------------------------------------------------------------------------

// CekHasilPage renders the public token form. With ?token= filled it
// redirects straight to /hasil/<token>, so the form also works without
// JavaScript (plain GET submit).
func CekHasilPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Same anti-caching posture as HasilPage; the template also carries
		// a <meta name=robots> for defense in depth.
		c.Header("X-Robots-Tag", "noindex, nofollow")
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")

		token := strings.ToUpper(strings.TrimSpace(c.Query("token")))
		if token != "" {
			if tokenChars.MatchString(token) {
				c.Redirect(http.StatusFound, "/hasil/"+token)
				return
			}
			c.HTML(http.StatusBadRequest, "public/cek_hasil.html", middleware.MergeTemplateData(c, gin.H{
				"error": "Format token tidak valid. Token ujian terdiri dari 8 huruf/angka (contoh: AB12CD34).",
			}))
			return
		}

		c.HTML(http.StatusOK, "public/cek_hasil.html", middleware.MergeTemplateData(c, gin.H{}))
	}
}
