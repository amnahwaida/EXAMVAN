// Command server is the EXAMVAN WebUI server entry point.
//
// It initialises the PostgreSQL connection, optionally connects to Redis,
// starts the async submission queue worker, registers all HTTP routes and
// middleware on a Gin engine, and runs the HTTP server with graceful
// shutdown support.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/handlers/admin"
	"github.com/examvan/webui/internal/handlers/api"
	"github.com/examvan/webui/internal/handlers/public"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/queue"
	redisclient "github.com/examvan/webui/internal/redis"
	"github.com/examvan/webui/internal/websocket"
)

// version is set at build time via -ldflags. Defaults to config.DefaultVersion.
var version = ""

func main() {
	// -----------------------------------------------------------------------
	// 1. Load config
	// -----------------------------------------------------------------------
	cfg := config.Load()
	if version != "" {
		cfg.Version = version
	}

	log.Printf("EXAMVAN WebUI v%s starting...", cfg.Version)
	log.Printf("Storage path: %s", cfg.StoragePath)
	log.Printf("Admin user: %s", cfg.AdminUser)

	if cfg.SecretKey == "" {
		log.Fatal("EXAMVAN_SECRET is required — set it to a random string of at least 32 characters")
	}

	// -----------------------------------------------------------------------
	// 2. Connect to PostgreSQL
	// -----------------------------------------------------------------------
	var pool *pgxpool.Pool
	if cfg.DatabaseURL == "" {
		log.Println("WARNING: DATABASE_URL not set — server will run without a database connection.")
		log.Println("         Most features will be unavailable. Set DATABASE_URL in .env or environment.")
	} else {
		p, err := database.Connect(cfg)
		if err != nil {
			log.Fatalf("Failed to connect to PostgreSQL: %v", err)
		}
		pool = p
		log.Println("PostgreSQL: connected and schema applied")
	}

	// -----------------------------------------------------------------------
	// 3. Connect to Redis (optional)
	// -----------------------------------------------------------------------
	ctx := context.Background()

	// Ensure admin user exists and migrate werkzeug password hashes to bcrypt
	if pool != nil {
		if err := models.EnsureAdminUser(ctx, pool, cfg.AdminUser, cfg.AdminPass); err != nil {
			log.Printf("WARNING: admin user creation failed: %v", err)
		}
		if cfg.AdminPass != "" {
			if err := models.MigrateAdminPassword(ctx, pool, cfg.AdminUser, cfg.AdminPass); err != nil {
				log.Printf("WARNING: admin password migration failed: %v", err)
			}
		}
	}
	rdb, err := redisclient.Connect(ctx, cfg.RedisURL)
	if err != nil {
		log.Printf("WARNING: Redis connection failed (%v) — running without Redis.", err)
		log.Println("         Submission queue, caching, and distributed rate limiting will be disabled.")
		rdb = nil
	}
	if cfg.RedisURL == "" {
		log.Println("Redis: not configured (REDIS_URL is empty) — optional features disabled")
	} else if rdb != nil {
		log.Println("Redis: connected")
	}

	// Share Redis client with rate-limit middleware.
	middleware.SetRedisClient(rdb)

	// -----------------------------------------------------------------------
	// 4. Start submission queue worker (only when both DB and Redis ready)
	// -----------------------------------------------------------------------
	if pool != nil && rdb != nil {
		go queue.StartWorker(rdb, pool)
		log.Println("Submission queue worker: started")
	} else {
		log.Println("Submission queue worker: not started (requires both PostgreSQL and Redis)")
	}

	// -----------------------------------------------------------------------
	// 5. Create Gin engine
	// -----------------------------------------------------------------------
	gin.SetMode(gin.DebugMode)

	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// -----------------------------------------------------------------------
	// 6. Session store (cookie-based)
	// -----------------------------------------------------------------------
	store := cookie.NewStore([]byte(cfg.SecretKey))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
	})
	r.Use(sessions.Sessions("examvan_session", store))

	// -----------------------------------------------------------------------
	// 7. Load HTML templates with FuncMap
	// -----------------------------------------------------------------------
	funcMap := template.FuncMap{
		"localizeUTC": func(utcStr string, offsetMinutes ...int) string {
			offset := 0
			if len(offsetMinutes) > 0 {
				offset = offsetMinutes[0]
			}
			return localizeUTCString(utcStr, offset)
		},
		"add":       func(a, b int) int { return a + b },
		"sub":       func(a, b int) int { return a - b },
		"mul":       func(a, b int) int { return a * b },
		"div":       func(a, b int) int { return a / b },
		"seq":       func(n int) []int { s := make([]int, n); for i := range s { s[i] = i }; return s },
		"dict":      func(values ...interface{}) map[string]interface{} { return toMap(values...) },
		"safe":      func(s string) template.HTML { return template.HTML(s) },
		"safeURL":   func(s string) template.URL { return template.URL(s) },
		"hasPrefix": strings.HasPrefix,
		"hasSuffix": strings.HasSuffix,
		"contains":  strings.Contains,
		"toUpper":   strings.ToUpper,
		"toLower":   strings.ToLower,
		"trimSpace": strings.TrimSpace,
		"default":   func(d, v interface{}) interface{} { return defaultVal(d, v) },
		"atoi":      func(s string) int { i, _ := strconv.Atoi(s); return i },
		"string":    func(v interface{}) string { return fmt.Sprintf("%v", v) },
		"json":      func(v interface{}) string { b, _ := json.Marshal(v); return string(b) },
		"percentOf": func(p, t float64) float64 { return percentOf(p, t) },
		"roundTo":   func(val float64, decimals int) float64 { return roundTo(val, decimals) },
		// Role helper functions
		"hasRole": func(roleStr string, role string) bool {
			if roleStr == "" {
				return false
			}
			// Check if role is in JSON array like ["guru","pengawas"]
			if strings.Contains(roleStr, role) {
				return true
			}
			return false
		},
		"displayRole": func(roleStr string) string {
			if roleStr == "" {
				return "Guru"
			}
			if strings.Contains(roleStr, "superadmin") {
				return "Super Admin"
			}
			if strings.Contains(roleStr, "operator") {
				return "Operator"
			}
			if strings.Contains(roleStr, "pengawas") {
				return "Pengawas"
			}
			return "Guru"
		},
		"formatExamTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02 15:04")
		},
		"substr": func(s string, start, end int) string {
			runes := []rune(s)
			if start >= len(runes) {
				return ""
			}
			if end > len(runes) {
				end = len(runes)
			}
			return string(runes[start:end])
		},
		// Layout helpers — return HTML fragments for self-contained pages
		"adminHead": func(version, csrfToken, title string) template.HTML {
			return template.HTML(fmt.Sprintf(
				`<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0"><title>EXAMVAN — %s</title><link rel="stylesheet" href="/static/css/theme.css?v=%s"><link rel="stylesheet" href="/static/css/tailwind/output.css?v=%s"><link rel="stylesheet" href="/static/css/admin-base.css?v=%s"><link rel="icon" type="image/png" href="/static/favicon.png"><meta name="csrf-token" content="%s">`,
				title, version, version, version, csrfToken))
		},
		"adminNav": func(activePage, adminRole, adminUser string) template.HTML {
			// Prevent XSS: escape user-controlled values
			adminUser = html.EscapeString(adminUser)
			adminRole = html.EscapeString(adminRole)
			isSuper := strings.Contains(adminRole, "superadmin")
			isOp := strings.Contains(adminRole, "operator")
			isGuru := strings.Contains(adminRole, "guru") || isSuper || isOp
			isPengawas := strings.Contains(adminRole, "pengawas") || isSuper
			guruLink := ""
			if isGuru {
				guruLink = fmt.Sprintf(`<a href="/admin/dashboard" class="nav-link %s"><svg class="icon-svg" aria-hidden="true"><use href="#hi-dashboard"/></svg> Daftar Ujian</a><a href="/admin/submissions" class="nav-link %s"><svg class="icon-svg" aria-hidden="true"><use href="#hi-results"/></svg> Hasil Ujian</a>`,
					activePageClass(activePage, "dashboard"), activePageClass(activePage, "submissions"))
			}
			pengawasLink := ""
			if isPengawas {
				pengawasLink = fmt.Sprintf(`<a href="/admin/pengawas" class="nav-link %s"><svg class="icon-svg" aria-hidden="true"><use href="#hi-eye"/></svg> Pengawasan</a>`, activePageClass(activePage, "pengawas"))
			}
			usersLink := ""
			if isSuper || isOp || strings.Contains(adminRole, "operator") {
				usersLink = fmt.Sprintf(`<a href="/admin/users" class="nav-link %s"><svg class="icon-svg" aria-hidden="true"><use href="#hi-users"/></svg> Kelola User</a>`, activePageClass(activePage, "users"))
			}
			roleDisplay := adminRole
			if isSuper {
				roleDisplay = "Super Admin"
			} else if isOp {
				roleDisplay = "Operator"
			}
			// Mobile nav links for hamburger menu
			mobileLinks := `<div class="dropdown-divider mobile-only-divider"></div><div class="mobile-nav-links">`
			if isGuru {
				mobileLinks += fmt.Sprintf(`<a href="/admin/dashboard" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-dashboard"/></svg> Daftar Ujian</a><a href="/admin/submissions" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-results"/></svg> Hasil Ujian</a>`, dropdownActive(activePage, "dashboard"), dropdownActive(activePage, "submissions"))
			}
			if isPengawas {
				mobileLinks += fmt.Sprintf(`<a href="/admin/pengawas" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-eye"/></svg> Pengawasan</a>`, dropdownActive(activePage, "pengawas"))
			}
			if isSuper || isOp {
				mobileLinks += fmt.Sprintf(`<a href="/admin/users" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-users"/></svg> Kelola User</a>`, dropdownActive(activePage, "users"))
			}
			mobileLinks += `</div><div class="dropdown-divider"></div>`
			return template.HTML(fmt.Sprintf(
				`<nav class="topbar"><div class="topbar-left"><div class="topbar-logo">E</div><a href="/" class="topbar-title" style="text-decoration:none;color:inherit;">EXAMVAN</a></div><div class="topbar-center"><div class="topbar-nav">%s%s%s</div></div><div class="topbar-right"><div class="topbar-menu-dropdown"><button class="topbar-menu-toggle" id="menuToggleBtn" onclick="event.stopPropagation();document.getElementById('menuDropdownContent').classList.toggle('show');"><span class="menu-hamburger-icon">&#9776;</span></button><div class="topbar-dropdown-content" id="menuDropdownContent"><div class="dropdown-header mobile-only-header"><div class="dropdown-brand-row"><div class="dropdown-logo">E</div><span class="dropdown-brand-title">EXAMVAN</span></div></div>%s<div class="dropdown-user-info"><span class="dropdown-user-name">%s</span><span class="dropdown-user-role">%s</span></div><div class="dropdown-divider"></div><button class="dropdown-item" onclick="openChangePasswordModal()"><svg class="icon-svg"><use href="#hi-key"/></svg> Ubah Password</button><div class="dropdown-divider"></div><a href="/admin/logout" class="dropdown-item dropdown-logout"><svg class="icon-svg" aria-hidden="true"><use href="#hi-logout"/></svg> Logout</a></div></div></div></nav>`,
				guruLink, pengawasLink, usersLink, mobileLinks, adminUser, roleDisplay))
		},
	}

	r.SetFuncMap(funcMap)

	templatesDir := filepath.Join("templates")
	tmpl := template.New("").Funcs(funcMap)
	filepath.Walk(templatesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".html" {
			return nil
		}
		rel, _ := filepath.Rel(templatesDir, path)
		name := filepath.ToSlash(rel)
		// Skip base.html — it's a reference file, not a renderable template.
		// Self-contained pages use {{adminHead}} and {{adminNav}} instead.
		if strings.HasSuffix(name, "/base.html") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("WARNING: cannot read template: %s: %v", path, err)
			return nil
		}
		if _, err := tmpl.New(name).Parse(string(data)); err != nil {
			log.Printf("WARNING: template parse error: %s: %v", path, err)
		}
		return nil
	})
	r.SetHTMLTemplate(tmpl)
	log.Println("Templates: loaded")

	// -----------------------------------------------------------------------
	// 8. Serve static files
	// -----------------------------------------------------------------------
	r.Static("/static", "./static")
	r.StaticFile("/favicon.ico", "./static/favicon.png")
	r.Static("/storage", cfg.StoragePath)

	// -----------------------------------------------------------------------
	// 9. Global middleware — inject DB connection, config, Redis, and WS hub
	// -----------------------------------------------------------------------
	// Inject config into every request context.
	r.Use(func(c *gin.Context) { c.Set("cfg", cfg); c.Next() })
	// Inject DB pool when available.
	if pool != nil {
		r.Use(middleware.DatabaseMiddleware(pool))
	}
	// Timeout middleware — wrap request context with deadline
	r.Use(middleware.TimeoutMiddleware(30 * time.Second))
	// Inject Redis client when available.
	if rdb != nil {
		r.Use(func(c *gin.Context) { c.Set("redis", rdb); c.Next() })
	}

	// -----------------------------------------------------------------------
	// 10. Register routes
	// -----------------------------------------------------------------------
	registerRoutes(r, cfg, pool)

	// -----------------------------------------------------------------------
	// 11. Start WebSocket hub
	// -----------------------------------------------------------------------
	hub := websocket.NewHub()
	go hub.Run()
	log.Println("WebSocket hub: started")

	// WebSocket endpoint.
	r.GET("/ws/:room_id", func(c *gin.Context) {
		roomID := c.Param("room_id")
		if err := hub.JoinRoom(c.Writer, c.Request, roomID); err != nil {
			log.Printf("websocket: join room %s error: %v", roomID, err)
		}
	})

	// Inject hub into context for handlers.
	r.Use(func(c *gin.Context) {
		c.Set("ws_hub", hub)
		c.Next()
	})

	// -----------------------------------------------------------------------
	// 12. Start HTTP server with graceful shutdown
	// -----------------------------------------------------------------------
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("HTTP server: listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for SIGINT or SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("Shutdown: received signal %v", sig)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Shutdown: HTTP server forced to close: %v", err)
	}

	// Close DB pool.
	if pool != nil {
		pool.Close()
		log.Println("Shutdown: database pool closed")
	}

	// Close Redis.
	if rdb != nil {
		if err := rdb.Close(); err != nil {
			log.Printf("Shutdown: Redis close error: %v", err)
		}
		log.Println("Shutdown: Redis connection closed")
	}

	log.Println("Shutdown: complete")
}

