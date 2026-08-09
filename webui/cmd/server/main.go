// Command server is the EXAMVAN WebUI server entry point.
//
// It initialises the PostgreSQL connection, optionally connects to Redis,
// starts the async submission queue worker, registers all HTTP routes and
// middleware on a Gin engine, and runs the HTTP server with graceful
// shutdown support.
package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"html/template"
	"log"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/examvan/webui/internal/config"
	"github.com/examvan/webui/internal/database"
	"github.com/examvan/webui/internal/handlers/admin"
	"github.com/examvan/webui/internal/handlers/api"
	"github.com/examvan/webui/internal/handlers/public"
	r2client "github.com/examvan/webui/internal/handlers/r2"
	"github.com/examvan/webui/internal/helpers"
	"github.com/examvan/webui/internal/middleware"
	"github.com/examvan/webui/internal/models"
	"github.com/examvan/webui/internal/queue"
	redisclient "github.com/examvan/webui/internal/redis"
	"github.com/examvan/webui/internal/services/examtoken"
	"github.com/examvan/webui/internal/websocket"
	redis "github.com/redis/go-redis/v9"
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

	// Cancellable context for background jobs (package-expiry reconciliation).
	// Canceled during graceful shutdown so jobs stop before the DB pool closes.
	jobCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()

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

	// Share Redis client with context (already handled by injectServices middleware)

	// Set the configured admin username so models package can use it.
	models.SuperAdminUsername = cfg.AdminUser

	// -----------------------------------------------------------------------
	// 4. Start submission queue worker (only when both DB and Redis ready)
	// -----------------------------------------------------------------------
	var worker *queue.Worker
	if pool != nil && rdb != nil {
		worker = queue.StartWorker(rdb, pool)
		log.Println("Submission queue worker: started")
		startHeartbeatFlusher(rdb, pool)
	} else {
		log.Println("Submission queue worker: not started (requires both PostgreSQL and Redis)")
	}

	// -----------------------------------------------------------------------
	// 4b. Init Cloudflare R2 client (mandatory — PDF upload/download/serving
	// all go through R2; config.Load() already required the credentials).
	// -----------------------------------------------------------------------
	r2 := r2client.NewClient(cfg.R2AccessKey, cfg.R2SecretKey, cfg.R2Endpoint, cfg.R2Bucket)
	if r2 == nil || !r2.Enabled() {
		log.Fatalf("Cloudflare R2 initialization failed — R2 is mandatory for PDF upload/download/serving.")
	}
	log.Println("Cloudflare R2: ready — PDF upload/download via R2")

	// -----------------------------------------------------------------------
	// 4c. Package-expiry reconciliation job: pauses an exhausted active
	// package and auto-activates another claimed voucher that still has
	// remaining lifetime (auto-fallback), so a user is never locked out while
	// a usable package is on hand.
	// -----------------------------------------------------------------------
	if pool != nil {
		admin.StartPackageExpiryJob(jobCtx, pool)
	}

	// -----------------------------------------------------------------------
	// 5. Create Gin engine
	// -----------------------------------------------------------------------
	if cfg.IsDevelopment() {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	// Trust only the Docker bridge network and loopback so ClientIP()
	// reads X-Forwarded-For from nginx without allowing IP spoofing.
	// Docker default bridge: 172.17.0.0/16; Compose internal: 172.x.x.x
	r.SetTrustedProxies([]string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(middleware.CORS(cfg.CORSOrigins))

	// -----------------------------------------------------------------------
	// 6. Session store (cookie-based)
	// -----------------------------------------------------------------------
	store := cookie.NewStore([]byte(cfg.SecretKey))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   86400, // 1 day
		HttpOnly: true,
		Secure:   !cfg.IsDevelopment(),
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
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b int) int { return a * b },
		"div": func(a, b int) int {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"seq": func(n int) []int {
			s := make([]int, n)
			for i := range s {
				s[i] = i
			}
			return s
		},
		"dict":      func(values ...interface{}) map[string]interface{} { return toMap(values...) },
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
			return models.HasRole(roleStr, role)
		},
		"displayRole": func(roleStr string) string {
			return models.DisplayRoles(roleStr)
		},
		"formatExamTime": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			jakartaLoc, _ := time.LoadLocation("Asia/Jakarta")
			return t.In(jakartaLoc).Format("2006-01-02 15:04")
		},
		"substr": func(s string, start, end int) string {
			runes := []rune(s)
			if start < 0 {
				start = 0
			}
			if start >= len(runes) {
				return ""
			}
			if end > len(runes) {
				end = len(runes)
			}
			if end <= start {
				return ""
			}
			return string(runes[start:end])
		},
		// Layout helpers are now handled via Go HTML templates in templates/admin/partials/
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
	// PDFs are served only through authenticated/admin/token-gated handlers.
	// Direct /storage/* access is intentionally not exposed.

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
	// Inject R2 client (always present — R2 is mandatory).
	r.Use(func(c *gin.Context) { c.Set("r2", r2); c.Next() })

	// -----------------------------------------------------------------------
	// -----------------------------------------------------------------------
	// 10. Start WebSocket hub
	// -----------------------------------------------------------------------
	hub := websocket.NewHub(rdb)
	go hub.Run()
	log.Println("WebSocket hub: started")

	// Inject hub into context for handlers.
	r.Use(func(c *gin.Context) {
		c.Set("ws_hub", hub)
		c.Next()
	})

	// -----------------------------------------------------------------------
	// 11. Register routes
	// -----------------------------------------------------------------------
	registerRoutes(r, cfg, pool)

	// WebSocket endpoint (session-based or token-based auth required).
	r.GET("/ws/:room_id", func(c *gin.Context) {
		session := sessions.Default(c)
		roomID := c.Param("room_id")

		authorized := false
		if adminID := session.Get(middleware.SessionKeyAdminID); adminID != nil {
			// Logged-in admin: only authorize for exams they may monitor.
			// Without this, any authenticated tenant could join any exam room
			// and receive another tenant's live student PII broadcasts.
			if examID, err := strconv.Atoi(roomID); err == nil {
				var uid int
				switch v := adminID.(type) {
				case int:
					uid = v
				case int64:
					uid = int(v)
				case float64:
					uid = int(v)
				}
				isSuper, _ := session.Get(middleware.SessionKeyIsSuper).(bool)
				if uid > 0 && models.UserCanAccessExam(c.Request.Context(), pool, uid, isSuper, examID) {
					authorized = true
				}
			}
		} else {
			token := c.GetHeader("X-Exam-Token")
			if token == "" {
				token = c.Query("token")
			}
			if token != "" {
				examID, err := strconv.Atoi(roomID)
				if err == nil {
					ctx := c.Request.Context()
					exam, err := models.GetExamByID(ctx, pool, examID)
					if err == nil && exam.IsActive() && examtoken.Matches(exam, token) {
						authorized = true
					}
				}
			}
		}

		if !authorized {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "Unauthorized",
			})
			return
		}

		if err := hub.JoinRoom(c.Writer, c.Request, roomID); err != nil {
			log.Printf("websocket: join room %s error: %v", roomID, err)
		}
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

	// Stop submission queue worker first so no new jobs are picked up.
	if worker != nil {
		worker.Stop()
		log.Println("Shutdown: queue worker stopped")
	}

	// Stop background jobs (package-expiry reconciliation) before closing the
	// DB pool so they never query a closed connection.
	cancelJobs()

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
	r.GET("/robots.txt", robotsHandler())

	r.GET("/login", loginPageHandler(cfg))
	r.POST("/login", middleware.RateLimit(10, time.Minute), middleware.CSRFRequired(), loginHandler(cfg))

	// Logout via POST only (with CSRF protection).
	r.POST("/logout", middleware.CSRFRequired(), logoutHandler())
	// Legacy GET /logout redirects to login (prevents CSRF-based force-logout).
	r.GET("/logout", func(c *gin.Context) { c.Redirect(http.StatusFound, "/login") })

	// Legacy: /admin/login → /login
	r.GET("/admin/login", func(c *gin.Context) { c.Redirect(http.StatusFound, middleware.LoginURLWithNext(c.Query("next"))) })
	r.POST("/admin/login", middleware.RateLimit(10, time.Minute), middleware.CSRFRequired(), loginHandler(cfg))

	r.GET("/register", registerPageHandler(cfg))
	r.POST("/register", middleware.RateLimit(5, time.Minute), middleware.CSRFRequired(), registerPostHandler(cfg))
	r.GET("/register/confirm", registerConfirmPageHandler(cfg))
	r.POST("/register/confirm", middleware.RateLimit(5, time.Minute), middleware.CSRFRequired(), registerConfirmPostHandler(cfg))
	r.POST("/register/resend", middleware.RateLimit(5, time.Minute), middleware.CSRFRequired(), resendOTPHandler(cfg))

	// Password recovery
	r.GET("/forgot-password", forgotPasswordPageHandler(cfg))
	r.POST("/forgot-password", middleware.RateLimit(5, time.Minute), middleware.CSRFRequired(), forgotPasswordPostHandler(cfg))
	r.GET("/reset-password", resetPasswordPageHandler(cfg))
	r.POST("/reset-password", middleware.RateLimit(5, time.Minute), middleware.CSRFRequired(), resetPasswordPostHandler(cfg))

	r.GET("/download", public.DownloadPage())
	r.GET("/download/apk", public.DownloadAPK())
	r.GET("/download/app/:id", middleware.RateLimit(60, time.Minute), public.DownloadSystemApp())
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

		// Student exam routes deliberately use RateLimitIP (IP-only, no
		// fingerprint dimension). The app sends no fingerprint measure here by
		// design: a mid-exam device_id change (reinstall/clear-data) must never
		// retarget this limiter under an in-progress exam. Per-device throttling
		// is enforced inside SubmitExam keyed by exam+MAC.
		apiGroup.GET("/exams", middleware.RateLimitIP(60, time.Minute), middleware.AndroidVersionCheck(), api.ListExams())
		apiGroup.POST("/exams/request-approval", middleware.RateLimitIP(30, time.Minute), middleware.AndroidVersionCheck(), api.RequestApproval())
		apiGroup.GET("/exams/token/:token", middleware.RateLimitIP(30, time.Minute), middleware.AndroidVersionCheck(), api.ExamByToken())
		apiGroup.GET("/exams/:exam_id/pdf", middleware.RateLimitIP(30, time.Minute), middleware.AndroidVersionCheck(), api.ExamPDF())
		// Per-IP limit stays high because an entire classroom often submits from
		// a single NAT'd school IP near the deadline.
		apiGroup.POST("/exams/:exam_id/submit", middleware.LimitBodySize(5*1024*1024), middleware.RateLimitIP(120, time.Minute), middleware.AndroidVersionCheck(), api.SubmitExam())
		// Poll the outcome of an async submission (job_id from submit, or the
		// device identity used on submit). IP-rate-limited like the other
		// student endpoints; polling is cheap (single Redis GET / DB lookup).
		apiGroup.GET("/exams/:exam_id/result", middleware.RateLimitIP(60, time.Minute), middleware.AndroidVersionCheck(), api.ExamResult())
		apiGroup.POST("/exams/:exam_id/access-log", middleware.LimitBodySize(256*1024), middleware.RateLimitIP(30, time.Minute), middleware.AndroidVersionCheck(), api.AccessLog())
		apiGroup.POST("/exams/:exam_id/complete", middleware.LimitBodySize(256*1024), middleware.RateLimitIP(30, time.Minute), middleware.AndroidVersionCheck(), api.CompleteExam())

		apiGroup.GET("/hasil/:token", middleware.RateLimitIP(30, time.Minute), public.HasilAPI())
	}

	// ---- Admin pages (auth required) ----
	adminPages := r.Group("/admin", middleware.AuthRequired())
	{
		// Billing is the ONLY page a feature-locked (expired) account may use —
		// it is where the owner renews (redeems/activates a voucher).
		adminPages.GET("/billing", admin.BillingPage())

		// Logout must stay reachable for locked accounts (GET redirects to login,
		// so a locked owner can always sign out).
		adminPages.GET("/logout", func(c *gin.Context) { c.Redirect(http.StatusFound, "/login") })

		// Every other page requires full feature access: a feature-locked
		// account is redirected to /admin/billing by FeatureLockRequired.
		lockedPages := adminPages.Group("", middleware.FeatureLockRequired())
		{
			lockedPages.GET("/dashboard", admin.Dashboard())
			lockedPages.GET("/dashboard/redirect", admin.DashboardRedirect())
			lockedPages.GET("/submissions", admin.SubmissionsPage())

			lockedPages.GET("/users", middleware.AdminManagementRequired(), admin.UsersPage())
			lockedPages.GET("/vouchers", middleware.SuperAdminRequired(), admin.VouchersPage())
			lockedPages.GET("/packages", middleware.SuperAdminRequired(), admin.PackagesPage())

			lockedPages.GET("/pengawas", admin.PengawasPage())
			lockedPages.GET("/pengawas/:exam_id", admin.PengawasDetailPage())
			lockedPages.GET("/system-apps", middleware.SuperAdminRequired(), admin.SystemAppsPage())
		}
	}

	// ---- Admin API (auth required) ----
	adminAPI := r.Group("/admin/api",
		middleware.AuthRequired(),
	)
	{
		// Billing endpoints a feature-locked (expired) account may still call:
		// list its claimed packages, redeem a voucher, and activate one. These
		// are the ONLY admin APIs not gated by FeatureLockRequired.
		billingAPI := adminAPI.Group("", middleware.CSRFRequired())
		billingAPI.Use(middleware.RateLimit(120, time.Minute))
		{
			billingAPI.POST("/vouchers/redeem", middleware.LimitBodySize(256*1024), admin.RedeemVoucherHandler())
			billingAPI.POST("/vouchers/activate", middleware.LimitBodySize(256*1024), admin.ActivateVoucherHandler())
		}
		adminAPI.GET("/vouchers/mine", admin.ListMyRedemptionsHandler())

		// ---- Everything else requires full feature access ----
		lockedAPI := adminAPI.Group("", middleware.FeatureLockRequired())
		{
			lockedAPI.GET("/stats", admin.Stats())

			// ---- CSRF-protected routes (all POST) ----
			csrfAPI := lockedAPI.Group("", middleware.CSRFRequired())
			csrfAPI.Use(middleware.RateLimit(120, time.Minute))
			{

				// Exams.
				csrfAPI.POST("/upload", middleware.RateLimit(10, time.Minute), admin.UploadExam())
				csrfAPI.POST("/exams/bulk-delete", middleware.LimitBodySize(1024*1024), admin.BulkDelete())
				csrfAPI.POST("/exams/bulk-toggle", middleware.LimitBodySize(1024*1024), admin.BulkToggle())
				csrfAPI.POST("/exams/:exam_id/toggle", middleware.LimitBodySize(256*1024), admin.ToggleExam())
				csrfAPI.POST("/exams/:exam_id/delete", middleware.LimitBodySize(256*1024), admin.DeleteExam())
				csrfAPI.POST("/exams/:exam_id/edit", middleware.LimitBodySize(2*1024*1024), admin.EditExam())
				csrfAPI.POST("/exams/:exam_id/questions", middleware.LimitBodySize(5*1024*1024), admin.SaveQuestions())
				csrfAPI.POST("/exams/:exam_id/regenerate-token", middleware.LimitBodySize(256*1024), admin.RegenerateToken())
				csrfAPI.POST("/exams/:exam_id/edit-token", middleware.LimitBodySize(256*1024), admin.EditToken())
				csrfAPI.POST("/exams/:exam_id/token-mode", middleware.LimitBodySize(256*1024), admin.UpdateTokenMode())
				csrfAPI.POST("/exams/:exam_id/start", middleware.LimitBodySize(256*1024), admin.StartExam())
				csrfAPI.POST("/exams/:exam_id/stop", middleware.LimitBodySize(256*1024), admin.StopExam())
				csrfAPI.POST("/exams/:exam_id/toggle-public-results", middleware.LimitBodySize(256*1024), admin.TogglePublicResults())
				csrfAPI.POST("/exams/:exam_id/toggle-show-answers", middleware.LimitBodySize(256*1024), admin.ToggleShowAnswers())
				csrfAPI.POST("/exams/:exam_id/delegate", middleware.LimitBodySize(256*1024), admin.PostDelegateExam())
				csrfAPI.POST("/pengawas/exams/:exam_id/approvals/:mac_address", middleware.LimitBodySize(256*1024), admin.SetApprovalStatus())

				// Submissions.
				csrfAPI.POST("/submissions/:id/delete", middleware.LimitBodySize(256*1024), admin.DeleteSubmission())

				// Users (super admin / operator only).
				adminUsers := csrfAPI.Group("", middleware.AdminManagementRequired())
				{
					adminUsers.POST("/users", middleware.LimitBodySize(256*1024), admin.CreateUser())
					adminUsers.POST("/users/update-instansi", middleware.LimitBodySize(256*1024), admin.UpdateInstansi())
					adminUsers.POST("/users/:user_id/edit", middleware.LimitBodySize(256*1024), admin.EditUser())
					adminUsers.POST("/users/:user_id/toggle-status", middleware.LimitBodySize(256*1024), admin.ToggleUserStatus())
					adminUsers.POST("/users/:user_id/verify", middleware.LimitBodySize(256*1024), admin.VerifyUser())
					adminUsers.POST("/users/:user_id/delete", middleware.LimitBodySize(256*1024), admin.DeleteUser())
				}

				// SaaS settings (super admin only).
				adminSettings := csrfAPI.Group("", middleware.SuperAdminRequired())
				{
					adminSettings.POST("/saas-settings", middleware.LimitBodySize(1*1024*1024), admin.SaasSettings())
					adminSettings.POST("/saas-settings/test-smtp", middleware.LimitBodySize(256*1024), admin.TestSMTPConnectionEndpoint())
					adminSettings.POST("/system-apps", middleware.LimitBodySize(500*1024*1024), admin.UploadSystemApp())
					adminSettings.POST("/system-apps/:id/delete", middleware.LimitBodySize(256*1024), admin.DeleteSystemApp())
					adminSettings.POST("/packages", middleware.LimitBodySize(256*1024), admin.SavePackageSettingsHandler())
				}

				// Vouchers (management — creation, toggling, deletion are
				// super-admin-only; redeem/activate above are billing-exempt).
				adminVouchers := csrfAPI.Group("", middleware.SuperAdminRequired())
				{
					adminVouchers.POST("/vouchers", middleware.LimitBodySize(256*1024), admin.CreateVoucherHandler())
					adminVouchers.POST("/vouchers/batch", middleware.LimitBodySize(256*1024), admin.CreateBatchVouchersHandler())
					adminVouchers.POST("/vouchers/:id/toggle", middleware.LimitBodySize(256*1024), admin.ToggleVoucherStatusHandler())
					adminVouchers.POST("/vouchers/:id/delete", middleware.LimitBodySize(256*1024), admin.DeleteVoucherHandler())
				}

				csrfAPI.POST("/change-password", middleware.LimitBodySize(256*1024), middleware.RateLimit(3, time.Minute), admin.ChangePassword())
				csrfAPI.POST("/instansi/update", middleware.LimitBodySize(256*1024), admin.UpdateInstansi())
			}

			// ---- Non-CSRF routes (GET / read-only) ----
			lockedAPI.GET("/exams/:exam_id/questions", admin.GetQuestions())
			lockedAPI.GET("/exams/:exam_id/delegate-data", admin.DelegateData())
			lockedAPI.GET("/exams/:exam_id/pdf", admin.ExamPDF())
			lockedAPI.GET("/submissions", admin.ListSubmissions())
			lockedAPI.GET("/submissions/:id/detail", admin.SubmissionDetail())
			lockedAPI.GET("/submissions/export", middleware.RateLimit(30, time.Minute), admin.ExportSubmissions())
			lockedAPI.GET("/submissions/:id/export_detail", middleware.RateLimit(30, time.Minute), admin.ExportSubmissionDetail())
			lockedAPI.GET("/queue/status", admin.QueueStatus())
			adminUsersRead := lockedAPI.Group("", middleware.AdminManagementRequired())
			{
				adminUsersRead.GET("/users", admin.ListUsers())
			}

			lockedAPI.GET("/pengawas/exams", admin.PengawasExams())
			lockedAPI.GET("/pengawas/exams/:exam_id/submissions", admin.PengawasExamSubmissions())
			lockedAPI.GET("/pengawas/exams/:exam_id/approvals", admin.GetPendingApprovals())
			lockedAPI.GET("/saas-settings", middleware.SuperAdminRequired(), admin.SaasSettings())
			lockedAPI.GET("/packages", middleware.SuperAdminRequired(), admin.ListPackagesSettingsHandler())
			lockedAPI.GET("/vouchers", middleware.SuperAdminRequired(), admin.ListVouchers())
			lockedAPI.GET("/vouchers/:id/redemptions", middleware.SuperAdminRequired(), admin.ListVoucherRedemptionsHandler())
		}
	}

	// ---- Legacy redirects ----
	r.GET("/admin", func(c *gin.Context) { c.Redirect(http.StatusFound, "/admin/dashboard") })
}

