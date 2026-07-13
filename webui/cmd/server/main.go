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
	"html"
	"html/template"
	"log"
	"math"
	"math/big"
	"net/http"
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
	"github.com/examvan/webui/internal/services/examtoken"
	"github.com/examvan/webui/internal/queue"
	redisclient "github.com/examvan/webui/internal/redis"
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

	if pool != nil {
		startTransactionCleaner(pool)
	}

	// -----------------------------------------------------------------------
	// 4b. Init Cloudflare R2 client (optional — for PDF offloading)
	// -----------------------------------------------------------------------
	var r2 *r2client.Client
	if cfg.R2AccessKey != "" && cfg.R2SecretKey != "" && cfg.R2Endpoint != "" {
		r2 = r2client.NewClient(cfg.R2AccessKey, cfg.R2SecretKey, cfg.R2Endpoint, cfg.R2Bucket)
		if r2 != nil && r2.Enabled() {
			log.Println("Cloudflare R2: ready — PDF upload/download via R2")
		} else {
			log.Println("Cloudflare R2: init failed — PDF will be served locally")
		}
	} else {
		log.Println("Cloudflare R2: not configured — PDF will be served from local storage")
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
		// Layout helpers — return HTML fragments for self-contained pages
		"adminHead": func(version, csrfToken, title string) template.HTML {
			titleEscaped := html.EscapeString(title)
			return template.HTML(fmt.Sprintf(
				`<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0"><title>EXAMVAN — %s</title><link rel="stylesheet" href="/static/css/theme.css?v=%s"><link rel="stylesheet" href="/static/css/tailwind/output.css?v=%s"><link rel="stylesheet" href="/static/css/admin-base.css?v=%s"><link rel="icon" type="image/png" href="/static/favicon.png"><meta name="csrf-token" content="%s">`,
				titleEscaped, version, version, version, csrfToken))
		},
		"adminNav": func(activePage, adminRole, adminUser, csrfToken string) template.HTML {
			// Prevent XSS: escape user-controlled values
			adminUser = html.EscapeString(adminUser)

			roleDisplay := models.DisplayRoles(adminRole)
			roleDisplay = html.EscapeString(roleDisplay)

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
			if isPengawas || isGuru {
				pengawasLink = fmt.Sprintf(`<a href="/admin/pengawas" class="nav-link %s"><svg class="icon-svg" aria-hidden="true"><use href="#hi-eye"/></svg> Pengawasan</a>`, activePageClass(activePage, "pengawas"))
			}
			pengaturanLink := ""
			if isSuper || isOp || strings.Contains(adminRole, "operator") {
				isActiveSettings := activePage == "users" || activePage == "billing" || activePage == "system-apps"
				activeClass := ""
				if isActiveSettings {
					activeClass = "active"
				}
				pengaturanLink = fmt.Sprintf(`<div class="topbar-menu-dropdown" style="display:inline-block;position:relative;"><button class="nav-link %s" onclick="event.stopPropagation();document.getElementById('pengaturanDropdown').classList.toggle('show');" style="background:none;border:none;cursor:pointer;font-family:inherit;font-size:inherit;display:inline-flex;align-items:center;padding:0 16px;"><svg class="icon-svg" aria-hidden="true"><use href="#hi-settings"/></svg> Pengaturan <svg class="icon-svg" style="width:12px;height:12px;margin-left:4px;"><use href="#hi-chevron-down"/></svg></button><div class="topbar-dropdown-content" id="pengaturanDropdown" style="left:0;right:auto;top:100%%;margin-top:8px;min-width:200px;">`, activeClass)
				pengaturanLink += fmt.Sprintf(`<a href="/admin/users" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-users"/></svg> Kelola User</a>`, dropdownActive(activePage, "users"))
				pengaturanLink += fmt.Sprintf(`<a href="/admin/billing" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-clipboard"/></svg> Billing & Paket</a>`, dropdownActive(activePage, "billing"))
				if isSuper {
					pengaturanLink += fmt.Sprintf(`<a href="/admin/system-apps" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-download"/></svg> Aplikasi Sistem</a>`, dropdownActive(activePage, "system-apps"))
				}
				pengaturanLink += `</div></div>`
			} else if isGuru {
				pengaturanLink = fmt.Sprintf(`<a href="/admin/billing" class="nav-link %s"><svg class="icon-svg" aria-hidden="true"><use href="#hi-clipboard"/></svg> Upgrade Paket</a>`, activePageClass(activePage, "billing"))
			}

			// Mobile nav links for hamburger menu
			mobileLinks := `<div class="dropdown-divider mobile-only-divider"></div><div class="mobile-nav-links">`
			if isGuru {
				mobileLinks += fmt.Sprintf(`<a href="/admin/dashboard" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-dashboard"/></svg> Daftar Ujian</a><a href="/admin/submissions" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-results"/></svg> Hasil Ujian</a>`, dropdownActive(activePage, "dashboard"), dropdownActive(activePage, "submissions"))
			}
			if isPengawas || isGuru {
				mobileLinks += fmt.Sprintf(`<a href="/admin/pengawas" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-eye"/></svg> Pengawasan</a>`, dropdownActive(activePage, "pengawas"))
			}
			if isSuper || isOp {
				mobileLinks += `<div class="dropdown-divider"></div><div style="padding: 8px 16px; font-size: 11px; font-weight: 600; color: #94a3b8; text-transform: uppercase; letter-spacing: 0.5px;">Pengaturan</div>`
				mobileLinks += fmt.Sprintf(`<a href="/admin/users" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-users"/></svg> Kelola User</a>`, dropdownActive(activePage, "users"))
				mobileLinks += fmt.Sprintf(`<a href="/admin/billing" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-clipboard"/></svg> Billing & Paket</a>`, dropdownActive(activePage, "billing"))
				if isSuper {
					mobileLinks += fmt.Sprintf(`<a href="/admin/system-apps" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-download"/></svg> Aplikasi Sistem</a>`, dropdownActive(activePage, "system-apps"))
				}
			} else if isGuru {
				mobileLinks += `<div class="dropdown-divider"></div>`
				mobileLinks += fmt.Sprintf(`<a href="/admin/billing" class="dropdown-item %s"><svg class="icon-svg"><use href="#hi-clipboard"/></svg> Upgrade Paket</a>`, dropdownActive(activePage, "billing"))
			}
			mobileLinks += `</div><div class="dropdown-divider"></div>`

			dropdownBillingLink := ""
			if isSuper || isOp {
				dropdownBillingLink = `<a href="/admin/billing" class="dropdown-item" style="text-decoration:none;color:inherit;"><svg class="icon-svg"><use href="#hi-clipboard"/></svg> Billing & Paket</a>`
			} else if isGuru {
				dropdownBillingLink = `<a href="/admin/billing" class="dropdown-item" style="text-decoration:none;color:inherit;"><svg class="icon-svg"><use href="#hi-clipboard"/></svg> Upgrade Paket</a>`
			}
			if isSuper {
				dropdownBillingLink += `<a href="/admin/system-apps" class="dropdown-item" style="text-decoration:none;color:inherit;"><svg class="icon-svg"><use href="#hi-download"/></svg> Aplikasi Sistem</a>`
			}
			if dropdownBillingLink != "" {
				dropdownBillingLink += `<div class="dropdown-divider"></div>`
			}

			csrfEscaped := html.EscapeString(csrfToken)
			return template.HTML(fmt.Sprintf(
				`<nav class="topbar"><div class="topbar-left"><div class="topbar-logo">E</div><a href="/" class="topbar-title" style="text-decoration:none;color:inherit;">EXAMVAN</a></div><div class="topbar-center"><div class="topbar-nav">%s%s%s%s</div></div><div class="topbar-right"><div class="topbar-menu-dropdown"><button class="topbar-menu-toggle" id="menuToggleBtn" onclick="event.stopPropagation();document.getElementById('menuDropdownContent').classList.toggle('show');"><span class="menu-hamburger-icon">&#9776;</span></button><div class="topbar-dropdown-content" id="menuDropdownContent"><div class="dropdown-header mobile-only-header"><div class="dropdown-brand-row"><div class="dropdown-logo">E</div><span class="dropdown-brand-title">EXAMVAN</span></div></div>%s<div class="dropdown-user-info"><span class="dropdown-user-name">%s</span><span class="dropdown-user-role">%s</span></div><div class="dropdown-divider"></div>%s<button class="dropdown-item" onclick="openChangePasswordModal()"><svg class="icon-svg"><use href="#hi-key"/></svg> Ubah Password</button><div class="dropdown-divider"></div><form method="POST" action="/logout" style="display:inline;"><input type="hidden" name="_csrf_token" value="%s"><button type="submit" class="dropdown-item dropdown-logout" style="width:100%%;border:none;background:none;cursor:pointer;"><svg class="icon-svg" aria-hidden="true"><use href="#hi-logout"/></svg> Logout</button></form></div></div></div></nav>`,
				guruLink, pengawasLink, pengaturanLink, "", mobileLinks, adminUser, roleDisplay, dropdownBillingLink, csrfEscaped))
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
	// Inject R2 client when available.
	if r2 != nil && r2.Enabled() {
		r.Use(func(c *gin.Context) { c.Set("r2", r2); c.Next() })
	}

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
		if session.Get(middleware.SessionKeyAdminID) != nil {
			authorized = true
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
	r.GET("/pricing", pricingHandler(cfg))

	r.GET("/login", loginPageHandler(cfg))
	r.POST("/login", middleware.RateLimit(10, time.Minute), loginHandler(cfg))

	// Logout via POST only (with CSRF protection).
	r.POST("/logout", middleware.CSRFRequired(), logoutHandler())
	// Legacy GET /logout redirects to login (prevents CSRF-based force-logout).
	r.GET("/logout", func(c *gin.Context) { c.Redirect(http.StatusFound, "/login") })

	// Legacy: /admin/login → /login
	r.GET("/admin/login", func(c *gin.Context) { c.Redirect(http.StatusFound, middleware.LoginURLWithNext(c.Query("next"))) })
	r.POST("/admin/login", middleware.RateLimit(10, time.Minute), loginHandler(cfg))

	r.GET("/register", registerPageHandler(cfg))
	r.POST("/register", middleware.RateLimit(5, time.Minute), registerPostHandler(cfg))
	r.GET("/register/confirm", registerConfirmPageHandler(cfg))
	r.POST("/register/confirm", middleware.RateLimit(5, time.Minute), registerConfirmPostHandler(cfg))

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

		apiGroup.GET("/exams", middleware.RateLimit(60, time.Minute), middleware.AndroidVersionCheck(), api.ListExams())
		apiGroup.POST("/exams/request-approval", middleware.RateLimit(30, time.Minute), middleware.AndroidVersionCheck(), api.RequestApproval())
		apiGroup.GET("/exams/token/:token", middleware.RateLimit(30, time.Minute), middleware.AndroidVersionCheck(), api.ExamByToken())
		apiGroup.GET("/exams/:exam_id/pdf", middleware.RateLimit(30, time.Minute), middleware.AndroidVersionCheck(), api.ExamPDF())
		apiGroup.POST("/exams/:exam_id/submit", middleware.LimitBodySize(5*1024*1024), middleware.RateLimit(10, time.Minute), middleware.AndroidVersionCheck(), api.SubmitExam())
		apiGroup.POST("/exams/:exam_id/access-log", middleware.LimitBodySize(256*1024), middleware.RateLimit(30, time.Minute), middleware.AndroidVersionCheck(), api.AccessLog())
		apiGroup.POST("/exams/:exam_id/complete", middleware.LimitBodySize(256*1024), middleware.RateLimit(30, time.Minute), middleware.AndroidVersionCheck(), api.CompleteExam())

		apiGroup.GET("/hasil/:token", middleware.RateLimit(30, time.Minute), public.HasilAPI())
		apiGroup.GET("/payments/doku/notify", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "active"}) })
		apiGroup.POST("/payments/doku/notify", middleware.RateLimit(20, time.Minute), api.DokuNotifyHandler(cfg))
	}

	// ---- Admin pages (auth required) ----
	adminPages := r.Group("/admin", middleware.AuthRequired())
	{
		adminPages.GET("/dashboard", admin.Dashboard())
		adminPages.GET("/dashboard/redirect", admin.DashboardRedirect())
		adminPages.GET("/submissions", admin.SubmissionsPage())

		adminPages.GET("/users", middleware.AdminManagementRequired(), admin.UsersPage())
		adminPages.GET("/billing", admin.BillingPage())

		adminPages.GET("/pengawas", admin.PengawasPage())
		adminPages.GET("/pengawas/:exam_id", admin.PengawasDetailPage())
		adminPages.GET("/system-apps", middleware.SuperAdminRequired(), admin.SystemAppsPage())

		// Logout via POST only (CSRF-protected in the main route below).
		// GET /admin/logout simply redirects to login (prevents CSRF-based logout).
		adminPages.GET("/logout", func(c *gin.Context) { c.Redirect(http.StatusFound, "/login") })
	}

	// ---- Admin API (auth required) ----
	adminAPI := r.Group("/admin/api",
		middleware.AuthRequired(),
	)
	{
		adminAPI.GET("/stats", admin.Stats())

		// ---- CSRF-protected routes (all POST) ----
		csrfAPI := adminAPI.Group("", middleware.CSRFRequired())
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
			}

			// Transactions & Subscriptions
			csrfAPI.POST("/transactions", middleware.LimitBodySize(5*1024*1024), admin.CreateTransaction(cfg))
			csrfAPI.POST("/transactions/doku", middleware.LimitBodySize(256*1024), admin.CreateDokuTransaction(cfg))
			adminTransactions := csrfAPI.Group("", middleware.SuperAdminRequired())
			{
				adminTransactions.POST("/transactions/:id/approve", middleware.LimitBodySize(256*1024), admin.ApproveTransaction())
				adminTransactions.POST("/transactions/:id/reject", middleware.LimitBodySize(256*1024), admin.RejectTransaction())
			}

			csrfAPI.POST("/change-password", middleware.LimitBodySize(256*1024), middleware.RateLimit(3, time.Minute), admin.ChangePassword())
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
		adminUsersRead := adminAPI.Group("", middleware.AdminManagementRequired())
		{
			adminUsersRead.GET("/users", admin.ListUsers())
		}

		adminAPI.GET("/transactions", admin.ListTransactions())
		adminAPI.GET("/transactions/proofs/:filename", middleware.SuperAdminRequired(), admin.ServeProofFile(cfg))
		adminAPI.GET("/pengawas/exams", admin.PengawasExams())
		adminAPI.GET("/pengawas/exams/:exam_id/submissions", admin.PengawasExamSubmissions())
		adminAPI.GET("/pengawas/exams/:exam_id/approvals", admin.GetPendingApprovals())
		adminAPI.GET("/saas-settings", middleware.SuperAdminRequired(), admin.SaasSettings())
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