// ---------------------------------------------------------------------------
// Routes registration
// ---------------------------------------------------------------------------

func registerRoutes(r *gin.Engine, cfg *config.Config, pool *pgxpool.Pool) {
	// ---- Public pages (no auth required) ----
	r.GET("/", indexHandler(cfg))
	r.GET("/index.html", indexHandler(cfg))

	r.GET("/login", loginPageHandler(cfg))
	r.POST("/login", loginHandler(cfg))
	r.GET("/logout", logoutHandler())

	// Legacy: /admin/login → /login
	r.GET("/admin/login", func(c *gin.Context) { c.Redirect(http.StatusFound, "/login") })
	r.POST("/admin/login", loginHandler(cfg))

	r.GET("/register", registerPageHandler(cfg))
	r.POST("/register", registerPostHandler(cfg))

	r.GET("/download", public.DownloadPage())
	r.GET("/download/apk", public.DownloadAPK())
	r.GET("/hasil/:token", public.HasilPage())

	// ---- Short URL redirect: /<8-char-token> → /hasil/<token> ----
	// Must be registered after all other fixed routes so it acts as a catch-all
	// for unrecognized 8-character alphanumeric tokens.
	r.GET("/:token", shortURLRedirectHandler())

	// ---- Public API (no auth, rate-limited) ----
	// Android version check is applied to endpoints used by the Android app.
	apiGroup := r.Group("/api")
	{
		apiGroup.GET("/health", api.Health())
		apiGroup.GET("/time", api.ServerTime())

		apiGroup.GET("/exams", middleware.RateLimit(60, time.Minute), middleware.AndroidVersionCheck(), api.ListExams())
		apiGroup.GET("/exams/token/:token", middleware.RateLimit(30, time.Minute), middleware.AndroidVersionCheck(), api.ExamByToken())
		apiGroup.GET("/exams/:exam_id/pdf", middleware.RateLimit(30, time.Minute), middleware.AndroidVersionCheck(), api.ExamPDF())
		apiGroup.POST("/exams/:exam_id/submit", middleware.RateLimit(10, time.Minute), middleware.AndroidVersionCheck(), api.SubmitExam())
		apiGroup.POST("/exams/:exam_id/access-log", middleware.RateLimit(30, time.Minute), middleware.AndroidVersionCheck(), api.AccessLog())

		apiGroup.GET("/hasil/:token", middleware.RateLimit(30, time.Minute), public.HasilAPI())
	}

	// ---- Admin pages (auth required) ----
	adminPages := r.Group("/admin", middleware.AuthRequired())
	{
		adminPages.GET("/dashboard", admin.Dashboard())
		adminPages.GET("/dashboard/redirect", admin.DashboardRedirect())
		adminPages.GET("/submissions", admin.SubmissionsPage())

		adminPages.GET("/users", middleware.AdminManagementRequired(), admin.UsersPage())

		adminPages.GET("/pengawas", admin.PengawasPage())
		adminPages.GET("/pengawas/:exam_id", admin.PengawasDetailPage())

		// Logout (within admin group so it works with /admin/logout links).
		adminPages.GET("/logout", logoutHandler())
	}

	// ---- Admin API (auth required) ----
	adminAPI := r.Group("/admin/api",
		middleware.AuthRequired(),
	)
	{
		adminAPI.GET("/stats", admin.Stats())

		// ---- CSRF-protected routes (all POST) ----
		csrfAPI := adminAPI.Group("", middleware.CSRFRequired())
		{

			// Exams.
			csrfAPI.POST("/upload", middleware.RateLimit(10, time.Minute), admin.UploadExam())
			csrfAPI.POST("/exams/bulk-delete", admin.BulkDelete())
			csrfAPI.POST("/exams/bulk-toggle", admin.BulkToggle())
			csrfAPI.POST("/exams/:exam_id/toggle", admin.ToggleExam())
			csrfAPI.POST("/exams/:exam_id/delete", admin.DeleteExam())
			csrfAPI.POST("/exams/:exam_id/edit", admin.EditExam())
			csrfAPI.POST("/exams/:exam_id/questions", admin.SaveQuestions())
			csrfAPI.POST("/exams/:exam_id/regenerate-token", admin.RegenerateToken())
			csrfAPI.POST("/exams/:exam_id/edit-token", admin.EditToken())
			csrfAPI.POST("/exams/:exam_id/toggle-public-results", admin.TogglePublicResults())
			csrfAPI.POST("/exams/:exam_id/toggle-show-answers", admin.ToggleShowAnswers())
			csrfAPI.POST("/exams/:exam_id/delegate", admin.PostDelegateExam())

			// Submissions.
			csrfAPI.POST("/submissions/:id/delete", admin.DeleteSubmission())

			// Users (super admin / operator only).
			adminUsers := csrfAPI.Group("", middleware.AdminManagementRequired())
			{
				adminUsers.POST("/users", admin.CreateUser())
				adminUsers.POST("/users/:user_id/edit", admin.EditUser())
				adminUsers.POST("/users/:user_id/toggle-status", admin.ToggleUserStatus())
				adminUsers.POST("/users/:user_id/verify", admin.VerifyUser())
			}

			// SaaS settings (super admin only).
			adminSettings := csrfAPI.Group("", middleware.SuperAdminRequired())
			{
				adminSettings.POST("/saas-settings", admin.SaasSettings())
			}
		}

		// ---- Non-CSRF routes (GET / read-only) ----
		adminAPI.GET("/exams/:exam_id/questions", admin.GetQuestions())
		adminAPI.GET("/exams/:exam_id/delegate-data", admin.DelegateData())
		adminAPI.GET("/exams/:exam_id/pdf", admin.ExamPDF())
		adminAPI.GET("/submissions", admin.ListSubmissions())
		adminAPI.GET("/submissions/:id/detail", admin.SubmissionDetail())
		adminAPI.GET("/submissions/export", admin.ExportSubmissions())
		adminAPI.GET("/submissions/:id/export_detail", admin.ExportSubmissionDetail())
		adminAPI.GET("/queue/status", admin.QueueStatus())
		adminAPI.GET("/pengawas/exams", admin.PengawasExams())
		adminAPI.GET("/pengawas/exams/:exam_id/submissions", admin.PengawasExamSubmissions())
		adminAPI.GET("/saas-settings", admin.SaasSettings())
	}

		// ---- Legacy redirects ----
	r.GET("/admin", func(c *gin.Context) { c.Redirect(http.StatusFound, "/admin/dashboard") })
}