// shortURLRedirectHandler redirects /<8-char-token> to /hasil/<token>.
// Uses regex to only match 8-character alphanumeric tokens (A-Z, 0-9),
// so it won't catch legitimate paths like /login, /admin, /api, etc.
func shortURLRedirectHandler() gin.HandlerFunc {
	tokenRe := regexp.MustCompile(fmt.Sprintf(`^[A-Z0-9]{%d}$`, config.TokenLength))
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
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		c.HTML(http.StatusOK, "public/index.html", data)
	}
}

func robotsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		seoIndex := true
		if dbValue, exists := c.Get("db"); exists && dbValue != nil {
			if pool, ok := dbValue.(*pgxpool.Pool); ok && pool != nil {
				seoIndex = models.GetSaasSettingBool(c.Request.Context(), pool, models.SettingSEOIndex, true)
			}
		}

		c.Header("Content-Type", "text/plain; charset=utf-8")
		if seoIndex {
			c.String(http.StatusOK, "User-agent: *\nAllow: /\nDisallow: /api/\nDisallow: /admin/\n")
		} else {
			c.String(http.StatusOK, "User-agent: *\nDisallow: /\n")
		}
	}
}

func loginPageHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Sudah login? Langsung ke dashboard, jangan tampilkan form login lagi.
		session := sessions.Default(c)
		if session.Get(middleware.SessionKeyAdminID) != nil {
			next := middleware.SafeRedirectPath(c.Query("next"))
			if next == "" {
				next = "/admin/dashboard"
			}
			c.Redirect(http.StatusFound, next)
			return
		}
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["error"] = nil
		// Flashes (e.g. "Pendaftaran berhasil..." or the forced-logout reason
		// from AuthRequired when an account's active period expired mid-session)
		// are consumed here so the login page can explain why the user was
		// signed out.
		data["flashes"] = session.Flashes()
		data["next"] = middleware.SafeRedirectPath(c.Query("next"))
		applyTurnstileData(c, data)
		c.HTML(http.StatusOK, "admin/login.html", data)
	}
}