type pricingPlan struct {
	Key           string
	Title         string
	Audience      string
	Popular       bool
	Accent        string
	MonthlyPrice  int64
	SemesterPrice int64
	AnnualPrice   int64
	MaxExams      string
	PdfLimit      string
	DraftLimit    string
	StorageLimit  string
	TokenMode     string
	Results       string
	Answers       string
	Features      []string
}

func pricingHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version

		var dbPool *pgxpool.Pool
		if dbValue, exists := c.Get("db"); exists && dbValue != nil {
			dbPool, _ = dbValue.(*pgxpool.Pool)
		}
		prices := models.GetPricingMap(c.Request.Context(), dbPool)

		plans := []pricingPlan{
			{Key: "guru", Title: "Paket Guru", Audience: "Guru les, bimbel kecil, tryout kelas", Accent: "guru", MonthlyPrice: prices["guru_bulanan"], SemesterPrice: prices["guru_semester"], AnnualPrice: prices["guru_tahunan"], MaxExams: "1 ujian aktif", PdfLimit: "10 MB per PDF", DraftLimit: "10 draft soal", StorageLimit: "100 MB storage", TokenMode: "Statis", Results: "Aktif", Answers: "Nonaktif", Features: []string{"Cocok untuk kelas kecil", "Support email"}},
			{Key: "individu", Title: "Paket Individu", Audience: "Pembuat tryout online, bimbel 1-2 kelas", Accent: "individu", MonthlyPrice: prices["individu_bulanan"], SemesterPrice: prices["individu_semester"], AnnualPrice: prices["individu_tahunan"], MaxExams: "2 ujian aktif", PdfLimit: "30 MB per PDF", DraftLimit: "30 draft soal", StorageLimit: "300 MB storage", TokenMode: "Statis + dinamis", Results: "Aktif", Answers: "Aktif", Features: []string{"Export CSV", "Support email + WA"}},
			{Key: "sekolah_kecil", Title: "Sekolah Kecil", Audience: "SD / MI, ujian PH / UTS", Accent: "kecil", MonthlyPrice: prices["sekolah_kecil_bulanan"], SemesterPrice: prices["sekolah_kecil_semester"], AnnualPrice: prices["sekolah_kecil_tahunan"], MaxExams: "3 ujian aktif", PdfLimit: "50 MB per PDF", DraftLimit: "50 draft soal", StorageLimit: "500 MB storage", TokenMode: "Statis", Results: "Aktif", Answers: "Nonaktif", Features: []string{"Manajemen Pengguna", "Paket hemat sekolah dasar"}},
			{Key: "sekolah_menengah", Title: "Sekolah Menengah", Audience: "SMP / MTs, ujian PAS / PAT", Accent: "menengah", MonthlyPrice: prices["sekolah_menengah_bulanan"], SemesterPrice: prices["sekolah_menengah_semester"], AnnualPrice: prices["sekolah_menengah_tahunan"], MaxExams: "5 ujian aktif", PdfLimit: "200 MB per PDF", DraftLimit: "200 draft soal", StorageLimit: "2 GB storage", TokenMode: "Statis + dinamis", Results: "Aktif", Answers: "Aktif", Features: []string{"Manajemen Pengguna", "Panel warna", "Dukungan email + WA"}},
			{Key: "sekolah_besar", Title: "Sekolah Besar", Audience: "SMA / MA / SMK, tryout skala besar", Popular: true, Accent: "besar", MonthlyPrice: prices["sekolah_besar_bulanan"], SemesterPrice: prices["sekolah_besar_semester"], AnnualPrice: prices["sekolah_besar_tahunan"], MaxExams: "10 ujian aktif", PdfLimit: "500 MB per PDF", DraftLimit: "500 draft soal", StorageLimit: "5 GB storage", TokenMode: "Statis + dinamis", Results: "Aktif", Answers: "Aktif", Features: []string{"Manajemen Pengguna", "Realtime pengawas", "Strict mode", "Export CSV lengkap"}},
			{Key: "sekolah_unggulan", Title: "Sekolah Unggulan", Audience: "Kampus, yayasan, skala kabupaten/kota", Accent: "unggulan", MonthlyPrice: prices["sekolah_unggulan_bulanan"], SemesterPrice: prices["sekolah_unggulan_semester"], AnnualPrice: prices["sekolah_unggulan_tahunan"], MaxExams: "Tak terbatas", PdfLimit: "Tak terbatas", DraftLimit: "Tak terbatas", StorageLimit: "Tak terbatas", TokenMode: "Statis + dinamis", Results: "Aktif", Answers: "Aktif", Features: []string{"Manajemen Pengguna", "Prioritas infrastruktur", "Backup mingguan", "SLA 99% uptime"}},
		}

		data["prices"] = prices
		data["plans"] = plans
		c.HTML(http.StatusOK, "public/pricing.html", data)
	}
}