// shortURLRedirectHandler redirects /<8-char-token> to /hasil/<token>.
// Uses regex to only match 8-character alphanumeric tokens (A-Z, 0-9),
// so it won't catch legitimate paths like /login, /admin, /api, etc.
func shortURLRedirectHandler() gin.HandlerFunc {
	tokenRe := regexp.MustCompile(`^[A-Z0-9]{8}$`)
	return func(c *gin.Context) {
		token := strings.ToUpper(strings.TrimSpace(c.Param("token")))
		if !tokenRe.MatchString(token) {
			// Not a valid token — show 404 or redirect to index
			c.Redirect(http.StatusFound, "/")
			return
		}
		c.Redirect(http.StatusFound, "/hasil/"+token)
	}
}

// ---------------------------------------------------------------------------
// Inline handlers for pages without dedicated handler packages
// ---------------------------------------------------------------------------

func indexHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.HTML(http.StatusOK, "public/index.html", gin.H{
			"version": cfg.Version,
		})
	}
}

func loginPageHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["error"] = nil
		data["flashes"] = nil
		c.HTML(http.StatusOK, "admin/login.html", data)
	}
}

func loginHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.TrimSpace(c.PostForm("username"))
		password := c.PostForm("password")

		if username == "" || password == "" {
			data := middleware.TemplateData(c)
			data["error"] = "Username dan password wajib diisi."
			data["version"] = cfg.Version
			c.HTML(http.StatusOK, "admin/login.html", data)
			return
		}

		// When there is no database, authentication is not possible.
		pool, exists := c.Get("db")
		if !exists || pool == nil {
			data := middleware.TemplateData(c)
			data["error"] = "Database tidak tersedia. Silakan hubungi administrator."
			data["version"] = cfg.Version
			c.HTML(http.StatusOK, "admin/login.html", data)
			return
		}

		dbPool := pool.(*pgxpool.Pool)
		ctx := c.Request.Context()

		user, errMsg := models.AuthenticateUser(ctx, dbPool, username, password)
		if errMsg != "" {
			data := middleware.TemplateData(c)
			data["error"] = errMsg
			data["version"] = cfg.Version
			c.HTML(http.StatusOK, "admin/login.html", data)
			return
		}

		session := sessions.Default(c)
		session.Set(middleware.SessionKeyAdminID, user.ID)
		session.Set(middleware.SessionKeyUsername, user.Username)
		isSuper := user.Username == cfg.AdminUser || models.HasRole(user.Role, models.RoleSuperAdmin)
		isOperator := models.HasRole(user.Role, models.RoleOperator)
		var adminRole string
		if isSuper {
			adminRole = "superadmin"
		} else if isOperator {
			adminRole = "operator"
		} else {
			adminRole = user.Role
		}
		session.Set(middleware.SessionKeyRole, adminRole)
		session.Set(middleware.SessionKeyIsSuper, isSuper)
		session.Set(middleware.SessionKeyInstansi, user.Instansi)
		if err := session.Save(); err != nil {
			log.Printf("session save error: %v", err)
		}

		c.Redirect(http.StatusFound, "/admin/dashboard")
	}
}

func logoutHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		session.Clear()
		if err := session.Save(); err != nil {
			log.Printf("session save error on logout: %v", err)
		}
		c.Redirect(http.StatusFound, "/login")
	}
}

func registerPageHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["error"] = nil
		data["flashes"] = nil
		c.HTML(http.StatusOK, "public/register.html", data)
	}
}

func registerPostHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.TrimSpace(c.PostForm("username"))
		whatsapp := strings.TrimSpace(c.PostForm("whatsapp"))
		password := c.PostForm("password")

		data := middleware.TemplateData(c)
		data["version"] = cfg.Version

		// Validate input
		if username == "" || password == "" || whatsapp == "" {
			data["error"] = "Username, nomor WhatsApp, dan password wajib diisi."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}

		if len(username) < 3 {
			data["error"] = "Username minimal 3 karakter."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}

		if len(password) < 8 {
			data["error"] = "Password minimal 8 karakter."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}

		// Validate WhatsApp format
		cleanWA := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(whatsapp, "+", ""), "-", ""), " ", "")
		if !strings.HasPrefix(cleanWA, "08") && !strings.HasPrefix(cleanWA, "62") {
			data["error"] = "Format nomor WhatsApp tidak valid. Harus diawali 08 atau 62."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}
		if len(cleanWA) < 10 || len(cleanWA) > 15 {
			data["error"] = "Nomor WhatsApp harus 10-15 digit."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}

		pool, exists := c.Get("db")
		if !exists || pool == nil {
			data["error"] = "Database tidak tersedia. Silakan hubungi administrator."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}
		dbPool := pool.(*pgxpool.Pool)
		ctx := c.Request.Context()

		// Check if username is taken
		if username == cfg.AdminUser || username == "admin" || username == "superadmin" {
			data["error"] = "Username tidak tersedia."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}

		existing, err := models.GetUserByUsername(ctx, dbPool, username)
		if err == nil && existing.Username != "" {
			data["error"] = "Username sudah digunakan."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}

		// Create user with 'active' status (no OTP for now)
		user := &models.AdminUser{
			Username:       username,
			PasswordHash:   password, // will be hashed by CreateUser
			Status:         models.UserStatusActive,
			Instansi:       "personal",
			Role:           models.SerializeRoles([]string{models.RoleGuru}),
			MaxExams:       models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxExams, 3),
			MaxPDFSize:     models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxPDFSize, 1048576),
			MaxDrafts:      models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxDrafts, 2),
			MaxDraftSize:   models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxDraftSize, 1048576),
			WhatsappNumber: cleanWA,
		}

		defaultDays := models.GetSaasSettingInt(ctx, dbPool,
			models.SettingDefaultActiveDays, 1)
		t := time.Now().UTC().AddDate(0, 0, defaultDays)
		user.ExpiresAt = &t

		created, err := models.CreateUser(ctx, dbPool, user)
		if err != nil {
			log.Printf("register error: %v", err)
			data["error"] = "Gagal mendaftarkan akun. Silakan coba lagi."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}

		log.Printf("New user registered: %s (ID: %d, WA: %s)", created.Username, created.ID, created.WhatsappNumber)

		// Set flash message and redirect to login
		session := sessions.Default(c)
		session.AddFlash("Pendaftaran berhasil! Silakan login dengan akun Anda.")
		if err := session.Save(); err != nil {
			log.Printf("session save error on register: %v", err)
		}

		c.Redirect(http.StatusFound, "/login")
	}
}