var (
	failedLogins = make(map[string]int)
	lockoutTimes = make(map[string]time.Time)
	loginLock    sync.Mutex
)

func loginHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Lowercase to match the storage convention (register/CreateUser store
		// usernames lowercase) and to keep the failed-login lockout keyed
		// consistently regardless of the case typed.
		username := strings.ToLower(strings.TrimSpace(c.PostForm("username")))
		password := c.PostForm("password")
		nextTarget := middleware.SafeRedirectPath(c.PostForm("next"))
		if nextTarget == "" {
			nextTarget = middleware.SafeRedirectPath(c.Query("next"))
		}

		if username == "" || password == "" {
			renderLoginPage(c, cfg, nextTarget, "Username dan password wajib diisi.")
			return
		}

		// When there is no database, authentication is not possible.
		pool, exists := c.Get("db")
		if !exists || pool == nil {
			renderLoginPage(c, cfg, nextTarget, "Database tidak tersedia. Silakan hubungi administrator.")
			return
		}

		dbPool := pool.(*pgxpool.Pool)
		ctx := c.Request.Context()

		// Cloudflare Turnstile bot protection (SuperAdmin-managed), same
		// fail-closed policy as registration: a failed check rejects the
		// request WITHOUT counting toward the lockout below.
		if models.GetSaasSettingBool(ctx, dbPool, models.SettingTurnstileEnabled, false) {
			secret := models.GetSaasSettingWithDefault(ctx, dbPool, models.SettingTurnstileSecretKey, "")
			token := strings.TrimSpace(c.PostForm("cf-turnstile-response"))
			if !verifyTurnstileToken(ctx, secret, token, c.ClientIP()) {
				log.Printf("login blocked: turnstile verification failed for username=%q", username)
				renderLoginPage(c, cfg, nextTarget, "Verifikasi keamanan gagal. Silakan coba lagi.")
				return
			}
		}

		// Account Lockout check
		rdbVal, redisExists := c.Get("redis")
		var rdb *redis.Client
		if redisExists && rdbVal.(*redis.Client) != nil {
			rdb = rdbVal.(*redis.Client)
		}

		lockoutKey := "lockout:" + username
		if rdb != nil {
			val, err := rdb.Get(ctx, lockoutKey).Result()
			if err == nil {
				attempts, _ := strconv.Atoi(val)
				if attempts >= 5 {
					ttl, _ := rdb.TTL(ctx, lockoutKey).Result()
					renderLoginPage(c, cfg, nextTarget, fmt.Sprintf("Akun dikunci sementara karena terlalu banyak kegagalan login. Silakan coba lagi dalam %d menit.", int(ttl.Minutes())+1))
					return
				}
			}
		} else {
			loginLock.Lock()
			if exp, locked := lockoutTimes[username]; locked && time.Now().Before(exp) {
				loginLock.Unlock()
				renderLoginPage(c, cfg, nextTarget, fmt.Sprintf("Akun dikunci sementara karena terlalu banyak kegagalan login. Silakan coba lagi dalam %d menit.", int(time.Until(exp).Minutes())+1))
				return
			}
			loginLock.Unlock()
		}

		user, errMsg := models.AuthenticateUser(ctx, dbPool, username, password)
		if errMsg != "" {
			// Increment failed attempts
			if rdb != nil {
				pipe := rdb.Pipeline()
				pipe.Incr(ctx, lockoutKey)
				pipe.Expire(ctx, lockoutKey, 15*time.Minute)
				_, _ = pipe.Exec(ctx)
			} else {
				loginLock.Lock()
				// Opportunistic cleanup so these in-memory maps (keyed by
				// attacker-controlled usernames when Redis is absent) cannot
				// grow unbounded: drop expired lockouts, and hard-cap as a
				// backstop against a flood of unique usernames.
				now := time.Now()
				for u, exp := range lockoutTimes {
					if now.After(exp) {
						delete(lockoutTimes, u)
						delete(failedLogins, u)
					}
				}
				if len(failedLogins) > 10000 {
					failedLogins = make(map[string]int)
				}
				failedLogins[username]++
				if failedLogins[username] >= 5 {
					lockoutTimes[username] = now.Add(15 * time.Minute)
					failedLogins[username] = 0
				}
				loginLock.Unlock()
			}

			renderLoginPage(c, cfg, nextTarget, errMsg)
			return
		}

		// Reset failed attempts upon successful login
		if rdb != nil {
			rdb.Del(ctx, lockoutKey)
		} else {
			loginLock.Lock()
			delete(failedLogins, username)
			delete(lockoutTimes, username)
			loginLock.Unlock()
		}

		session := sessions.Default(c)
		// Regenerate session — clear old values, set new, save.
		session.Clear()
		session.Set(middleware.SessionKeyAdminID, user.ID)
		session.Set(middleware.SessionKeyUsername, user.Username)
		session.Set(middleware.SessionKeyName, user.Name)
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

		redirectTarget := "/admin/dashboard"
		if nextTarget != "" {
			redirectTarget = nextTarget
		}
		// A feature-locked (expired) account is sent straight to the billing
		// page: it can renew there (redeem/activate a voucher) but every other
		// admin page is blocked by FeatureLockRequired.
		if user.IsFeatureLocked() {
			redirectTarget = "/admin/billing"
		}
		c.Redirect(http.StatusFound, redirectTarget)
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
		// Sudah login? Tidak perlu mendaftar lagi, langsung ke dashboard.
		session := sessions.Default(c)
		if session.Get(middleware.SessionKeyAdminID) != nil {
			c.Redirect(http.StatusFound, "/admin/dashboard")
			return
		}
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["error"] = nil
		data["flashes"] = nil

		applyRegisterPageData(c, data)

		c.HTML(http.StatusOK, "public/register.html", data)
	}
}