func parsePrice(val string, defaultVal int64) int64 {
	if val == "" {
		return defaultVal
	}
	p, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return defaultVal
	}
	return p
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
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["error"] = nil
		data["flashes"] = nil
		data["next"] = middleware.SafeRedirectPath(c.Query("next"))
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
		username := strings.TrimSpace(c.PostForm("username"))
		password := c.PostForm("password")
		nextTarget := middleware.SafeRedirectPath(c.PostForm("next"))
		if nextTarget == "" {
			nextTarget = middleware.SafeRedirectPath(c.Query("next"))
		}

		if username == "" || password == "" {
			data := middleware.TemplateData(c)
			data["error"] = "Username dan password wajib diisi."
			data["version"] = cfg.Version
			data["next"] = nextTarget
			c.HTML(http.StatusOK, "admin/login.html", data)
			return
		}

		// When there is no database, authentication is not possible.
		pool, exists := c.Get("db")
		if !exists || pool == nil {
			data := middleware.TemplateData(c)
			data["error"] = "Database tidak tersedia. Silakan hubungi administrator."
			data["version"] = cfg.Version
			data["next"] = nextTarget
			c.HTML(http.StatusOK, "admin/login.html", data)
			return
		}

		dbPool := pool.(*pgxpool.Pool)
		ctx := c.Request.Context()

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
					data := middleware.TemplateData(c)
					data["error"] = fmt.Sprintf("Akun dikunci sementara karena terlalu banyak kegagalan login. Silakan coba lagi dalam %d menit.", int(ttl.Minutes())+1)
					data["version"] = cfg.Version
					data["next"] = nextTarget
					c.HTML(http.StatusOK, "admin/login.html", data)
					return
				}
			}
		} else {
			loginLock.Lock()
			if exp, locked := lockoutTimes[username]; locked && time.Now().Before(exp) {
				loginLock.Unlock()
				data := middleware.TemplateData(c)
				data["error"] = fmt.Sprintf("Akun dikunci sementara karena terlalu banyak kegagalan login. Silakan coba lagi dalam %d menit.", int(time.Until(exp).Minutes())+1)
				data["version"] = cfg.Version
				data["next"] = nextTarget
				c.HTML(http.StatusOK, "admin/login.html", data)
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
				failedLogins[username]++
				if failedLogins[username] >= 5 {
					lockoutTimes[username] = time.Now().Add(15 * time.Minute)
					failedLogins[username] = 0
				}
				loginLock.Unlock()
			}

			data := middleware.TemplateData(c)
			data["error"] = errMsg
			data["version"] = cfg.Version
			data["next"] = nextTarget
			c.HTML(http.StatusOK, "admin/login.html", data)
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
		data := middleware.TemplateData(c)
		data["version"] = cfg.Version
		data["error"] = nil
		data["flashes"] = nil

		pool, exists := c.Get("db")
		if exists && pool != nil {
			dbPool := pool.(*pgxpool.Pool)
			data["email_enabled"] = models.GetSaasSettingBool(c.Request.Context(), dbPool, models.SettingEmailVerificationEnabled, false)
		} else {
			data["email_enabled"] = false
		}

		c.HTML(http.StatusOK, "public/register.html", data)
	}
}

func registerPostHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		username := strings.TrimSpace(c.PostForm("username"))
		email := strings.TrimSpace(c.PostForm("email"))
		password := c.PostForm("password")

		data := middleware.TemplateData(c)
		data["version"] = cfg.Version

		// Validate input
		if username == "" || password == "" || email == "" {
			data["error"] = "Username, email, dan password wajib diisi."
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

		if !strings.Contains(email, "@") || !strings.Contains(email, ".") {
			data["error"] = "Format email tidak valid."
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
			Username:       username,
			PasswordHash:   password, // will be hashed by CreateUser
			Status:         status,
			Instansi:       "personal",
			Role:           models.SerializeRoles([]string{models.RoleGuru}),
			MaxExams:       models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxExams, 3),
			MaxPDFSize:     models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxPDFSize, 1048576),
			MaxDrafts:      models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxDrafts, 2),
			MaxDraftSize:   models.GetSaasSettingInt(ctx, dbPool, models.SettingDefaultMaxDraftSize, 1048576),
			WhatsappNumber: "",
			Email:          email,
			OTPCode:        otpCode,
			OTPExpiry:      otpExpiry,
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
		err := dbPool.QueryRow(ctx,
			`SELECT id, username, email, status, otp_code, otp_expiry FROM admin_users 
			 WHERE LOWER(username) = LOWER($1)`, username).Scan(&u.ID, &u.Username, &u.Email, &u.Status, &u.OTPCode, &u.OTPExpiry)

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
			data["error"] = "Kode OTP yang Anda masukkan salah."
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

// startTransactionCleaner runs a background routine to reject pending transactions older than 24 hours.
func startTransactionCleaner(pool *pgxpool.Pool) {
	if pool == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		// Run immediately on start
		cleanExpiredTransactions(context.Background(), pool)

		for range ticker.C {
			cleanExpiredTransactions(context.Background(), pool)
		}
	}()
	log.Println("transaction-cleaner: started (clean every 1h)")
}

func cleanExpiredTransactions(ctx context.Context, pool *pgxpool.Pool) {
	result, err := pool.Exec(ctx,
		`UPDATE transactions 
		 SET status = 'rejected', notes = 'Expired automatically after 24h pending'
		 WHERE status = 'pending' AND created_at < CURRENT_TIMESTAMP - INTERVAL '24 hours'`,
	)
	if err != nil {
		log.Printf("transaction-cleaner: failed to clean expired transactions: %v", err)
		return
	}
	rows := result.RowsAffected()
	if rows > 0 {
		log.Printf("transaction-cleaner: successfully expired %d pending transactions", rows)
	}
}