// ---------------------------------------------------------------------------
// Template function implementations
// ---------------------------------------------------------------------------

// localizeUTCString converts a UTC timestamp to a localized time string.
func localizeUTCString(utcStr string, offsetMinutes int) string {
	if utcStr == "" {
		return "—"
	}
	s := strings.TrimSpace(utcStr)
	s = strings.ReplaceAll(s, " ", "T")
	if !strings.HasSuffix(s, "Z") && !strings.Contains(s, "+") {
		s += "Z"
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t, err = time.Parse("2006-01-02T15:04:05Z", s)
		if err != nil {
			return utcStr
		}
	}
	local := t.Add(time.Duration(offsetMinutes) * time.Minute)
	return local.Format("2006-01-02 15:04:05")
}

// activePageClass returns "active" when page matches activePage for nav highlighting.
func activePageClass(activePage, page string) string {
	if activePage == page {
		return "active"
	}
	return ""
}

// dropdownActive returns "active-dropdown-item" when page matches activePage.
func dropdownActive(activePage, page string) string {
	if activePage == page {
		return "active-dropdown-item"
	}
	return ""
}

// toMap converts alternating key-value pairs into a string-keyed map.
func toMap(values ...interface{}) map[string]interface{} {
	m := make(map[string]interface{}, len(values)/2)
	for i := 0; i < len(values)-1; i += 2 {
		key, ok := values[i].(string)
		if !ok {
			continue
		}
		m[key] = values[i+1]
	}
	return m
}

// defaultVal returns the default value when v is nil or an empty string.
func defaultVal(d, v interface{}) interface{} {
	if v == nil {
		return d
	}
	if s, ok := v.(string); ok && s == "" {
		return d
	}
	return v
}

// percentOf returns p as a percentage of t.
func percentOf(p, t float64) float64 {
	if t == 0 {
		return 0
	}
	return math.Round(p/t*10000) / 100
}

// roundTo rounds a float64 to the given number of decimal places.
func roundTo(val float64, decimals int) float64 {
	pow := math.Pow(10, float64(decimals))
	return math.Round(val*pow) / pow
}