// applyRegisterPageData fills the template keys shared by the register page and
// the register POST handler: email-verification state and the Cloudflare
// Turnstile widget config. The POST handler re-renders public/register.html on
// every validation error, so both paths must populate these consistently
// (otherwise the widget would vanish and the email banner would mis-label the
// second step after a failed submit).
func applyRegisterPageData(c *gin.Context, data gin.H) {
	pool, exists := c.Get("db")
	if exists && pool != nil {
		data["email_enabled"] = models.GetSaasSettingBool(c.Request.Context(), pool.(*pgxpool.Pool), models.SettingEmailVerificationEnabled, false)
	} else {
		data["email_enabled"] = false
	}
	applyTurnstileData(c, data)
}

// applyTurnstileData fills the Cloudflare Turnstile widget config for any
// public form page (register, login, forgot-password). Keys come from the
// SuperAdmin-managed SaaS settings; the widget simply isn't rendered when
// Turnstile is disabled or the DB is unavailable.
func applyTurnstileData(c *gin.Context, data gin.H) {
	pool, exists := c.Get("db")
	if exists && pool != nil {
		dbPool := pool.(*pgxpool.Pool)
		ctx := c.Request.Context()
		data["turnstile_enabled"] = models.GetSaasSettingBool(ctx, dbPool, models.SettingTurnstileEnabled, false)
		data["turnstile_site_key"] = models.GetSaasSettingWithDefault(ctx, dbPool, models.SettingTurnstileSiteKey, "")
	} else {
		data["turnstile_enabled"] = false
		data["turnstile_site_key"] = ""
	}
}

// renderLoginPage re-renders admin/login.html with the given error while
// keeping the page context (next target, Turnstile widget config) consistent.
func renderLoginPage(c *gin.Context, cfg *config.Config, next, errMsg string) {
	data := middleware.TemplateData(c)
	data["version"] = cfg.Version
	data["error"] = errMsg
	// Consume pending flashes so a flash set before a failed login attempt is
	// not replayed forever (and the expiry flash from AuthRequired still shows
	// even when the next request is a failed login POST).
	data["flashes"] = sessions.Default(c).Flashes()
	data["next"] = next
	applyTurnstileData(c, data)
	c.HTML(http.StatusOK, "admin/login.html", data)
}

// verifyTurnstileToken validates a Cloudflare Turnstile widget response token
// against the siteverify endpoint. It fails closed: any error (network, empty
// secret/token, invalid token) returns false so the registration is rejected.
func verifyTurnstileToken(ctx context.Context, secret, response, remoteIP string) bool {
	if strings.TrimSpace(secret) == "" || strings.TrimSpace(response) == "" {
		return false
	}
	// Use the caller's context so the siteverify call is canceled when the
	// request context is aborted (e.g. the 30s timeout middleware) or the
	// client disconnects — not just after our own 10s timeout.
	form := url.Values{}
	form.Set("secret", secret)
	form.Set("response", response)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://challenges.cloudflare.com/turnstile/v0/siteverify",
		strings.NewReader(form.Encode()))
	if err != nil {
		log.Printf("turnstile siteverify request error: %v", err)
		return false
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("turnstile siteverify error: %v", err)
		return false
	}
	defer resp.Body.Close()
	var out struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		log.Printf("turnstile siteverify decode error: %v", err)
		return false
	}
	return out.Success
}

func registerPostHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.ToLower(strings.TrimSpace(c.PostForm("username")))
		email := strings.TrimSpace(c.PostForm("email"))
		password := c.PostForm("password")

		data := middleware.TemplateData(c)
		data["version"] = cfg.Version

		// Keep the register-page render keys consistent on error re-renders.
		applyRegisterPageData(c, data)

		// Validation failures re-render the form; echo the submitted username and
		// email back so the user does not have to retype them (the password is
		// deliberately never echoed back — a failed submit must re-enter it).
		// registerError centralizes this so every error path is consistent.
		registerError := func(errMsg string) {
			data["error"] = errMsg
			data["form_username"] = username
			data["form_email"] = email
			c.HTML(http.StatusOK, "public/register.html", data)
		}

		// Validate input
		if username == "" || password == "" || email == "" {
			registerError("Username, email, dan password wajib diisi.")
			return
		}

		if !models.IsValidUsername(username) {
			registerError("Username hanya boleh berisi huruf kecil, angka, titik, garis bawah, dan strip (3-32 karakter).")
			return
		}

		if len(password) < 8 {
			registerError("Password minimal 8 karakter.")
			return
		}

		if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
			registerError("Format email tidak valid.")
			return
		}

		pool, exists := c.Get("db")
		if !exists || pool == nil {
			registerError("Database tidak tersedia. Silakan hubungi administrator.")
			return
		}
		dbPool := pool.(*pgxpool.Pool)
		ctx := c.Request.Context()

		// Per-IP account creation cap (SuperAdmin-managed; 0 = unlimited) — a
		// defense-in-depth layer on top of Turnstile. Counts accounts registered
		// from this IP within the last 24 hours, so a script that rotates past
		// the Turnstile challenge still cannot farm many accounts from one IP.
		if maxPerIP := models.GetSaasSettingInt(ctx, dbPool, models.SettingMaxAccountsPerIP, 3); maxPerIP > 0 {
			clientIP := c.ClientIP()
			recent, err := models.CountRecentRegistrationsByIP(ctx, dbPool, clientIP)
			if err == nil && !models.RegistrationAllowedByPerIPLimit(recent, maxPerIP) {
				log.Printf("register blocked: per-IP limit reached for %s (%d/%d in 24h)", clientIP, recent, maxPerIP)
				registerError(fmt.Sprintf("Terlalu banyak pendaftaran dari alamat IP ini dalam 24 jam terakhir (maks %d akun). Silakan coba lagi besok atau hubungi administrator.", maxPerIP))
				return
			}
		}

		// Cloudflare Turnstile bot protection (SuperAdmin-managed). Fail-closed:
		// when enabled, a submission without a valid widget token is rejected.
		// This blocks mass-registration scripts even when they rotate IPs, since
		// each account creation now needs a fresh, single-use Turnstile token.
		if models.GetSaasSettingBool(ctx, dbPool, models.SettingTurnstileEnabled, false) {
			secret := models.GetSaasSettingWithDefault(ctx, dbPool, models.SettingTurnstileSecretKey, "")
			token := strings.TrimSpace(c.PostForm("cf-turnstile-response"))
			if !verifyTurnstileToken(ctx, secret, token, c.ClientIP()) {
				log.Printf("register blocked: turnstile verification failed for username=%q", username)
				registerError("Verifikasi keamanan gagal. Silakan coba lagi.")
				return
			}
		}

		// Enforce the trusted email-domain whitelist (SuperAdmin-managed).
		whitelist := models.GetSaasSettingWithDefault(ctx, dbPool, models.SettingEmailDomainWhitelist, "")
		if !models.EmailDomainAllowed(whitelist, email) {
			allowed := strings.Join(models.ParseDomainList(whitelist), ", ")
			registerError("Domain email tidak diizinkan untuk pendaftaran. Gunakan email dari domain terpercaya: " + allowed)
			return
		}

		// Check if username is taken
		if username == cfg.AdminUser || username == "admin" || username == "superadmin" {
			registerError("Username tidak tersedia.")
			return
		}

		existing, err := models.GetUserByUsername(ctx, dbPool, username)
		if err == nil && existing.Username != "" {
			registerError("Username sudah digunakan.")
			return
		}

		// Email must also be unique across accounts. Syncs with the partial
		// unique index in schema.sql (email <> ''), and is done here for a
		// friendlier message than a raw DB constraint violation.
		existingByEmail, err := models.GetUserByEmail(ctx, dbPool, email)
		if err == nil && existingByEmail.Email != "" {
			registerError("Email sudah terdaftar. Gunakan email lain atau masuk dengan akun yang ada.")
			return
		}

		// Check if email verification is enabled
		emailEnabled := models.GetSaasSettingBool(ctx, dbPool, models.SettingEmailVerificationEnabled, false)
		status := models.UserStatusActive
		var otpCode *string
		var otpExpiry *time.Time

		if emailEnabled {
			status = models.UserStatusPendingOTP
			const digits = "0123456789"
			result := make([]byte, 6)
			for i := 0; i < 6; i++ {
				n, err := rand.Int(rand.Reader, big.NewInt(10))
				if err != nil {
					result[i] = digits[time.Now().UnixNano()%10]
				} else {
					result[i] = digits[n.Int64()]
				}
			}
			codeStr := string(result)
			otpCode = &codeStr
			expiryTime := time.Now().UTC().Add(15 * time.Minute)
			otpExpiry = &expiryTime
		}

		user := &models.AdminUser{
			Username:           username,
			PasswordHash:       password, // will be hashed by CreateUser
			Status:             status,
			Instansi:           "personal",
			RegisteredIP:       c.ClientIP(),
			Role:               models.SerializeRoles([]string{models.RoleGuru}),
			MaxExams:           models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxExams, 3),
			MaxPDFSize:         models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxPDFSize, 1048576),
			MaxConcurrentExams: models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxConcurrentExams, 2),
			MaxStorageSize:     int64(models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxStorageSize, 52428800)),
			WhatsappNumber:     "",
			Email:              email,
			OTPCode:            otpCode,
			OTPExpiry:          otpExpiry,
		}

		defaultDays := models.GetSaasSettingInt(ctx, dbPool,
			models.SettingDefaultActiveDays, 14)
		t := time.Now().UTC().AddDate(0, 0, defaultDays)
		user.ExpiresAt = &t

		created, err := models.CreateUser(ctx, dbPool, user)
		if err != nil {
			log.Printf("register error: %v", err)
			data["error"] = "Gagal mendaftarkan akun. Silakan coba lagi."
			c.HTML(http.StatusOK, "public/register.html", data)
			return
		}

		log.Printf("New user registered: %s (ID: %d)", created.Username, created.ID)

		if emailEnabled {
			// Retrieve SMTP settings
			smtpHost := models.GetSaasSettingWithDefault(ctx, dbPool, models.SettingSMTPHost, "smtp.gmail.com")
			smtpPort := models.GetSaasSettingWithDefault(ctx, dbPool, models.SettingSMTPPort, "587")
			smtpUser := models.GetSaasSettingWithDefault(ctx, dbPool, models.SettingSMTPUser, "")
			smtpPassword := models.GetSaasSettingWithDefault(ctx, dbPool, models.SettingSMTPPassword, "")
			senderName := models.GetSaasSettingWithDefault(ctx, dbPool, models.SettingSMTPSenderName, "EXAMVAN")

			err = helpers.SendVerificationEmail(smtpHost, smtpPort, smtpUser, smtpPassword, senderName, email, username, *otpCode)
			if err != nil {
				log.Printf("Failed to send verification email to %s: %v", email, err)
				// Delete user record
				_, _ = dbPool.Exec(ctx, `DELETE FROM admin_users WHERE id = $1`, created.ID)

				data["error"] = "Gagal mengirimkan email verifikasi. Pastikan pengaturan SMTP admin sudah benar atau hubungi admin."
				data["form_username"] = username
				data["form_email"] = email
				c.HTML(http.StatusOK, "public/register.html", data)
				return
			}

			c.Redirect(http.StatusFound, "/register/confirm?username="+username)
			return
		}

		// Set flash message and redirect to login
		session := sessions.Default(c)
		session.AddFlash("Pendaftaran berhasil! Silakan login dengan akun Anda.")
		if err := session.Save(); err != nil {
			log.Printf("session save error on register: %v", err)
		}

		c.Redirect(http.StatusFound, "/login")
	}
}

func registerConfirmPageHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.TrimSpace(c.Query("username"))
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["error"] = nil

		if username == "" {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		pool, exists := c.Get("db")
		if !exists || pool == nil {
			c.Redirect(http.StatusFound, "/login")
			return
		}
		dbPool := pool.(*pgxpool.Pool)
		ctx := c.Request.Context()

		// Fetch user details
		var u models.AdminUser
		err := dbPool.QueryRow(ctx,
			`SELECT username, email, status, otp_code FROM admin_users 
			 WHERE LOWER(username) = LOWER($1)`, username).Scan(&u.Username, &u.Email, &u.Status, &u.OTPCode)

		if err != nil || u.Status != models.UserStatusPendingOTP || u.OTPCode == nil {
			c.Redirect(http.StatusFound, "/login")
			return
		}

		data["username"] = u.Username
		data["email"] = u.Email
		data["masked_email"] = maskEmail(u.Email)

		c.HTML(http.StatusOK, "public/register_confirm.html", data)
	}
}

func maskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return email
	}
	username := parts[0]
	domain := parts[1]

	if len(username) <= 2 {
		return username + "***@" + domain
	}
	return string(username[0]) + strings.Repeat("*", len(username)-2) + string(username[len(username)-1]) + "@" + domain
}

func registerConfirmPostHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.TrimSpace(c.Query("username"))
		otpCode := strings.TrimSpace(c.PostForm("otp_code"))

		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["username"] = username

		pool, exists := c.Get("db")
		if !exists || pool == nil {
			data["error"] = "Database tidak tersedia."
			c.HTML(http.StatusOK, "public/register_confirm.html", data)
			return
		}
		dbPool := pool.(*pgxpool.Pool)
		ctx := c.Request.Context()

		var u models.AdminUser
		var otpAttempts int
		err := dbPool.QueryRow(ctx,
			`SELECT id, username, email, status, otp_code, otp_expiry, otp_attempts FROM admin_users
			 WHERE LOWER(username) = LOWER($1)`, username).Scan(&u.ID, &u.Username, &u.Email, &u.Status, &u.OTPCode, &u.OTPExpiry, &otpAttempts)

		if err != nil {
			data["error"] = "User tidak ditemukan."
			c.HTML(http.StatusOK, "public/register_confirm.html", data)
			return
		}

		data["email"] = u.Email
		data["masked_email"] = maskEmail(u.Email)

		if u.Status != models.UserStatusPendingOTP || u.OTPCode == nil || *u.OTPCode == "" {
			data["error"] = "Akun Anda sudah aktif atau tidak membutuhkan verifikasi."
			c.HTML(http.StatusOK, "public/register_confirm.html", data)
			return
		}

		if *u.OTPCode != otpCode {
			// Count the wrong guess. After the limit, delete the (still
			// unverified) registration so brute force cannot continue — the
			// user simply registers again. Mirrors the expiry-delete behaviour.
			if otpAttempts+1 >= maxOTPAttempts {
				_, _ = dbPool.Exec(ctx, `DELETE FROM admin_users WHERE id = $1`, u.ID)
				data["error"] = "Terlalu banyak percobaan salah. Silakan lakukan registrasi ulang."
			} else {
				_, _ = dbPool.Exec(ctx, `UPDATE admin_users SET otp_attempts = otp_attempts + 1 WHERE id = $1`, u.ID)
				data["error"] = "Kode OTP yang Anda masukkan salah. Sisa percobaan: " + strconv.Itoa(maxOTPAttempts-otpAttempts-1) + "."
			}
			c.HTML(http.StatusOK, "public/register_confirm.html", data)
			return
		}

		// Check expiry
		if u.OTPExpiry != nil {
			if time.Now().UTC().After(*u.OTPExpiry) {
				// Delete user record so they can try again
				_, _ = dbPool.Exec(ctx, `DELETE FROM admin_users WHERE id = $1`, u.ID)
				data["error"] = "Kode OTP telah kedaluwarsa. Silakan lakukan registrasi ulang."
				c.HTML(http.StatusOK, "public/register_confirm.html", data)
				return
			}
		}

		// Activate user
		_, err = dbPool.Exec(ctx,
			`UPDATE admin_users SET status = 'active', otp_code = NULL, otp_expiry = NULL WHERE id = $1`, u.ID)
		if err != nil {
			log.Printf("Failed to activate user %s: %v", username, err)
			data["error"] = "Gagal mengaktifkan akun. Silakan coba lagi."
			c.HTML(http.StatusOK, "public/register_confirm.html", data)
			return
		}

		session := sessions.Default(c)
		session.AddFlash("Pendaftaran berhasil! Akun Anda telah aktif, silakan login.")
		session.Save()

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

// startHeartbeatFlusher periodically flushes heartbeat data from Redis to PostgreSQL.
// Runs in the background, does not block API requests.
func startHeartbeatFlusher(rdb *redis.Client, pool *pgxpool.Pool) {
	if rdb == nil || pool == nil {
		log.Println("heartbeat-flusher: skipped (requires Redis + PostgreSQL)")
		return
	}

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			flushHeartbeatsQueue(context.Background(), rdb, pool)
		}
	}()
	log.Println("heartbeat-flusher: started (flush every 30s)")
}

func flushHeartbeatsQueue(ctx context.Context, rdb *redis.Client, pool *pgxpool.Pool) {
	const batchSize = 100

	// Pop up to batchSize items from the list
	var payloads []string
	for i := 0; i < batchSize; i++ {
		val, err := rdb.RPop(ctx, "examvan:heartbeats:pending").Result()
		if err != nil {
			break
		}
		payloads = append(payloads, val)
	}

	if len(payloads) == 0 {
		return
	}

	type heartbeatData struct {
		ExamID       int    `json:"exam_id"`
		MacAddress   string `json:"mac_address"`
		StudentName  string `json:"student_name"`
		ExamNumber   string `json:"exam_number"`
		StudentClass string `json:"student_class"`
		DeviceInfo   string `json:"device_info"`
		IPAddress    string `json:"ip_address"`
		Event        string `json:"event"`
		LastSeenStr  string `json:"last_seen"`
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Printf("heartbeat-flusher: tx begin error: %v", err)
		return
	}
	defer tx.Rollback(ctx)

	for _, payload := range payloads {
		var hb heartbeatData
		if err := json.Unmarshal([]byte(payload), &hb); err != nil {
			continue
		}

		t, err := time.Parse(time.RFC3339, hb.LastSeenStr)
		if err != nil {
			t = time.Now().UTC()
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO student_access_logs
			 (exam_id, student_identifier, student_name, exam_number, student_class, event, ip_address, device_info, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			hb.ExamID, hb.MacAddress, hb.StudentName, hb.ExamNumber, hb.StudentClass,
			hb.Event, hb.IPAddress, hb.DeviceInfo, t,
		)
		if err != nil {
			log.Printf("heartbeat-flusher: insert error: %v", err)
		}

		// UPSERT into submissions to make the student appear in "Monitoring Perangkat" immediately
		var latestAnswers *string
		errLookup := tx.QueryRow(ctx, "SELECT answers_json FROM submissions WHERE exam_id=$1 AND mac_address=$2 ORDER BY created_at DESC LIMIT 1", hb.ExamID, hb.MacAddress).Scan(&latestAnswers)

		// If no row exists, or the latest one is already submitted, insert a new empty row
		if errLookup == pgx.ErrNoRows || (errLookup == nil && latestAnswers != nil && *latestAnswers != "") {
			_, err = tx.Exec(ctx, `
				INSERT INTO submissions (exam_id, student_name, exam_number, student_class, mac_address, start_time, created_at, identity_data)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			`, hb.ExamID, hb.StudentName, hb.ExamNumber, hb.StudentClass, hb.MacAddress, t.Format(time.RFC3339), t, "{}")
			if err != nil {
				log.Printf("heartbeat-flusher: failed to create empty submission: %v", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		log.Printf("heartbeat-flusher: tx commit error: %v", err)
	} else {
		log.Printf("heartbeat-flusher: successfully flushed %d heartbeats to PostgreSQL", len(payloads))
	}
}
